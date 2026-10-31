// Package resources registers credential configuration and file capabilities.
// Provider/storage drivers remain independent of the capability execution layer.
package resources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/junler/intent-admin/internal/aiprovider"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/internal/storage"
)

type Service struct {
	db     *gorm.DB
	cache  *cache.Cache
	cfg    *config.Config
	cipher *secret.Cipher
}

func Register(db *gorm.DB, c *cache.Cache, cfg *config.Config, cipher *secret.Cipher) *Service {
	s := &Service{db, c, cfg, cipher}
	s.registerProviders()
	s.registerStorage()
	s.registerFiles()
	return s
}
func (s *Service) dbFor(ctx context.Context) *gorm.DB { return dbx.DBFrom(ctx, s.db) }

// Ciphertext must never reach SQL debug logs or database-error query traces.
func (s *Service) privateDB(ctx context.Context) *gorm.DB {
	return s.dbFor(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
}
func actorName(ctx context.Context) string {
	if a, ok := capability.ActorFrom(ctx); ok {
		return a.UserName
	}
	return "system"
}
func audit(ctx context.Context) model.IdentityAudit {
	now := time.Now()
	return model.IdentityAudit{CreateBy: actorName(ctx), UpdateBy: actorName(ctx), CreateTime: now, UpdateTime: now}
}
func mappedError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errcode.NotFoundf("记录不存在")
	}
	if strings.Contains(err.Error(), "23505") || errors.Is(err, gorm.ErrDuplicatedKey) {
		return errcode.Conflictf("默认配置冲突，请刷新后重试")
	}
	return errcode.Internalf("保存配置失败，请检查数据库连接")
}

type Empty struct{}
type ProviderID struct {
	ProviderID int64 `json:"providerId" uri:"providerId" form:"providerId" validate:"required,gt=0"`
}
type StorageID struct {
	StorageID int64 `json:"storageId" uri:"storageId" form:"storageId" validate:"required,gt=0"`
}

func (s *Service) provider(ctx context.Context, id int64) (model.AIProvider, error) {
	var row model.AIProvider
	err := s.dbFor(ctx).Where("provider_id = ? AND del_flag = '0'", id).First(&row).Error
	return row, mappedError(err)
}
func (s *Service) storageConfig(ctx context.Context, id int64) (model.StorageConfig, error) {
	var row model.StorageConfig
	err := s.dbFor(ctx).Where("storage_id = ? AND del_flag = '0'", id).First(&row).Error
	return row, mappedError(err)
}
func (s *Service) providerCfg(row model.AIProvider) (aiprovider.Config, error) {
	key, err := s.cipher.Decrypt(row.APIKeyEnc)
	if err != nil {
		return aiprovider.Config{}, err
	}
	return aiprovider.Config{Vendor: row.Vendor, Adapter: row.Adapter, BaseURL: row.BaseURL, APIKey: key, Model: row.DefaultModel, Temperature: row.Temperature, MaxTokens: row.MaxTokens, TimeoutSec: row.TimeoutSec}, nil
}
func (s *Service) driver(row model.StorageConfig) (storage.Driver, error) {
	cfg, err := s.storageCfg(row)
	if err != nil {
		return nil, err
	}
	return storage.New(cfg)
}
func (s *Service) storageCfg(row model.StorageConfig) (storage.Config, error) {
	key, err := s.cipher.Decrypt(row.SecretKeyEnc)
	if err != nil {
		return storage.Config{}, err
	}
	return storage.Config{Driver: row.Driver, Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket, AccessKeyID: row.AccessKeyID, SecretKey: key, BaseURL: row.BaseURL, URLMode: row.URLMode, LocalDir: row.LocalDir, UseSSL: row.UseSSL, PathStyle: row.PathStyle, PresignExpireSec: row.PresignExpireSec}, nil
}

// ProviderModels is shared with AI clients without exposing database credentials.
type ProviderModels = aiprovider.ModelInfo

