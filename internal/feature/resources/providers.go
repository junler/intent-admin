package resources

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/aiprovider"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/pkg/secret"
)

type ProviderInput struct {
	ProviderID   int64    `json:"providerId" uri:"providerId" form:"providerId"`
	Name         string   `json:"name" validate:"max=64"`
	Vendor       string   `json:"vendor" validate:"max=32"`
	BaseURL      string   `json:"baseUrl" validate:"max=255"`
	APIKey       string   `json:"apiKey" validate:"max=4096"`
	DefaultModel string   `json:"defaultModel" validate:"max=100"`
	Temperature  *float64 `json:"temperature" validate:"omitempty,min=0,max=2"`
	MaxTokens    *int     `json:"maxTokens" validate:"omitempty,min=1,max=1048576"`
	TimeoutSec   int      `json:"timeoutSec" validate:"min=0,max=300"`
	Sort         int      `json:"sort"`
	Status       string   `json:"status" validate:"omitempty,oneof=0 1"`
}
type ProviderStatus struct {
	ProviderID int64  `json:"providerId" validate:"required,gt=0"`
	Status     string `json:"status" validate:"required,oneof=0 1"`
}
type ConfigListInput struct {
	paginate.Query
	Name   string `json:"name" form:"name"`
	Status string `json:"status" form:"status" validate:"omitempty,oneof=0 1"`
}
type ProviderView struct {
	model.AIProvider
	APIKey string `json:"apiKey"`
}
type ProviderList struct {
	Rows     []ProviderView `json:"rows"`
	Total    int64          `json:"total"`
	PageNum  int            `json:"pageNum"`
	PageSize int            `json:"pageSize"`
}

