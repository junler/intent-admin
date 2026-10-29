package resources

import (
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/pkg/response"
)

// Export completion is a personal delivery channel, independent of notice and
// file administration grants. A UUID in a notice never grants file access.
type Notification struct {
	NoticeID      int64     `gorm:"column:notice_id" json:"noticeId"`
	NoticeTitle   string    `gorm:"column:notice_title" json:"noticeTitle"`
	NoticeType    string    `gorm:"column:notice_type" json:"noticeType"`
	NoticeContent string    `gorm:"column:notice_content" json:"noticeContent"`
	CreateBy      string    `gorm:"column:create_by" json:"createBy"`
	CreateTime    time.Time `gorm:"column:create_time" json:"createTime"`
	UploadID      string    `gorm:"-" json:"uploadId,omitempty"`
	FileName      string    `gorm:"-" json:"fileName,omitempty"`
	DownloadURL   string    `gorm:"-" json:"downloadUrl,omitempty"`
}

var exportFileID = regexp.MustCompile(`文件编号\s+([0-9a-fA-F-]{36})(?:\s|，|。|$)`)

func (s *Service) notifications(c *gin.Context) {
	if !require(c, "") {
		return
	}
	actor, _ := capability.ActorFrom(c.Request.Context())
	var query paginate.Query
	if c.ShouldBindQuery(&query) != nil || query.PageNum > 1_000_000 {
		response.Fail(c, errcode.BadRequestf("分页参数无效"))
		return
	}
	query.Normalize()
	ctx := c.Request.Context()
	q := s.dbFor(ctx).Table("sys_notice").Where("status = '0' AND del_flag = '0'")
	prefix := "导出完成：%"
	if actor.Can("system:notice:list") {
		q = q.Where("notice_title NOT LIKE ? OR create_by = ?", prefix, actor.UserName)
	} else {
		q = q.Where("notice_title LIKE ? AND create_by = ?", prefix, actor.UserName)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		response.Fail(c, err)
		return
	}
	rows := []Notification{}
	if err := q.Select("notice_id, notice_title, notice_type, notice_content, create_by, create_time").Order("create_time DESC, notice_id DESC").Scopes(query.Scope()).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	for i := range rows {
		notice := &rows[i]
		if notice.CreateBy != actor.UserName || !strings.HasPrefix(notice.NoticeTitle, "导出完成：") {
			continue
		}
		matches := exportFileID.FindStringSubmatch(notice.NoticeContent)
		if len(matches) != 2 {
			continue
		}
		id, err := uuid.Parse(matches[1])
		if err != nil {
			continue
		}
		var file model.Upload
		err = s.dbFor(ctx).Where("upload_id = ? AND create_by = ? AND biz_type = 'export' AND status = '0' AND del_flag = '0'", id.String(), actor.UserName).First(&file).Error
		if err != nil {
			continue
		}
		notice.UploadID = file.UploadID
		notice.FileName = file.FileName
		notice.DownloadURL = "/system/upload/" + file.UploadID + "/download"
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, response.Page{Rows: rows, Total: count, PageNum: query.PageNum, PageSize: query.PageSize})
}