func (s *Service) Models(ctx context.Context) ([]aiprovider.ModelInfo, error) {
	var rows []model.AIProvider
	if err := s.dbFor(ctx).Where("status = '0' AND del_flag = '0' AND supports_tool_calling = true AND last_tested_at IS NOT NULL").Order("is_default DESC, sort ASC, provider_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []aiprovider.ModelInfo{}
	for _, r := range rows {
		models := []string{}
		_ = json.Unmarshal(r.Models, &models)
		out = append(out, aiprovider.ModelInfo{ProviderID: r.ProviderID, Name: r.Name, DefaultModel: r.DefaultModel, Models: models, IsDefault: r.IsDefault})
	}
	return out, nil
}
func (s *Service) ChatModel(ctx context.Context, selection string) (einomodel.ToolCallingChatModel, error) {
	var row model.AIProvider
	q := s.dbFor(ctx).Where("status = '0' AND del_flag = '0' AND supports_tool_calling = true AND last_tested_at IS NOT NULL")
	override := selection
	if prefix, rest, ok := strings.Cut(selection, ":"); ok {
		if id, err := strconv.ParseInt(prefix, 10, 64); err == nil {
			q = q.Where("provider_id = ?", id)
			override = rest
		}
	}
	err := q.Order("is_default DESC, sort ASC, provider_id ASC").First(&row).Error
	if err != nil {
		return nil, errcode.BadRequestf("尚无已测试且启用的模型服务，请在模型服务页面配置")
	}
	cfg, err := s.providerCfg(row)
	if err != nil {
		return nil, err
	}
	if override != "" {
		cfg.Model = override
	}
	return aiprovider.BuildChatModel(ctx, cfg)
}

// Bootstrap is transactional and does not overwrite existing administrator settings.
func (s *Service) Bootstrap(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ctx = dbx.WithTx(ctx, tx)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(74190031)).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.StorageConfig{}).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			b := s.cfg.Storage.Bootstrap
			driver := b.Driver
			if driver == "" {
				driver = "local"
			}
			enc, err := s.cipher.Encrypt(b.SecretKey)
			if err != nil {
				return err
			}
			row := model.StorageConfig{Name: "初始存储", Driver: driver, Preset: "custom", Endpoint: b.Endpoint, Region: b.Region, Bucket: b.Bucket, AccessKeyID: b.AccessKeyID, SecretKeyEnc: enc, UseSSL: b.UseSSL, PathStyle: b.PathStyle, BaseURL: b.BaseURL, URLMode: b.URLMode, PresignExpireSec: b.PresignExpireSec, LocalDir: b.LocalDir, MaxSize: 20 << 20, AllowExt: json.RawMessage(`[".jpg",".jpeg",".png",".gif",".webp",".pdf",".txt",".csv",".xlsx",".zip"]`), Status: "1", DelFlag: "0", IdentityAudit: audit(ctx)}
			if row.LocalDir == "" {
				row.LocalDir = "./uploads"
			}
			if row.URLMode == "" {
				row.URLMode = "static"
			}
			if row.PresignExpireSec <= 0 {
				row.PresignExpireSec = 3600
			}
			driverCfg, err := s.storageCfg(row)
			if err != nil {
				return err
			}
			result := storage.Probe(ctx, driverCfg)
			// A trusted bootstrap may name a browser-facing host unreachable
			// from its own container. Verify internal S3 I/O and preserve the
			// missing public-route validation as an explicit warning.
			if driver == "s3" && !result.OK && result.BucketExists && result.Writable && result.Readable && strings.Contains(result.Error, "公网 URL") {
				result.OK = true
				result.Warning = result.Error
				result.Error = ""
			}
			now := time.Now()
			row.LastTestedAt = &now
			row.LastTestResult, _ = json.Marshal(result)
			if result.OK {
				row.Status = "0"
				row.IsDefault = true
			}

			if err := s.privateDB(ctx).Create(&row).Error; err != nil {
				return mappedError(err)
			}
		}
		if err := tx.Model(&model.AIProvider{}).Count(&count).Error; err != nil {
			return err
		}
		b := s.cfg.AI.Bootstrap
		if count == 0 && b.APIKey != "" {
			vendor := b.Vendor
			if vendor == "" {
				vendor = "custom"
			}
			cfg, err := aiprovider.Normalize(aiprovider.Config{Vendor: vendor, BaseURL: b.BaseURL, APIKey: b.APIKey, Model: b.Model, TimeoutSec: 60})
			if err != nil {
				return err
			}
			enc, err := s.cipher.Encrypt(b.APIKey)
			if err != nil {
				return err
			}
			row := model.AIProvider{Name: "初始模型服务", Vendor: vendor, Adapter: cfg.Adapter, BaseURL: cfg.BaseURL, APIKeyEnc: enc, DefaultModel: cfg.Model, TimeoutSec: 60, Models: json.RawMessage(`[]`), Status: "1", DelFlag: "0", IdentityAudit: audit(ctx)}
			if err = s.privateDB(ctx).Create(&row).Error; err != nil {
				return mappedError(err)
			}
		}
		return nil
	})
}

