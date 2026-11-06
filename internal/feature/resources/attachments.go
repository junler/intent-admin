package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

// AttachmentPolicy is registered at startup by a business module. Authorize
// must check both the live record and the caller's permissions on each access.
// Entity names are registry keys, never client-selected SQL table names.
type AttachmentPolicy struct {
	Authorize func(context.Context, string, bool) error
}
type AttachmentTarget struct {
	Entity   string `json:"entity" form:"entity" validate:"required,max=100"`
	RecordID string `json:"recordId" form:"recordId" validate:"required,max=100"`
}
type AttachmentInput struct {
	AttachmentTarget
	UploadIDs []string `json:"uploadIds" validate:"max=20,dive,uuid"`
	Revision  string   `json:"revision" validate:"required,len=64"`
}
type AttachmentItem struct {
	UploadID string `json:"uploadId"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	Mime     string `json:"mime"`
}
type AttachmentList struct {
	Rows     []AttachmentItem `json:"rows"`
	Revision string           `json:"revision"`
}
type attachmentLink struct {
	Entity   string
	RecordID string
	UploadID string
}

func (attachmentLink) TableName() string { return "sys_attachment" }

func (s *Service) RegisterAttachmentPolicy(entity string, p AttachmentPolicy) {
	if entity == "" || p.Authorize == nil {
		panic("invalid attachment policy")
	}
	if s.attachmentPolicies == nil {
		s.attachmentPolicies = map[string]AttachmentPolicy{}
	}
	if _, ok := s.attachmentPolicies[entity]; ok {
		panic("duplicate attachment policy")
	}
	s.attachmentPolicies[entity] = p
}
func (s *Service) authorizeAttachment(ctx context.Context, t AttachmentTarget, write bool) error {
	if _, ok := capability.ActorFrom(ctx); !ok {
		return errcode.Unauthorizedf("请先登录")
	}
	if t.RecordID == "" || len(t.RecordID) > 100 || strings.TrimSpace(t.RecordID) != t.RecordID {
		return errcode.BadRequestf("业务记录编号无效")
	}
	p, ok := s.attachmentPolicies[t.Entity]
	if !ok {
		return errcode.NotFoundf("业务模块未启用附件")
	}
	return p.Authorize(ctx, t.RecordID, write)
}
func attachmentRevision(rows []AttachmentItem) string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UploadID)
	}
	slices.Sort(ids)
	h := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(h[:])
}
func (s *Service) attachmentList(ctx context.Context, t AttachmentTarget) (AttachmentList, error) {
	if err := s.authorizeAttachment(ctx, t, false); err != nil {
		return AttachmentList{}, err
	}
	rows := []AttachmentItem{}
	err := s.dbFor(ctx).Table("sys_attachment a").Select("u.upload_id,u.file_name,u.size,u.mime").Joins("JOIN sys_upload u ON u.upload_id=a.upload_id").Where("a.entity=? AND a.record_id=? AND u.del_flag='0' AND u.status='0'", t.Entity, t.RecordID).Order("a.create_time,u.upload_id").Scan(&rows).Error
	return AttachmentList{Rows: rows, Revision: attachmentRevision(rows)}, err
}
func (s *Service) planAttachments(ctx context.Context, in AttachmentInput) (*capability.Preview, error) {
	if err := s.authorizeAttachment(ctx, in.AttachmentTarget, true); err != nil {
		return nil, err
	}
	current, err := s.attachmentList(ctx, in.AttachmentTarget)
	if err != nil {
		return nil, err
	}
	if in.Revision != current.Revision {
		return nil, errcode.Conflictf("附件已变化，请刷新后重新编辑")
	}
	if len(in.UploadIDs) > 20 {
		return nil, errcode.BadRequestf("每条记录最多 20 个附件")
	}
	seen := map[string]bool{}
	a, _ := capability.ActorFrom(ctx)
	for _, id := range in.UploadIDs {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			return nil, errcode.BadRequestf("附件编号无效或重复")
		}
		seen[id] = true
		file, err := s.file(ctx, id)
		if err != nil {
			return nil, err
		}
		already := slices.ContainsFunc(current.Rows, func(r AttachmentItem) bool { return r.UploadID == id })
		if !already && (file.BizType != "business" || file.CreateBy != a.UserName) {
			return nil, errcode.Forbiddenf("只能关联自己上传的业务附件")
		}
		var links int64
		if err := s.dbFor(ctx).Table("sys_attachment").Where("upload_id=? AND NOT (entity=? AND record_id=?)", id, in.Entity, in.RecordID).Count(&links).Error; err != nil {
			return nil, err
		}
		if links > 0 {
			return nil, errcode.Conflictf("附件已关联其他业务记录")
		}
	}
	before := []string{}
	for _, r := range current.Rows {
		before = append(before, r.UploadID)
	}
	p := &capability.Preview{Summary: "更新业务附件", Affected: 1, Changes: []capability.Change{{Entity: in.Entity, ID: in.RecordID, Op: capability.OpUpdate, Label: in.RecordID, Fields: []capability.FieldDiff{{Field: "attachments", Label: "附件", Before: before, After: in.UploadIDs}}}}}
	return p, nil
}
func (s *Service) replaceAttachments(ctx context.Context, in AttachmentInput) (capability.Empty, error) {
	db := s.dbFor(ctx)
	if err := db.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "attachments:"+in.Entity+":"+in.RecordID).Error; err != nil {
		return capability.Empty{}, err
	}
	// Lock uploads in stable order; two records cannot concurrently claim a file.
	ids := slices.Clone(in.UploadIDs)
	slices.Sort(ids)
	if len(ids) > 0 {
		var files []model.Upload
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("upload_id IN ?", ids).Order("upload_id").Find(&files).Error; err != nil {
			return capability.Empty{}, err
		}
	}
	p, err := s.planAttachments(ctx, in)
	if err != nil {
		return capability.Empty{}, err
	}
	if err := db.Where("entity=? AND record_id=?", in.Entity, in.RecordID).Delete(&attachmentLink{}).Error; err != nil {
		return capability.Empty{}, err
	}
	for _, id := range ids {
		if err := db.Create(&attachmentLink{Entity: in.Entity, RecordID: in.RecordID, UploadID: id}).Error; err != nil {
			return capability.Empty{}, err
		}
	}
	capability.RecorderFrom(ctx).Update(in.Entity, in.RecordID, in.RecordID, map[string]any{"attachments": p.Changes[0].Fields[0].Before}, map[string]any{"attachments": ids})
	return capability.Empty{}, nil
}
func (s *Service) attachmentUpload(c *gin.Context) {
	var t AttachmentTarget
	if c.ShouldBindQuery(&t) != nil {
		response.Fail(c, errcode.BadRequestf("附件参数无效"))
		return
	}
	if err := s.authorizeAttachment(c.Request.Context(), t, true); err != nil {
		response.Fail(c, err)
		return
	}
	c.Set("business-upload", true)
	s.receive(c, false)
}
func (s *Service) attachmentAccess(ctx context.Context, id string) (bool, error) {
	var link attachmentLink
	err := s.dbFor(ctx).Where("upload_id=?", id).Take(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, s.authorizeAttachment(ctx, AttachmentTarget{Entity: link.Entity, RecordID: link.RecordID}, false)
}
func (s *Service) registerAttachments() {
	capability.Register(capability.Capability[AttachmentTarget, AttachmentList]{Meta: capability.Meta{Name: "system.attachment.list", Summary: "查询业务附件", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/system/attachment/list"}}, Fn: s.attachmentList})
	capability.Register(capability.Capability[AttachmentInput, capability.Empty]{Meta: capability.Meta{Name: "system.attachment.replace", Summary: "更新业务附件", Risk: capability.RiskMedium, Mutating: true, HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/system/attachment"}}, Fn: s.replaceAttachments, DryRun: s.planAttachments})
}
