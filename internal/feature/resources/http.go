package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/junler/intent-admin/internal/aiprovider"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
	"github.com/junler/intent-admin/internal/storage"
)

func require(c *gin.Context, perm string) bool {
	actor, ok := capability.ActorFrom(c.Request.Context())
	if !ok {
		response.Abort(c, errcode.Unauthorizedf("请先登录"))
		return false
	}
	if !actor.Can(perm) {
		response.Abort(c, errcode.Forbiddenf("没有权限执行此操作"))
		return false
	}
	return true
}
func (s *Service) Mount(g *gin.RouterGroup) {
	g.GET("/notifications", s.notifications)
	g.POST("/system/ai-provider/test", s.testProvider)
	g.POST("/system/storage/test", s.testStorage)
	g.POST("/system/upload", s.upload)
	g.POST("/system/user/profile/avatar", s.avatar)
	g.GET("/system/upload/:uploadId/url", s.fileURL)
	g.GET("/system/upload/:uploadId/download", s.download)
}
func (s *Service) MountPublic(g *gin.RouterGroup) { g.GET("/public/files/:uploadId", s.publicFile) }
func providerInput(r model.AIProvider) ProviderInput {
	return ProviderInput{ProviderID: r.ProviderID, Name: r.Name, Vendor: r.Vendor, BaseURL: r.BaseURL, DefaultModel: r.DefaultModel, Temperature: r.Temperature, MaxTokens: r.MaxTokens, TimeoutSec: r.TimeoutSec, Sort: r.Sort}
}
func storageInput(r model.StorageConfig) StorageInput {
	exts := []string{}
	_ = json.Unmarshal(r.AllowExt, &exts)
	return StorageInput{StorageID: r.StorageID, Name: r.Name, Driver: r.Driver, Preset: r.Preset, Endpoint: r.Endpoint, Region: r.Region, Bucket: r.Bucket, AccessKeyID: r.AccessKeyID, UseSSL: r.UseSSL, PathStyle: r.PathStyle, BaseURL: r.BaseURL, URLMode: r.URLMode, PresignExpireSec: r.PresignExpireSec, LocalDir: r.LocalDir, MaxSize: r.MaxSize, AllowExt: exts}
}
func (s *Service) testProvider(c *gin.Context) {
	if !require(c, "system:aiprovider:edit") {
		return
	}
	var in ProviderInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if c.ShouldBindJSON(&in) != nil {
		response.Fail(c, errcode.BadRequestf("请求格式错误"))
		return
	}
	ctx := c.Request.Context()
	before := model.AIProvider{}
	var err error
	if in.ProviderID > 0 {
		before, err = s.provider(ctx, in.ProviderID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if in.Name == "" {
			key, base, modelName := in.APIKey, in.BaseURL, in.DefaultModel
			in = providerInput(before)
			in.APIKey = key
			if base != "" {
				in.BaseURL = base
			}
			if modelName != "" {
				in.DefaultModel = modelName
			}
		}
	}
	after, err := s.normalizeProvider(in, before)
	if err != nil {
		response.Fail(c, err)
		return
	}
	cfg, err := s.providerCfg(after)
	if err != nil {
		response.Fail(c, err)
		return
	}
	result := aiprovider.Probe(ctx, cfg)
	if before.ProviderID > 0 && sameProviderConnection(before, after) {
		raw, _ := json.Marshal(result)
		models, _ := json.Marshal(result.Models)
		now := time.Now()
		patch := map[string]any{"models": string(models), "supports_tool_calling": result.SupportsToolCalling, "last_tested_at": now, "last_test_result": string(raw)}
		if !result.OK {
			patch["is_default"] = false
			patch["status"] = "1"
		}
		tx := s.privateDB(ctx).Model(&model.AIProvider{}).Where("provider_id = ? AND api_key_enc = ? AND base_url = ? AND default_model = ? AND vendor = ? AND adapter = ? AND temperature IS NOT DISTINCT FROM ? AND max_tokens IS NOT DISTINCT FROM ? AND timeout_sec = ? AND del_flag = '0'", before.ProviderID, before.APIKeyEnc, before.BaseURL, before.DefaultModel, before.Vendor, before.Adapter, before.Temperature, before.MaxTokens, before.TimeoutSec).Updates(patch)
		if tx.Error != nil {
			response.Fail(c, mappedError(tx.Error))
			return
		}
		if tx.RowsAffected == 0 {
			response.Fail(c, errcode.New(errcode.StaleData, "配置已变化，请重新测试"))
			return
		}
	}
	response.OK(c, result)
}
func sameProviderConnection(a, b model.AIProvider) bool {
	return a.Vendor == b.Vendor && a.BaseURL == b.BaseURL && a.DefaultModel == b.DefaultModel && a.APIKeyEnc == b.APIKeyEnc && sameValue(a.Temperature, b.Temperature) && sameValue(a.MaxTokens, b.MaxTokens) && a.TimeoutSec == b.TimeoutSec
}
func (s *Service) testStorage(c *gin.Context) {
	if !require(c, "system:storage:edit") {
		return
	}
	var in StorageInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if c.ShouldBindJSON(&in) != nil {
		response.Fail(c, errcode.BadRequestf("请求格式错误"))
		return
	}
	ctx := c.Request.Context()
	before := model.StorageConfig{}
	var err error
	if in.StorageID > 0 {
		before, err = s.storageConfig(ctx, in.StorageID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if in.Name == "" {
			key := in.SecretKey
			in = storageInput(before)
			in.SecretKey = key
		}
	}
	after, err := s.normalizeStorage(in, before)
	if err != nil {
		response.Fail(c, err)
		return
	}
	cfg, err := s.storageCfg(after)
	if err != nil {
		response.Fail(c, err)
		return
	}
	result := storage.Probe(ctx, cfg)
	if before.StorageID > 0 && sameStorageConnection(before, after) {
		raw, _ := json.Marshal(result)
		now := time.Now()
		patch := map[string]any{"last_tested_at": now, "last_test_result": string(raw)}
		if !result.OK {
			patch["is_default"] = false
			patch["status"] = "1"
		}
		tx := s.privateDB(ctx).Model(&model.StorageConfig{}).Where("storage_id = ? AND secret_key_enc IS NOT DISTINCT FROM ? AND endpoint = ? AND bucket = ? AND local_dir = ? AND base_url = ? AND url_mode = ? AND region = ? AND access_key_id = ? AND use_ssl = ? AND path_style = ? AND presign_expire_sec = ? AND del_flag = '0'", before.StorageID, before.SecretKeyEnc, before.Endpoint, before.Bucket, before.LocalDir, before.BaseURL, before.URLMode, before.Region, before.AccessKeyID, before.UseSSL, before.PathStyle, before.PresignExpireSec).Updates(patch)
		if tx.Error != nil {
			response.Fail(c, mappedError(tx.Error))
			return
		}
		if tx.RowsAffected == 0 {
			response.Fail(c, errcode.New(errcode.StaleData, "配置已变化，请重新测试"))
			return
		}
	}
	response.OK(c, result)
}
func sameStorageConnection(a, b model.StorageConfig) bool {
	return a.Driver == b.Driver && a.Endpoint == b.Endpoint && a.Bucket == b.Bucket && a.Region == b.Region && a.LocalDir == b.LocalDir && a.BaseURL == b.BaseURL && a.URLMode == b.URLMode && a.AccessKeyID == b.AccessKeyID && a.SecretKeyEnc == b.SecretKeyEnc && a.UseSSL == b.UseSSL && a.PathStyle == b.PathStyle && a.PresignExpireSec == b.PresignExpireSec
}

// CleanupExports removes only expired export artifacts, never arbitrary uploads.
func (s *Service) CleanupExports(ctx context.Context, cutoff time.Time) error {
	var rows []model.Upload
	if err := s.dbFor(ctx).Where("biz_type = 'export' AND create_time < ? AND status <> '2'", cutoff).Limit(1000).Find(&rows).Error; err != nil {
		return err
	}
	for _, file := range rows {
		cfg, err := s.storageConfig(ctx, file.StorageID)
		if err != nil {
			return err
		}
		d, err := s.driver(cfg)
		if err != nil {
			return err
		}
		if err = d.Delete(ctx, file.ObjectKey); err != nil {
			return err
		}
		if err = s.dbFor(ctx).Model(&model.Upload{}).Where("upload_id = ?", file.UploadID).Updates(map[string]any{"del_flag": "2", "status": "2"}).Error; err != nil {
			return err
		}
	}
	return nil
}