// Rekey rotates both tables atomically, including soft-deleted rows which may be restored.
func Rekey(ctx context.Context, db *gorm.DB, oldCipher, newCipher *secret.Cipher) error {
	return db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE sys_ai_provider, sys_storage_config IN ACCESS EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		for _, spec := range []struct{ table, id, column string }{{"sys_ai_provider", "provider_id", "api_key_enc"}, {"sys_storage_config", "storage_id", "secret_key_enc"}} {
			var rows []map[string]any
			if err := tx.Table(spec.table).Select(spec.id + ", " + spec.column).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				encoded, _ := row[spec.column].(string)
				plain, err := oldCipher.Decrypt(encoded)
				if err != nil {
					return fmt.Errorf("无法解密 %s #%v，未写入任何变更: %w", spec.table, row[spec.id], err)
				}
				encrypted, err := newCipher.Encrypt(plain)
				if err != nil {
					return err
				}
				if err = tx.Table(spec.table).Where(spec.id+" = ?", row[spec.id]).Update(spec.column, encrypted).Error; err != nil {
					return errors.New("密钥轮换写入失败，事务已回退")
				}
			}
		}
		return nil
	})
}

// Only opaque vault references, never ciphertext, enter changeset undo payloads.
type undoRow struct {
	Table    string         `json:"table"`
	IDField  string         `json:"idField"`
	ID       any            `json:"id"`
	Before   map[string]any `json:"before"`
	After    map[string]any `json:"after"`
	VaultRef string         `json:"vaultRef,omitempty"`
	Created  bool           `json:"created,omitempty"`
}
type undoData struct {
	Rows []undoRow `json:"rows"`
}
type vaultData struct{ Column, Before, After string }

