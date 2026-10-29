package resources

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/pkg/response"
)

type FileID struct {
	UploadID string `json:"uploadId" form:"uploadId" uri:"uploadId" validate:"required,uuid"`
}
type FileListInput struct {
	paginate.Query
	FileName string `json:"fileName" form:"fileName"`
	BizType  string `json:"bizType" form:"bizType"`
}
type FileList struct {
	Rows     []model.Upload `json:"rows"`
	Total    int64          `json:"total"`
	PageNum  int            `json:"pageNum"`
	PageSize int            `json:"pageSize"`
}

func (s *Service) defaultStorage(ctx context.Context) (model.StorageConfig, error) {
	var cfg model.StorageConfig
	err := s.dbFor(ctx).Where("status = '0' AND del_flag = '0' AND last_tested_at IS NOT NULL AND last_test_result->>'ok' = 'true'").Order("is_default DESC, storage_id ASC").First(&cfg).Error
	if err != nil {
		return cfg, errcode.BadRequestf("暂无已测试且启用的存储配置")
	}
	return cfg, nil
}
func (s *Service) file(ctx context.Context, id string) (model.Upload, error) {
	var file model.Upload
	err := s.dbFor(ctx).Where("upload_id = ? AND del_flag = '0' AND status = '0'", id).First(&file).Error
	return file, mappedError(err)
}
func canReadFile(ctx context.Context, file model.Upload) bool {
	a, ok := capability.ActorFrom(ctx)
	return ok && (a.IsSuper() || a.UserName == file.CreateBy || a.Can("system:upload:query") || a.Can("system:upload:list"))
}
func (s *Service) fileList(ctx context.Context, in FileListInput) (FileList, error) {
	q := s.dbFor(ctx).Model(&model.Upload{}).Where("del_flag = '0'")
	if in.FileName != "" {
		q = q.Where("file_name ILIKE ?", "%"+in.FileName+"%")
	}
	if in.BizType != "" {
		q = q.Where("biz_type = ?", in.BizType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return FileList{}, err
	}
	var rows []model.Upload
	if err := q.Order("create_time DESC").Scopes(in.Query.Scope()).Find(&rows).Error; err != nil {
		return FileList{}, err
	}
	for i := range rows {
		if u, err := s.urlFor(ctx, rows[i]); err == nil {
			rows[i].URL = u
		}
	}
	return FileList{Rows: rows, Total: total, PageNum: in.PageNum, PageSize: in.PageSize}, nil
}
func (s *Service) signature(id, expires string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWT.Secret))
	_, _ = mac.Write([]byte("local-file:" + id + ":" + expires))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Service) localURL(file model.Upload) string {
	exp := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	// Avatar references must survive session and S3 URL expiry. Their opaque,
	// signed URL remains valid until the file is deleted or signing key rotates.
	if file.BizType == "avatar" {
		exp = "0"
	}
	return strings.TrimRight(s.cfg.App.BasePath, "/") + "/public/files/" + url.PathEscape(file.UploadID) + "?expires=" + exp + "&signature=" + s.signature(file.UploadID, exp)
}
func (s *Service) urlFor(ctx context.Context, file model.Upload) (string, error) {
	cfg, err := s.storageConfig(ctx, file.StorageID)
	if err != nil {
		return "", err
	}
	if cfg.Driver == "local" || file.BizType == "avatar" {
		return s.localURL(file), nil
	}
	d, err := s.driver(cfg)
	if err != nil {
		return "", err
	}
	return d.URL(ctx, file.ObjectKey)
}
func (s *Service) fileURL(c *gin.Context) {
	file, err := s.file(c.Request.Context(), c.Param("uploadId"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !canReadFile(c.Request.Context(), file) {
		response.Fail(c, errcode.Forbiddenf("没有权限访问此文件"))
		return
	}
	u, err := s.urlFor(c.Request.Context(), file)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, map[string]string{"url": u})
}
func (s *Service) publicFile(c *gin.Context) {
	id, exp, signature := c.Param("uploadId"), c.Query("expires"), c.Query("signature")
	expires, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || (expires != 0 && expires <= time.Now().Unix()) || !hmac.Equal([]byte(signature), []byte(s.signature(id, exp))) {
		response.Fail(c, errcode.Unauthorizedf("文件链接已过期"))
		return
	}
	file, err := s.file(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	cfg, err := s.storageConfig(c.Request.Context(), file.StorageID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if (expires == 0 && file.BizType != "avatar") || (cfg.Driver != "local" && file.BizType != "avatar") {
		response.Fail(c, errcode.NotFoundf("文件不存在"))
		return
	}
	s.stream(c, file, cfg, true)
}
func (s *Service) download(c *gin.Context) {
	file, err := s.file(c.Request.Context(), c.Param("uploadId"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !canReadFile(c.Request.Context(), file) {
		response.Fail(c, errcode.Forbiddenf("没有权限下载此文件"))
		return
	}
	cfg, err := s.storageConfig(c.Request.Context(), file.StorageID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	s.stream(c, file, cfg, false)
}
func (s *Service) stream(c *gin.Context, file model.Upload, cfg model.StorageConfig, inline bool) {
	d, err := s.driver(cfg)
	if err != nil {
		response.Fail(c, err)
		return
	}
	r, err := d.Open(c.Request.Context(), file.ObjectKey)
	if err != nil {
		response.Fail(c, errcode.NotFoundf("文件内容不存在"))
		return
	}
	defer r.Close()
	disposition := "attachment"
	if inline && slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, file.Mime) {
		disposition = "inline"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.FileName}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Cache-Control", "private, max-age=300")
	c.DataFromReader(http.StatusOK, file.Size, file.Mime, r, nil)
}
func safeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	r := []rune(name)
	if len(r) > 250 {
		name = string(r[len(r)-250:])
	}
	return name
}
func validateFile(name, mimeType string, header []byte, avatar bool) error {
	ext := strings.ToLower(filepath.Ext(name))
	if slices.Contains([]string{".html", ".htm", ".svg", ".js", ".mjs", ".exe", ".sh", ".php", ".bat", ".cmd", ".com"}, ext) {
		return errcode.BadRequestf("不允许上传可执行或主动内容文件")
	}
	if strings.HasPrefix(mimeType, "text/html") || strings.Contains(mimeType, "javascript") || mimeType == "image/svg+xml" {
		return errcode.BadRequestf("文件实际内容不安全")
	}
	if avatar || slices.Contains([]string{".jpg", ".jpeg", ".png", ".gif", ".webp"}, ext) {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(header))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
			return errcode.BadRequestf("图片格式无效或像素过大")
		}
		expected := map[string][]string{"jpeg": {".jpg", ".jpeg"}, "png": {".png"}, "gif": {".gif"}, "webp": {".webp"}}
		if !slices.Contains(expected[format], ext) {
			return errcode.BadRequestf("图片扩展名与实际格式不符")
		}
	}
	return nil
}
func (s *Service) upload(c *gin.Context) {
	if !require(c, "system:upload:add") {
		return
	}
	s.receive(c, false)
}
func (s *Service) avatar(c *gin.Context) { s.receive(c, true) }
func (s *Service) receive(c *gin.Context, avatar bool) {
	ctx := c.Request.Context()
	cfg, err := s.defaultStorage(ctx)
	if err != nil {
		response.Fail(c, err)
		return
	}
	limit := cfg.MaxSize
	if limit <= 0 || limit > 1<<30 {
		limit = 20 << 20
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+(1<<20))
	if err = c.Request.ParseMultipartForm(8 << 20); err != nil {
		response.Fail(c, errcode.BadRequestf("文件过大或上传请求无效"))
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	field := "file"
	if avatar {
		field = "avatarfile"
	}
	file, err := c.FormFile(field)
	if err != nil && avatar {
		file, err = c.FormFile("file")
	}
	if err != nil {
		response.Fail(c, errcode.BadRequestf("请选择上传文件"))
		return
	}
	if file.Size > limit {
		response.Fail(c, errcode.BadRequestf("文件超过大小限制"))
		return
	}
	r, err := file.Open()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer r.Close()
	biz := c.PostForm("bizType")
	if biz == "" {
		biz = c.PostForm("biz_type")
	}
	if avatar {
		biz = "avatar"
	}
	if biz == "" {
		biz = "attach"
	}
	if biz != "avatar" && biz != "attach" {
		response.Fail(c, errcode.BadRequestf("不允许的文件业务类型"))
		return
	}
	out, err := s.store(ctx, cfg, file.Filename, r, biz)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if avatar {
		a, _ := capability.ActorFrom(ctx)
		if err = s.dbFor(ctx).Model(&model.User{}).Where("user_id = ?", a.UserID).Update("avatar", out["url"]).Error; err != nil {
			_ = s.dbFor(ctx).Model(&model.Upload{}).Where("upload_id = ?", out["uploadId"]).Update("del_flag", "2").Error
			response.Fail(c, err)
			return
		}
	}
	response.OK(c, out)
}

// StoreReader is used by asynchronous exporters. The stream is bounded on disk.
func (s *Service) StoreReader(ctx context.Context, filename string, r io.Reader, mimeType, bizType string) (map[string]any, error) {
	if bizType != "export" && bizType != "attach" {
		return nil, errcode.BadRequestf("不允许的生成文件类型")
	}
	cfg, err := s.defaultStorage(ctx)
	if err != nil {
		return nil, err
	}
	return s.store(ctx, cfg, filename, r, bizType)
}
func (s *Service) store(ctx context.Context, cfg model.StorageConfig, name string, r io.Reader, biz string) (map[string]any, error) {
	name = safeFilename(name)
	ext := strings.ToLower(filepath.Ext(name))
	allowed := []string{}
	if json.Unmarshal(cfg.AllowExt, &allowed) != nil {
		return nil, errcode.BadRequestf("存储扩展名配置无效")
	}
	if !slices.Contains(allowed, ext) {
		return nil, errcode.BadRequestf("不允许上传 %s 文件", ext)
	}
	limit := cfg.MaxSize
	if limit <= 0 || limit > 1<<30 {
		limit = 20 << 20
	}
	tmp, err := os.CreateTemp("", "intent-upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if size == 0 || size > limit {
		return nil, errcode.BadRequestf("文件为空或超过大小限制（%d 字节）", limit)
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, _ := tmp.Read(head)
	mimeType := http.DetectContentType(head[:n])
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	imageHeader, err := io.ReadAll(io.LimitReader(tmp, 1<<20))
	if err != nil {
		return nil, err
	}
	if err = validateFile(name, mimeType, imageHeader, biz == "avatar"); err != nil {
		return nil, err
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if ext == ".xlsx" {
		mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	key := fmt.Sprintf("%s/%s/%s%s", biz, time.Now().Format("2006/01"), uuid.NewString(), ext)
	d, err := s.driver(cfg)
	if err != nil {
		return nil, err
	}
	if err = d.Put(ctx, key, tmp, size, mimeType); err != nil {
		return nil, errcode.Internalf("文件写入存储失败")
	}
	file := model.Upload{UploadID: uuid.NewString(), FileName: name, ObjectKey: key, Ext: ext, Size: size, Mime: mimeType, BizType: biz, StorageID: cfg.StorageID, Status: "0", DelFlag: "0", IdentityAudit: audit(ctx)}
	if cfg.Driver == "s3" && cfg.URLMode == "static" {
		file.URL, _ = d.URL(ctx, key)
	}
	if err = s.dbFor(ctx).Create(&file).Error; err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = d.Delete(cleanup, key)
		return nil, err
	}
	u, err := s.urlFor(ctx, file)
	if err != nil {
		return nil, err
	}
	return map[string]any{"uploadId": file.UploadID, "url": u, "fileName": file.FileName, "size": file.Size, "storageId": file.StorageID}, nil
}
func (s *Service) registerFiles() {
	capability.Register(capability.Capability[FileListInput, FileList]{Meta: capability.Meta{Name: "system.upload.list", Summary: "查询文件", Tags: []string{"文件管理"}, Perm: "system:upload:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/system/upload/list"}, AIEnabled: true, MCPEnabled: true}, Fn: s.fileList})
	capability.Register(capability.Capability[FileID, model.Upload]{Meta: capability.Meta{Name: "system.upload.get", Summary: "获取文件信息", Tags: []string{"文件管理"}, Perm: "system:upload:query", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/system/upload/:uploadId"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in FileID) (model.Upload, error) {
		file, err := s.file(ctx, in.UploadID)
		if err != nil {
			return file, err
		}
		if !canReadFile(ctx, file) {
			return file, errcode.Forbiddenf("没有文件访问权限")
		}
		file.URL, err = s.urlFor(ctx, file)
		return file, err
	}})
	capability.Register(capability.Capability[FileID, capability.Empty]{Meta: capability.Meta{Name: "system.upload.remove", Summary: "删除文件", Tags: []string{"文件管理"}, Perm: "system:upload:remove", Risk: capability.RiskHigh, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: "/system/upload/:uploadId"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in FileID) (capability.Empty, error) {
		file, err := s.file(ctx, in.UploadID)
		if err != nil {
			return capability.Empty{}, err
		}
		b, after := map[string]any{"del_flag": "0"}, map[string]any{"del_flag": "2"}
		if err = s.dbFor(ctx).Model(&model.Upload{}).Where("upload_id = ?", in.UploadID).Updates(map[string]any{"del_flag": "2", "update_time": time.Now(), "update_by": actorName(ctx)}).Error; err != nil {
			return capability.Empty{}, err
		}
		rec := capability.RecorderFrom(ctx)
		rec.Update(file.TableName(), file.UploadID, file.FileName, b, after)
		rec.SetUndo(undoData{Rows: []undoRow{{Table: file.TableName(), IDField: "upload_id", ID: file.UploadID, Before: b, After: after}}})
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in FileID) (*capability.Preview, error) {
		file, err := s.file(ctx, in.UploadID)
		if err != nil {
			return nil, err
		}
		return preview(file.TableName(), file.UploadID, file.FileName, capability.OpDelete, map[string]any{"file_name": file.FileName}, nil), nil
	}, Undo: s.undo})
}
