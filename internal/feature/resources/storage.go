package resources

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/internal/storage"
)

type StorageInput struct {
	StorageID        int64    `json:"storageId" uri:"storageId" form:"storageId"`
	Name             string   `json:"name" validate:"max=64"`
	Driver           string   `json:"driver" validate:"required,oneof=local s3"`
	Preset           string   `json:"preset" validate:"max=32"`
	Endpoint         string   `json:"endpoint" validate:"max=255"`
	Region           string   `json:"region" validate:"max=64"`
	Bucket           string   `json:"bucket" validate:"max=128"`
	AccessKeyID      string   `json:"accessKeyId" validate:"max=255"`
	SecretKey        string   `json:"secretKey" validate:"max=4096"`
	UseSSL           bool     `json:"useSsl"`
	PathStyle        bool     `json:"pathStyle"`
	BaseURL          string   `json:"baseUrl" validate:"max=255"`
	URLMode          string   `json:"urlMode" validate:"omitempty,oneof=static presigned"`
	PresignExpireSec int      `json:"presignExpireSec" validate:"min=0,max=604800"`
	LocalDir         string   `json:"localDir" validate:"max=255"`
	MaxSize          int64    `json:"maxSize" validate:"min=0,max=1073741824"`
	AllowExt         []string `json:"allowExt"`
	Status           string   `json:"status" validate:"omitempty,oneof=0 1"`
}
type StorageStatus struct {
	StorageID int64  `json:"storageId" validate:"required,gt=0"`
	Status    string `json:"status" validate:"required,oneof=0 1"`
}
type StorageView struct {
	model.StorageConfig
	SecretKey string `json:"secretKey"`
}
type StorageList struct {
	Rows     []StorageView `json:"rows"`
	Total    int64         `json:"total"`
	PageNum  int           `json:"pageNum"`
	PageSize int           `json:"pageSize"`
}