func (s *Service) providerView(row model.AIProvider) (ProviderView, error) {
	key, err := s.cipher.Decrypt(row.APIKeyEnc)
	if err != nil {
		return ProviderView{}, err
	}
	return ProviderView{row, secret.Mask(key)}, nil
}
func (s *Service) normalizeProvider(in ProviderInput, current model.AIProvider) (model.AIProvider, error) {
	if strings.TrimSpace(in.Name) == "" {
		return current, errcode.BadRequestf("模型服务名称必填")
	}
	key := in.APIKey
	if key == "" {
		var err error
		key, err = s.cipher.Decrypt(current.APIKeyEnc)
		if err != nil {
			return current, err
		}
	}
	cfg, err := aiprovider.Normalize(aiprovider.Config{Vendor: in.Vendor, BaseURL: in.BaseURL, APIKey: key, Model: in.DefaultModel, TimeoutSec: in.TimeoutSec})
	if err != nil {
		return current, errcode.BadRequestf("%s", err.Error())
	}
	row := current
	row.Name = strings.TrimSpace(in.Name)
	row.Vendor = cfg.Vendor
	row.Adapter = cfg.Adapter
	row.BaseURL = cfg.BaseURL
	row.DefaultModel = cfg.Model
	if in.Temperature != nil {
		if *in.Temperature < 0 || *in.Temperature > 2 {
			return row, errcode.BadRequestf("温度必须在 0 到 2 之间")
		}
		value := math.Round(*in.Temperature*100) / 100
		row.Temperature = &value
	} else {
		row.Temperature = nil
	}
	if in.MaxTokens != nil && (*in.MaxTokens < 1 || *in.MaxTokens > 1048576) {
		return row, errcode.BadRequestf("最大输出长度必须在 1 到 1048576 之间")
	}
	row.MaxTokens = in.MaxTokens
	row.TimeoutSec = cfg.TimeoutSec
	row.Sort = in.Sort
	changed := current.ProviderID == 0 || !sameProviderConnection(current, row) || in.APIKey != ""
	if changed {
		row.SupportsToolCalling = false
		row.LastTestedAt = nil
		row.LastTestResult = nil
		row.Models = json.RawMessage(`[]`)
		row.Status = "1"
		row.IsDefault = false
	}
	if in.Status != "" {
		if in.Status == "0" && (!row.SupportsToolCalling || row.LastTestedAt == nil) {
			return row, errcode.BadRequestf("请先测试模型服务，确认支持工具调用后再启用")
		}
		row.Status = in.Status
	}
	if row.Status == "" {
		row.Status = "1"
	}
	if row.Status != "0" {
		row.IsDefault = false
	}
	if in.APIKey != "" {
		row.APIKeyEnc, err = s.cipher.Encrypt(in.APIKey)
		if err != nil {
			return row, err
		}
	}
	row.DelFlag = "0"
	return row, nil
}
func providerMap(r model.AIProvider) map[string]any {
	return map[string]any{"name": r.Name, "vendor": r.Vendor, "adapter": r.Adapter, "base_url": r.BaseURL, "default_model": r.DefaultModel, "temperature": r.Temperature, "max_tokens": r.MaxTokens, "timeout_sec": r.TimeoutSec, "sort": r.Sort, "status": r.Status, "is_default": r.IsDefault, "del_flag": r.DelFlag, "supports_tool_calling": r.SupportsToolCalling, "last_tested_at": r.LastTestedAt, "last_test_result": jsonValue(r.LastTestResult), "models": jsonValue(r.Models)}
}
func (s *Service) providerList(ctx context.Context, in ConfigListInput) (ProviderList, error) {
	q := s.dbFor(ctx).Model(&model.AIProvider{}).Where("del_flag = '0'")
	if in.Name != "" {
		q = q.Where("name ILIKE ?", "%"+in.Name+"%")
	}
	if in.Status != "" {
		q = q.Where("status = ?", in.Status)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return ProviderList{}, err
	}
	var rows []model.AIProvider
	if err := q.Scopes(in.Query.Scope()).Order("sort ASC, provider_id DESC").Find(&rows).Error; err != nil {
		return ProviderList{}, err
	}
	out := ProviderList{Rows: []ProviderView{}, Total: count, PageNum: in.PageNum, PageSize: in.PageSize}
	for _, r := range rows {
		v, err := s.providerView(r)
		if err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, v)
	}
	return out, nil
}
func (s *Service) saveProvider(ctx context.Context, in ProviderInput, create bool) (ProviderView, error) {
	before := model.AIProvider{}
	var err error
	if !create {
		if in.ProviderID <= 0 {
			return ProviderView{}, errcode.BadRequestf("providerId 必填")
		}
		before, err = s.lockProvider(ctx, in.ProviderID)
		if err != nil {
			return ProviderView{}, err
		}
	}
	after, err := s.normalizeProvider(in, before)
	if err != nil {
		return ProviderView{}, err
	}
	rec := capability.RecorderFrom(ctx)
	var data undoData
	if create {
		after.IdentityAudit = audit(ctx)
		if err := s.privateDB(ctx).Create(&after).Error; err != nil {
			return ProviderView{}, mappedError(err)
		}
		rec.Create(after.TableName(), capability.ID(after.ProviderID), after.Name, providerMap(after))
		data.Rows = []undoRow{{Table: after.TableName(), IDField: "provider_id", ID: after.ProviderID, After: providerMap(after), Created: true}}
	} else {
		b, a := diffMap(providerMap(before), providerMap(after))
		ref, err := s.vaultGuard(ctx, "api_key_enc", before.APIKeyEnc, after.APIKeyEnc, !sameProviderConnection(before, after))
		if err != nil {
			return ProviderView{}, err
		}
		if len(a) == 0 && ref == "" {
			rec.Skip(after.TableName(), capability.ID(after.ProviderID), after.Name, "配置没有变化")
			return s.providerView(after)
		}
		after.IdentityAudit.UpdateBy = actorName(ctx)
		after.IdentityAudit.UpdateTime = audit(ctx).UpdateTime
		if err = s.privateDB(ctx).Save(&after).Error; err != nil {
			return ProviderView{}, mappedError(err)
		}
		label := after.Name
		if in.APIKey != "" {
			label += "（凭据已更新）"
		}
		rec.Update(after.TableName(), capability.ID(after.ProviderID), label, b, a)
		data.Rows = []undoRow{configUndoRow(after.TableName(), "provider_id", after.ProviderID, b, a, providerMap(before), providerMap(after), ref)}
	}
	rec.SetUndo(data)
	return s.providerView(after)
}
func (s *Service) previewProvider(ctx context.Context, in ProviderInput, create bool) (*capability.Preview, error) {
	before := model.AIProvider{}
	var err error
	if !create {
		before, err = s.provider(ctx, in.ProviderID)
		if err != nil {
			return nil, err
		}
	}
	after, err := s.normalizeProvider(in, before)
	if err != nil {
		return nil, err
	}
	op := capability.OpUpdate
	if create {
		op = capability.OpCreate
	}
	p := preview(after.TableName(), capability.ID(in.ProviderID), after.Name, op, providerMap(before), providerMap(after))
	if in.APIKey != "" {
		p.Warn("API Key 将被加密替换；变更历史仅记录凭据变更事实")
		if p.Affected == 0 {
			p.AddChange(capability.Change{Entity: after.TableName(), ID: capability.ID(in.ProviderID), Label: after.Name, Op: op, Fields: []capability.FieldDiff{{Field: "credentialChanged", Label: "凭据", Before: "已配置", After: "替换"}}})
		}
	}
	return p, nil
}
func (s *Service) providerDefault(ctx context.Context, in ProviderID) (capability.Empty, error) {
	row, err := s.provider(ctx, in.ProviderID)
	if err != nil {
		return capability.Empty{}, err
	}
	if row.Status != "0" || !row.SupportsToolCalling || row.LastTestedAt == nil {
		return capability.Empty{}, errcode.BadRequestf("默认模型必须已启用且已通过工具调用测试")
	}
	if err = s.dbFor(ctx).Exec("SELECT pg_advisory_xact_lock(?)", int64(74190032)).Error; err != nil {
		return capability.Empty{}, err
	}
	var rows []model.AIProvider
	if err = s.dbFor(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("del_flag = '0' AND (is_default = true OR provider_id = ?)", in.ProviderID).Find(&rows).Error; err != nil {
		return capability.Empty{}, err
	}
	found := false
	for _, r := range rows {
		if r.ProviderID == in.ProviderID {
			found = true
			if r.Status != "0" || !r.SupportsToolCalling || r.LastTestedAt == nil {
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
		want := r.ProviderID == in.ProviderID
		if r.IsDefault == want {
			continue
		}
		b, a := map[string]any{"is_default": r.IsDefault}, map[string]any{"is_default": want}
		if !want {
			if err = s.dbFor(ctx).Model(&model.AIProvider{}).Where("provider_id = ?", r.ProviderID).Update("is_default", false).Error; err != nil {
				return capability.Empty{}, mappedError(err)
			}
		}
		rec.Update(r.TableName(), capability.ID(r.ProviderID), r.Name, b, a)
		data.Rows = append(data.Rows, undoRow{Table: r.TableName(), IDField: "provider_id", ID: r.ProviderID, Before: b, After: a})
	}
	if err = s.dbFor(ctx).Model(&model.AIProvider{}).Where("provider_id = ?", in.ProviderID).Update("is_default", true).Error; err != nil {
		return capability.Empty{}, mappedError(err)
	}
	rec.SetUndo(data)
	return capability.Empty{}, nil
}
func (s *Service) registerProviders() {
	meta := func(name, summary, method, path, perm string, write bool) capability.Meta {
		risk := capability.RiskLow
		if write {
			risk = capability.RiskHigh
		}
		return capability.Meta{Name: "system.aiProvider." + name, Summary: summary, Tags: []string{"模型服务"}, Perm: "system:aiprovider:" + perm, Risk: risk, Mutating: write, Reversible: write, HTTP: &capability.HTTPBinding{Method: method, Path: "/system/ai-provider" + path}, AIEnabled: !write, MCPEnabled: !write, LogTitle: "模型服务"}
	}
	capability.Register(capability.Capability[ConfigListInput, ProviderList]{Meta: meta("list", "查询模型服务", "GET", "/list", "list", false), Fn: s.providerList})
	capability.Register(capability.Capability[ProviderID, ProviderView]{Meta: meta("get", "获取模型服务", "GET", "/:providerId", "query", false), Fn: func(ctx context.Context, in ProviderID) (ProviderView, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return ProviderView{}, err
		}
		return s.providerView(r)
	}})
	capability.Register(capability.Capability[Empty, []aiprovider.Preset]{Meta: meta("presets", "模型厂商预设", "GET", "/presets", "list", false), Fn: func(context.Context, Empty) ([]aiprovider.Preset, error) { return aiprovider.Presets(), nil }})
	for _, create := range []bool{true, false} {
		name, method, perm := "update", "PUT", "edit"
		if create {
			name, method, perm = "create", "POST", "add"
		}
		capability.Register(capability.Capability[ProviderInput, ProviderView]{Meta: meta(name, "保存模型服务", ""+method, "/"+name, perm, true), Fn: func(ctx context.Context, in ProviderInput) (ProviderView, error) {
			return s.saveProvider(ctx, in, create)
		}, DryRun: func(ctx context.Context, in ProviderInput) (*capability.Preview, error) {
			return s.previewProvider(ctx, in, create)
		}, Undo: s.undo})
	}
	capability.Register(capability.Capability[ProviderID, capability.Empty]{Meta: meta("setDefault", "设为默认模型", "PUT", "/setDefault", "edit", true), Fn: s.providerDefault, DryRun: func(ctx context.Context, in ProviderID) (*capability.Preview, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return nil, err
		}
		if r.Status != "0" || !r.SupportsToolCalling || r.LastTestedAt == nil {
			return nil, errcode.BadRequestf("请先测试并启用模型")
		}
		return s.defaultPreview(ctx, r.TableName(), "provider_id", r.ProviderID)
	}, Undo: s.undo})
	capability.Register(capability.Capability[ProviderStatus, capability.Empty]{Meta: meta("changeStatus", "启停模型服务", "PUT", "/changeStatus", "edit", true), Fn: func(ctx context.Context, in ProviderStatus) (capability.Empty, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return capability.Empty{}, err
		}
		if in.Status == "0" && (!r.SupportsToolCalling || r.LastTestedAt == nil) {
			return capability.Empty{}, errcode.BadRequestf("请先测试模型服务")
		}
		b := map[string]any{"status": r.Status, "is_default": r.IsDefault}
		a := map[string]any{"status": in.Status, "is_default": r.IsDefault && in.Status == "0"}
		if err = s.dbFor(ctx).Model(&model.AIProvider{}).Where("provider_id = ?", r.ProviderID).Updates(a).Error; err != nil {
			return capability.Empty{}, err
		}
		rec := capability.RecorderFrom(ctx)
		rec.Update(r.TableName(), capability.ID(r.ProviderID), r.Name, b, a)
		rec.SetUndo(undoData{Rows: []undoRow{{Table: r.TableName(), IDField: "provider_id", ID: r.ProviderID, Before: b, After: a}}})
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in ProviderStatus) (*capability.Preview, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return nil, err
		}
		if in.Status == "0" && (!r.SupportsToolCalling || r.LastTestedAt == nil) {
			return nil, errcode.BadRequestf("请先测试模型服务")
		}
		return preview(r.TableName(), capability.ID(r.ProviderID), r.Name, capability.OpUpdate, map[string]any{"status": r.Status, "is_default": r.IsDefault}, map[string]any{"status": in.Status, "is_default": r.IsDefault && in.Status == "0"}), nil
	}, Undo: s.undo})
	capability.Register(capability.Capability[ProviderID, capability.Empty]{Meta: meta("remove", "删除模型服务", "DELETE", "/:providerId", "remove", true), Fn: func(ctx context.Context, in ProviderID) (capability.Empty, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return capability.Empty{}, err
		}
		if r.IsDefault {
			return capability.Empty{}, errcode.Conflictf("请先选择其他默认模型")
		}
		b, a := map[string]any{"del_flag": "0"}, map[string]any{"del_flag": "2"}
		if err = s.dbFor(ctx).Model(&model.AIProvider{}).Where("provider_id = ?", r.ProviderID).Updates(a).Error; err != nil {
			return capability.Empty{}, err
		}
		rec := capability.RecorderFrom(ctx)
		rec.Update(r.TableName(), capability.ID(r.ProviderID), r.Name, b, a)
		rec.SetUndo(undoData{Rows: []undoRow{{Table: r.TableName(), IDField: "provider_id", ID: r.ProviderID, Before: b, After: a}}})
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in ProviderID) (*capability.Preview, error) {
		r, err := s.provider(ctx, in.ProviderID)
		if err != nil {
			return nil, err
		}
		if r.IsDefault {
			return nil, errcode.Conflictf("请先选择其他默认模型")
		}
		return preview(r.TableName(), capability.ID(r.ProviderID), r.Name, capability.OpDelete, providerMap(r), nil), nil
	}, Undo: s.undo})
}