func (s *Service) vaultCipher() (*secret.Cipher, error) {
	key := sha256.Sum256([]byte("intent-admin:credential-vault:" + s.cfg.JWT.Secret))
	return secret.New(hex.EncodeToString(key[:]))
}
func (s *Service) vault(ctx context.Context, column, before, after string) (string, error) {
	return s.vaultGuard(ctx, column, before, after, false)
}
func (s *Service) vaultGuard(ctx context.Context, column, before, after string, force bool) (string, error) {
	if !capability.RecordsHistory() {
		return "", nil
	}
	if before == after && !force {
		return "", nil
	}
	oldPlain, err := s.cipher.Decrypt(before)
	if err != nil {
		return "", err
	}
	newPlain, err := s.cipher.Decrypt(after)
	if err != nil {
		return "", err
	}
	vaultCipher, err := s.vaultCipher()
	if err != nil {
		return "", err
	}
	oldSealed, err := vaultCipher.Encrypt(oldPlain)
	if err != nil {
		return "", err
	}
	newSealed, err := vaultCipher.Encrypt(newPlain)
	if err != nil {
		return "", err
	}
	ref := uuid.NewString()
	raw, _ := json.Marshal(vaultData{column, oldSealed, newSealed})
	ttl := time.Duration(s.cfg.Changeset.RetainDays) * 24 * time.Hour
	if ttl <= 0 {
		ttl = 90 * 24 * time.Hour
	}
	err = s.cache.Set(ctx, s.cache.Key("changeset_vault", ref), raw, ttl).Err()
	return ref, err
}
func (s *Service) undo(ctx context.Context, raw json.RawMessage) error {
	var data undoData
	if err := json.Unmarshal(raw, &data); err != nil || len(data.Rows) == 0 {
		return errcode.BadRequestf("配置回滚数据无效")
	}
	inverse := undoData{}
	err := s.dbFor(ctx).Transaction(func(tx *gorm.DB) error {
		for _, lockID := range []int64{74190032, 74190033} {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", lockID).Error; err != nil {
				return err
			}
		}
		patches := make([]map[string]any, len(data.Rows))
		for i, row := range data.Rows {
			expected := map[string]string{"sys_ai_provider": "provider_id", "sys_storage_config": "storage_id", "sys_upload": "upload_id"}
			if expected[row.Table] == "" || row.IDField != expected[row.Table] {
				return errcode.Forbiddenf("不可回滚的配置表或标识")
			}
			var current map[string]any
			if err := tx.Table(row.Table).Clauses(clause.Locking{Strength: "UPDATE"}).Where(row.IDField+" = ?", row.ID).Take(&current).Error; err != nil {
				return mappedError(err)
			}
			for field, want := range row.After {
				if !sameValue(current[field], want) {
					return errcode.Conflictf("配置字段 %s 已被后续修改，请人工核对后恢复", field)
				}
			}
			patch := map[string]any{}
			for k, v := range row.Before {
				patch[k] = v
			}
			if row.Created {
				patch = map[string]any{"del_flag": "2"}
				if row.Table != "sys_upload" {
					patch["is_default"] = false
				}
			}
			if row.Table == "sys_upload" && fmt.Sprint(patch["del_flag"]) == "0" && fmt.Sprint(current["status"]) == "2" {
				return errcode.Conflictf("文件内容已永久清理，无法回滚删除")
			}
			if row.VaultRef != "" {
				raw, err := s.cache.Get(ctx, s.cache.Key("changeset_vault", row.VaultRef)).Bytes()
				if err != nil {
					return errcode.Conflictf("密钥回滚凭据已过期，无法恢复")
				}
				var v vaultData
				if json.Unmarshal(raw, &v) != nil || (v.Column != "api_key_enc" && v.Column != "secret_key_enc") {
					return errcode.Internalf("密钥回滚凭据无效")
				}
				vaultCipher, err := s.vaultCipher()
				if err != nil {
					return err
				}
				beforePlain, err := vaultCipher.Decrypt(v.Before)
				if err != nil {
					return errcode.Conflictf("密钥回滚凭据无法解密，请核对 JWT 密钥")
				}
				afterPlain, err := vaultCipher.Decrypt(v.After)
				if err != nil {
					return err
				}
				currentEncoded, _ := current[v.Column].(string)
				currentPlain, err := s.cipher.Decrypt(currentEncoded)
				if err != nil {
					return err
				}
				if currentPlain != afterPlain {
					return errcode.Conflictf("凭证已被后续修改，不能覆盖")
				}
				restored, err := s.cipher.Encrypt(beforePlain)
				if err != nil {
					return err
				}
				patch[v.Column] = restored
			}
			invBefore, invAfter := map[string]any{}, map[string]any{}
			for field, value := range patch {
				if field == "api_key_enc" || field == "secret_key_enc" {
					continue
				}
				invBefore[field] = current[field]
				invAfter[field] = value
			}
			invRow := undoRow{Table: row.Table, IDField: row.IDField, ID: row.ID, Before: invBefore, After: invAfter}
			if row.VaultRef != "" {
				merged := map[string]any{}
				for k, v := range current {
					merged[k] = v
				}
				for k, v := range patch {
					merged[k] = v
				}
				for _, field := range configGuardFields(row.Table) {
					invRow.After[field] = merged[field]
				}
			}
			if row.VaultRef != "" {
				column := "api_key_enc"
				if row.Table == "sys_storage_config" {
					column = "secret_key_enc"
				}
				currentEncoded, _ := current[column].(string)
				restored, _ := patch[column].(string)
				ref, err := s.vault(ctx, column, currentEncoded, restored)
				if err != nil {
					return err
				}
				invRow.VaultRef = ref
			}
			inverse.Rows = append(inverse.Rows, invRow)
			patches[i] = patch
		}
		// Clear involved defaults before restoring a previous one, so row order
		// cannot trip the partial unique indexes during a swap.
		for i, row := range data.Rows {
			if _, ok := patches[i]["is_default"]; ok {
				if err := tx.Table(row.Table).Where(row.IDField+" = ?", row.ID).Update("is_default", false).Error; err != nil {
					return mappedError(err)
				}
			}
		}
		for i := len(data.Rows) - 1; i >= 0; i-- {
			row := data.Rows[i]
			if err := tx.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Table(row.Table).Where(row.IDField+" = ?", row.ID).Updates(patches[i]).Error; err != nil {
				return mappedError(err)
			}
		}
		return nil
	})
	if err == nil {
		capability.RecorderFrom(ctx).SetUndo(inverse)
	}
	return err
}
func sameValue(a, b any) bool {
	normalize := func(v any) any {
		if v == nil {
			return nil
		}
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			v = rv.Elem().Interface()
		}
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339Nano)
		}
		if text, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
				return t.UTC().Format(time.RFC3339Nano)
			}
			if value, err := strconv.ParseFloat(text, 64); err == nil {
				return value
			}
		}
		var raw []byte
		switch x := v.(type) {
		case []byte:
			raw = x
		case json.RawMessage:
			raw = x
		case string:
			if strings.HasPrefix(x, "[") || strings.HasPrefix(x, "{") {
				raw = []byte(x)
			}
		}
		if len(raw) > 0 {
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				return parsed
			}
		}
		return v
	}
	a, b = normalize(a), normalize(b)
	if reflect.DeepEqual(a, b) {
		return true
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func diffMap(before, after map[string]any) (map[string]any, map[string]any) {
	b, a := map[string]any{}, map[string]any{}
	for k, v := range after {
		if !sameValue(before[k], v) {
			b[k] = before[k]
			a[k] = v
		}
	}
	return b, a
}
func preview(table, id, label string, op capability.Op, before, after map[string]any) *capability.Preview {
	p := &capability.Preview{Reversible: true}
	fields := []capability.FieldDiff{}
	for k, a := range after {
		if capability.IsSensitive(k) {
			continue
		}
		if !sameValue(before[k], a) {
			fields = append(fields, capability.FieldDiff{Field: k, Label: resourceLabels[k], Before: before[k], After: a})
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Field < fields[j].Field })
	if op == capability.OpUpdate && len(fields) == 0 {
		return p
	}
	p.AddChange(capability.Change{Entity: table, ID: id, Label: label, Op: op, Fields: fields})
	return p
}

func (s *Service) lockProvider(ctx context.Context, id int64) (model.AIProvider, error) {
	var row model.AIProvider
	err := s.privateDB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_id = ? AND del_flag = '0'", id).First(&row).Error
	return row, mappedError(err)
}
func (s *Service) lockStorage(ctx context.Context, id int64) (model.StorageConfig, error) {
	var row model.StorageConfig
	err := s.privateDB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("storage_id = ? AND del_flag = '0'", id).First(&row).Error
	return row, mappedError(err)
}

func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

var resourceLabels = map[string]string{"name": "名称", "vendor": "厂商", "adapter": "适配器", "base_url": "服务地址", "default_model": "默认模型", "temperature": "温度", "max_tokens": "最大输出长度", "timeout_sec": "超时（秒）", "status": "状态", "is_default": "默认配置", "del_flag": "删除状态", "supports_tool_calling": "支持工具调用", "last_tested_at": "最近测试时间", "last_test_result": "最近测试结果", "models": "可用模型", "driver": "存储驱动", "preset": "预设", "endpoint": "存储地址", "region": "区域", "bucket": "存储桶", "access_key_id": "访问密钥 ID", "use_ssl": "HTTPS", "path_style": "路径访问模式", "url_mode": "文件链接模式", "presign_expire_sec": "链接有效期（秒）", "local_dir": "本地目录", "max_size": "上传大小限制", "allow_ext": "允许的扩展名"}

func configGuardFields(table string) []string {
	if table == "sys_ai_provider" {
		return []string{"vendor", "adapter", "base_url", "default_model", "temperature", "max_tokens", "timeout_sec", "supports_tool_calling", "last_tested_at", "last_test_result", "models", "status", "is_default"}
	}
	return []string{"driver", "endpoint", "region", "bucket", "access_key_id", "use_ssl", "path_style", "base_url", "url_mode", "presign_expire_sec", "local_dir", "last_tested_at", "last_test_result", "status", "is_default"}
}
func configUndoRow(table, idField string, id any, b, a, before, after map[string]any, ref string) undoRow {
	row := undoRow{Table: table, IDField: idField, ID: id, Before: map[string]any{}, After: map[string]any{}, VaultRef: ref}
	for k, v := range b {
		row.Before[k] = v
	}
	for k, v := range a {
		row.After[k] = v
	}
	if ref != "" {
		for _, field := range configGuardFields(table) {
			row.After[field] = after[field]
		}
		for _, field := range []string{"last_tested_at", "last_test_result", "supports_tool_calling", "models"} {
			if value, ok := before[field]; ok {
				row.Before[field] = value
			}
		}
	}
	return row
}

func (s *Service) defaultPreview(ctx context.Context, table, idField string, id int64) (*capability.Preview, error) {
	var rows []map[string]any
	if err := s.dbFor(ctx).Table(table).Select(idField+", name, is_default").Where("del_flag = '0' AND (is_default = true OR "+idField+" = ?)", id).Order(idField + " ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &capability.Preview{Reversible: true}
	for _, r := range rows {
		target := fmt.Sprint(r[idField]) == fmt.Sprint(id)
		current, _ := r["is_default"].(bool)
		if current == target {
			continue
		}
		p := preview(table, fmt.Sprint(r[idField]), fmt.Sprint(r["name"]), capability.OpUpdate, map[string]any{"is_default": current}, map[string]any{"is_default": target})
		for _, change := range p.Changes {
			out.AddChange(change)
		}
	}
	return out, nil
}