func (s *Service) storageView(row model.StorageConfig) (StorageView, error) {
	key, err := s.cipher.Decrypt(row.SecretKeyEnc)
	if err != nil {
		return StorageView{}, err
	}
	return StorageView{row, secret.Mask(key)}, nil
}
func (s *Service) normalizeStorage(in StorageInput, before model.StorageConfig) (model.StorageConfig, error) {
	if strings.TrimSpace(in.Name) == "" {
		return before, errcode.BadRequestf("存储名称必填")
	}
	if in.MaxSize == 0 {
		in.MaxSize = 20 << 20
	}
	if in.URLMode == "" {
		in.URLMode = "static"
	}
	if in.PresignExpireSec == 0 {
		in.PresignExpireSec = 3600
	}
	if in.Driver == "local" && in.LocalDir == "" {
		in.LocalDir = "./uploads"
	}
	if in.Preset == "minio" {
		in.PathStyle = true
	}
	for _, preset := range storage.Presets() {
		if preset.Preset == in.Preset && in.Driver == "s3" && in.Endpoint == "" {
			in.Endpoint = preset.Endpoint
		}
	}
	in.Endpoint = strings.ReplaceAll(in.Endpoint, "{region}", strings.TrimSpace(in.Region))
	if strings.ContainsAny(in.Endpoint, "{}") {
		return before, errcode.BadRequestf("请填写完整存储地址并替换区域或 account 占位符")
	}

	if in.Preset == "" {
		in.Preset = "custom"
	}
	if len(in.AllowExt) == 0 {
		in.AllowExt = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt", ".csv", ".xlsx", ".zip"}
	}
	exts := []string{}
	for _, e := range in.AllowExt {
		e = strings.ToLower(strings.TrimSpace(e))
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if len(e) < 2 || len(e) > 16 || filepath.Ext("file"+e) != e || strings.ContainsAny(e, "/\\") {
			return before, errcode.BadRequestf("无效的扩展名")
		}
		exts = append(exts, e)
	}
	row := before
	row.Name = strings.TrimSpace(in.Name)
	row.Driver = in.Driver
	row.Preset = in.Preset
	row.Endpoint = in.Endpoint
	row.Region = in.Region
	row.Bucket = in.Bucket
	row.AccessKeyID = in.AccessKeyID
	row.UseSSL = in.UseSSL
	row.PathStyle = in.PathStyle
	row.BaseURL = strings.TrimRight(in.BaseURL, "/")
	row.URLMode = in.URLMode
	row.PresignExpireSec = in.PresignExpireSec
	row.LocalDir = in.LocalDir
	row.MaxSize = in.MaxSize
	row.AllowExt, _ = json.Marshal(exts)
	var err error
	if in.SecretKey != "" {
		row.SecretKeyEnc, err = s.cipher.Encrypt(in.SecretKey)
		if err != nil {
			return row, err
		}
	}
	cfg, err := s.storageCfg(row)
	if err != nil {
		return row, err
	}
	if _, err = storage.New(cfg); err != nil {
		return row, errcode.BadRequestf("%s", err.Error())
	}
	if before.StorageID == 0 || !sameStorageConnection(before, row) {
		row.LastTestedAt = nil
		row.LastTestResult = nil
		row.Status = "1"
		row.IsDefault = false
	}
	if in.Status != "" {
		if in.Status == "0" && !storageReady(row) {
			return row, errcode.BadRequestf("请先测试存储后再启用")
		}
		row.Status = in.Status
	}
	if row.Status == "" {
		row.Status = "1"
	}
	if row.Status != "0" {
		row.IsDefault = false
	}
	row.DelFlag = "0"
	return row, nil
}
func storageMap(r model.StorageConfig) map[string]any {
	return map[string]any{"name": r.Name, "driver": r.Driver, "preset": r.Preset, "endpoint": r.Endpoint, "region": r.Region, "bucket": r.Bucket, "access_key_id": r.AccessKeyID, "use_ssl": r.UseSSL, "path_style": r.PathStyle, "base_url": r.BaseURL, "url_mode": r.URLMode, "presign_expire_sec": r.PresignExpireSec, "local_dir": r.LocalDir, "max_size": r.MaxSize, "allow_ext": string(r.AllowExt), "status": r.Status, "is_default": r.IsDefault, "del_flag": r.DelFlag, "last_tested_at": r.LastTestedAt, "last_test_result": jsonValue(r.LastTestResult)}
}
func (s *Service) storageList(ctx context.Context, in ConfigListInput) (StorageList, error) {
	q := s.dbFor(ctx).Model(&model.StorageConfig{}).Where("del_flag = '0'")
	if in.Name != "" {
		q = q.Where("name ILIKE ?", "%"+in.Name+"%")
	}
	if in.Status != "" {
		q = q.Where("status = ?", in.Status)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return StorageList{}, err
	}
	var rows []model.StorageConfig
	if err := q.Scopes(in.Query.Scope()).Order("storage_id DESC").Find(&rows).Error; err != nil {
		return StorageList{}, err
	}
	out := StorageList{Rows: []StorageView{}, Total: count, PageNum: in.PageNum, PageSize: in.PageSize}
	for _, r := range rows {
		v, err := s.storageView(r)
		if err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, v)
	}
	return out, nil
}
func (s *Service) saveStorage(ctx context.Context, in StorageInput, create bool) (StorageView, error) {
	before := model.StorageConfig{}
	var err error
	if !create {
		if in.StorageID <= 0 {
			return StorageView{}, errcode.BadRequestf("storageId 必填")
		}
		before, err = s.lockStorage(ctx, in.StorageID)
		if err != nil {
			return StorageView{}, err
		}
	}
	after, err := s.normalizeStorage(in, before)
	if err != nil {
		return StorageView{}, err
	}
	if err = s.checkStorageLocation(ctx, before, after); err != nil {
		return StorageView{}, err
	}
	rec := capability.RecorderFrom(ctx)
	var data undoData
	if create {
		after.IdentityAudit = audit(ctx)
		if err = s.privateDB(ctx).Create(&after).Error; err != nil {
			return StorageView{}, mappedError(err)
		}
		rec.Create(after.TableName(), capability.ID(after.StorageID), after.Name, storageMap(after))
		data.Rows = []undoRow{{Table: after.TableName(), IDField: "storage_id", ID: after.StorageID, After: storageMap(after), Created: true}}
	} else {
		b, a := diffMap(storageMap(before), storageMap(after))
		ref, err := s.vaultGuard(ctx, "secret_key_enc", before.SecretKeyEnc, after.SecretKeyEnc, !sameStorageConnection(before, after))
		if err != nil {
			return StorageView{}, err
		}
		if len(a) == 0 && ref == "" {
			rec.Skip(after.TableName(), capability.ID(after.StorageID), after.Name, "配置没有变化")
			return s.storageView(after)
		}
		after.IdentityAudit.UpdateBy = actorName(ctx)
		after.IdentityAudit.UpdateTime = audit(ctx).UpdateTime
		if err = s.privateDB(ctx).Save(&after).Error; err != nil {
			return StorageView{}, mappedError(err)
		}
		label := after.Name
		if in.SecretKey != "" {
			label += "（凭据已更新）"
		}
		rec.Update(after.TableName(), capability.ID(after.StorageID), label, b, a)
		data.Rows = []undoRow{configUndoRow(after.TableName(), "storage_id", after.StorageID, b, a, storageMap(before), storageMap(after), ref)}
	}
	rec.SetUndo(data)
	return s.storageView(after)
}
func (s *Service) previewStorage(ctx context.Context, in StorageInput, create bool) (*capability.Preview, error) {
	before := model.StorageConfig{}
	var err error
	if !create {
		before, err = s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return nil, err
		}
	}
	after, err := s.normalizeStorage(in, before)
	if err != nil {
		return nil, err
	}
	if err = s.checkStorageLocation(ctx, before, after); err != nil {
		return nil, err
	}
	op := capability.OpUpdate
	if create {
		op = capability.OpCreate
	}
	p := preview(after.TableName(), capability.ID(in.StorageID), after.Name, op, storageMap(before), storageMap(after))
	if in.SecretKey != "" {
		p.Warn("Secret 将加密保存，变更历史不含凭据值")
		if p.Affected == 0 {
			p.AddChange(capability.Change{Entity: after.TableName(), ID: capability.ID(in.StorageID), Label: after.Name, Op: op, Fields: []capability.FieldDiff{{Field: "credentialChanged", Before: "已配置", After: "替换"}}})
		}
	}
	return p, nil
}
func (s *Service) storageDefault(ctx context.Context, in StorageID) (capability.Empty, error) {
	row, err := s.storageConfig(ctx, in.StorageID)
	if err != nil {
		return capability.Empty{}, err
	}
	if row.Status != "0" || !storageReady(row) {
		return capability.Empty{}, errcode.BadRequestf("默认存储必须已测试并启用")
	}
	if err = s.dbFor(ctx).Exec("SELECT pg_advisory_xact_lock(?)", int64(74190033)).Error; err != nil {
		return capability.Empty{}, err
	}
	var rows []model.StorageConfig
	if err = s.dbFor(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("del_flag = '0' AND (is_default = true OR storage_id = ?)", in.StorageID).Find(&rows).Error; err != nil {
		return capability.Empty{}, err
	}
	found := false
	for _, r := range rows {
		if r.StorageID == in.StorageID {
			found = true
			if r.Status != "0" || !storageReady(r) {
				return capability.Empty{}, errcode.Conflictf("配置已停用或测试状态已变化，请重新测试")
			}
		}
	}
	if !found {
		return capability.Empty{}, errcode.NotFoundf("配置不存在")
	}
	rec := capability.RecorderFrom(ctx)
	data := undoData{}
	for _, r := range rows {
		want := r.StorageID == in.StorageID
		if r.IsDefault == want {
			continue
		}
		b, a := map[string]any{"is_default": r.IsDefault}, map[string]any{"is_default": want}
		if !want {
			if err = s.dbFor(ctx).Model(&model.StorageConfig{}).Where("storage_id = ?", r.StorageID).Update("is_default", false).Error; err != nil {
				return capability.Empty{}, mappedError(err)
			}
		}
		rec.Update(r.TableName(), capability.ID(r.StorageID), r.Name, b, a)
		data.Rows = append(data.Rows, undoRow{Table: r.TableName(), IDField: "storage_id", ID: r.StorageID, Before: b, After: a})
	}
	if err = s.dbFor(ctx).Model(&model.StorageConfig{}).Where("storage_id = ?", in.StorageID).Update("is_default", true).Error; err != nil {
		return capability.Empty{}, mappedError(err)
	}
	rec.SetUndo(data)
	return capability.Empty{}, nil
}
func (s *Service) registerStorage() {
	meta := func(name, summary, method, path, perm string, write bool) capability.Meta {
		risk := capability.RiskLow
		if write {
			risk = capability.RiskHigh
		}
		return capability.Meta{Name: "system.storage." + name, Summary: summary, Tags: []string{"存储配置"}, Perm: "system:storage:" + perm, Risk: risk, Mutating: write, Reversible: write, HTTP: &capability.HTTPBinding{Method: method, Path: "/system/storage" + path}, AIEnabled: !write, MCPEnabled: !write, LogTitle: "存储配置"}
	}
	capability.Register(capability.Capability[ConfigListInput, StorageList]{Meta: meta("list", "查询存储配置", "GET", "/list", "list", false), Fn: s.storageList})
	capability.Register(capability.Capability[StorageID, StorageView]{Meta: meta("get", "获取存储配置", "GET", "/:storageId", "query", false), Fn: func(ctx context.Context, in StorageID) (StorageView, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return StorageView{}, err
		}
		return s.storageView(r)
	}})
	capability.Register(capability.Capability[Empty, []storage.Preset]{Meta: meta("presets", "存储预设", "GET", "/presets", "list", false), Fn: func(context.Context, Empty) ([]storage.Preset, error) { return storage.Presets(), nil }})
	for _, create := range []bool{true, false} {
		name, method, perm := "update", "PUT", "edit"
		if create {
			name, method, perm = "create", "POST", "add"
		}
		capability.Register(capability.Capability[StorageInput, StorageView]{Meta: meta(name, "保存存储配置", method, "/"+name, perm, true), Fn: func(ctx context.Context, in StorageInput) (StorageView, error) { return s.saveStorage(ctx, in, create) }, DryRun: func(ctx context.Context, in StorageInput) (*capability.Preview, error) {
			return s.previewStorage(ctx, in, create)
		}, Undo: s.undo})
	}
	capability.Register(capability.Capability[StorageID, capability.Empty]{Meta: meta("setDefault", "设为默认存储", "PUT", "/setDefault", "edit", true), Fn: s.storageDefault, DryRun: func(ctx context.Context, in StorageID) (*capability.Preview, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return nil, err
		}
		if r.Status != "0" || !storageReady(r) {
			return nil, errcode.BadRequestf("请先测试并启用存储")
		}
		return s.defaultPreview(ctx, r.TableName(), "storage_id", r.StorageID)
	}, Undo: s.undo})
	capability.Register(capability.Capability[StorageStatus, capability.Empty]{Meta: meta("changeStatus", "启停存储配置", "PUT", "/changeStatus", "edit", true), Fn: func(ctx context.Context, in StorageStatus) (capability.Empty, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return capability.Empty{}, err
		}
		if in.Status == "0" && !storageReady(r) {
			return capability.Empty{}, errcode.BadRequestf("请先测试存储")
		}
		b := map[string]any{"status": r.Status, "is_default": r.IsDefault}
		a := map[string]any{"status": in.Status, "is_default": r.IsDefault && in.Status == "0"}
		if err = s.dbFor(ctx).Model(&model.StorageConfig{}).Where("storage_id = ?", r.StorageID).Updates(a).Error; err != nil {
			return capability.Empty{}, err
		}
		rec := capability.RecorderFrom(ctx)
		rec.Update(r.TableName(), capability.ID(r.StorageID), r.Name, b, a)
		rec.SetUndo(undoData{Rows: []undoRow{{Table: r.TableName(), IDField: "storage_id", ID: r.StorageID, Before: b, After: a}}})
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in StorageStatus) (*capability.Preview, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return nil, err
		}
		if in.Status == "0" && !storageReady(r) {
			return nil, errcode.BadRequestf("请先测试存储")
		}
		return preview(r.TableName(), capability.ID(r.StorageID), r.Name, capability.OpUpdate, map[string]any{"status": r.Status, "is_default": r.IsDefault}, map[string]any{"status": in.Status, "is_default": r.IsDefault && in.Status == "0"}), nil
	}, Undo: s.undo})
	capability.Register(capability.Capability[StorageID, capability.Empty]{Meta: meta("remove", "删除存储配置", "DELETE", "/:storageId", "remove", true), Fn: func(ctx context.Context, in StorageID) (capability.Empty, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return capability.Empty{}, err
		}
		if r.IsDefault {
			return capability.Empty{}, errcode.Conflictf("请先选择其他默认存储")
		}
		var n int64
		if err = s.dbFor(ctx).Model(&model.Upload{}).Where("storage_id = ? AND status <> '2'", r.StorageID).Count(&n).Error; err != nil {
			return capability.Empty{}, err
		}
		if n > 0 {
			return capability.Empty{}, errcode.Conflictf("该存储仍有 %d 个文件，不能删除", n)
		}
		b, a := map[string]any{"del_flag": "0"}, map[string]any{"del_flag": "2"}
		if err = s.dbFor(ctx).Model(&model.StorageConfig{}).Where("storage_id = ?", r.StorageID).Updates(a).Error; err != nil {
			return capability.Empty{}, err
		}
		rec := capability.RecorderFrom(ctx)
		rec.Update(r.TableName(), capability.ID(r.StorageID), r.Name, b, a)
		rec.SetUndo(undoData{Rows: []undoRow{{Table: r.TableName(), IDField: "storage_id", ID: r.StorageID, Before: b, After: a}}})
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in StorageID) (*capability.Preview, error) {
		r, err := s.storageConfig(ctx, in.StorageID)
		if err != nil {
			return nil, err
		}
		if r.IsDefault {
			return nil, errcode.Conflictf("请先选择其他默认存储")
		}
		var n int64
		if err = s.dbFor(ctx).Model(&model.Upload{}).Where("storage_id = ? AND status <> '2'", r.StorageID).Count(&n).Error; err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, errcode.Conflictf("该存储仍有 %d 个文件，不能删除", n)
		}
		return preview(r.TableName(), capability.ID(r.StorageID), r.Name, capability.OpDelete, storageMap(r), nil), nil
	}, Undo: s.undo})
}

func (s *Service) checkStorageLocation(ctx context.Context, before, after model.StorageConfig) error {
	if before.StorageID == 0 || before.Driver == after.Driver && before.Endpoint == after.Endpoint && before.Bucket == after.Bucket && before.LocalDir == after.LocalDir {
		return nil
	}
	var n int64
	if err := s.dbFor(ctx).Model(&model.Upload{}).Where("storage_id = ? AND status <> '2'", before.StorageID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errcode.Conflictf("该存储仍有关联文件，请新建存储配置并切换默认项，以保留旧文件访问")
	}
	return nil
}

func storageReady(r model.StorageConfig) bool {
	if r.LastTestedAt == nil {
		return false
	}
	var result storage.TestResult
	return json.Unmarshal(r.LastTestResult, &result) == nil && result.OK && result.BucketExists && result.Writable && result.Readable
}
