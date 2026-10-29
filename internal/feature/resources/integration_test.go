package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pressly/goose/v3"
	"io"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/migrations"
)

// Run against a disposable PostgreSQL database and Redis with
// INTENT_ADMIN_RESOURCE_INTEGRATION=1. The database must be dedicated to this test.
func TestResourceIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_RESOURCE_INTEGRATION") != "1" {
		t.Skip("requires disposable PostgreSQL and Redis")
	}
	cfg, err := config.Load("../../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("INTENT_ADMIN_DB_PORT") == "" {
		cfg.DB.Port = 15432
	}
	if os.Getenv("INTENT_ADMIN_DB_NAME") == "" {
		cfg.DB.Name = "intent_resources_integration"
	}
	if !strings.Contains(cfg.DB.Name, "_integration") {
		t.Fatal("resource integration requires a dedicated database name containing _integration")
	}
	if os.Getenv("INTENT_ADMIN_DB_PASSWORD") == "" {
		cfg.DB.Password = "all-test-only"
	}
	if os.Getenv("INTENT_ADMIN_REDIS_ADDR") == "" {
		cfg.Redis.Addr = "127.0.0.1:16379"
	}
	cfg.Redis.KeyPrefix = "resources-integration:" + uuid.NewString() + ":"
	cfg.Storage.Bootstrap.LocalDir = t.TempDir()
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbx.Close(db)
	sqlDB, _ := db.DB()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("TRUNCATE sys_upload, sys_ai_provider, sys_storage_config RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cipher, err := secret.New(cfg.App.EncryptKey)
	if err != nil {
		t.Fatal(err)
	}
	svc := Register(db, c, cfg, cipher)
	ctx := capability.WithActor(context.Background(), &capability.Actor{Type: capability.ActorHuman, Source: capability.SourceHTTP, UserName: "resource-test", UserID: 1, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical})
	if err = svc.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
			return
		}
		fmt.Fprint(w, `{"id":"chat-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer upstream.Close()
	p, err := svc.saveProvider(ctx, ProviderInput{Name: "test", Vendor: "custom", BaseURL: upstream.URL, APIKey: "first-private-provider-key", DefaultModel: "test-model"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "1" || p.IsDefault || p.APIKey == "first-private-provider-key" {
		t.Fatalf("unsafe provider defaults: %+v", p)
	}
	var persisted model.AIProvider
	if err = db.First(&persisted, "provider_id = ?", p.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.APIKeyEnc == "first-private-provider-key" {
		t.Fatal("plaintext persisted")
	}
	rec := capability.NewRecorder()
	updateCtx := capability.WithRecorder(ctx, rec)
	in := providerInput(persisted)
	in.APIKey = "second-private-provider-key"
	if _, err = svc.saveProvider(updateCtx, in, false); err != nil {
		t.Fatal(err)
	}
	raw, ok := rec.UndoOverride()
	if !ok || strings.Contains(string(raw), "first-private-provider-key") || strings.Contains(string(raw), persisted.APIKeyEnc) {
		t.Fatal("undo data leaked encrypted or plaintext key")
	}
	undoRecorder := capability.NewRecorder()
	if err = svc.undo(capability.WithRecorder(ctx, undoRecorder), raw); err != nil {
		t.Fatal(err)
	}
	inverseRaw, ok := undoRecorder.UndoOverride()
	if !ok {
		t.Fatal("missing inverse compensation")
	}
	if err = svc.undo(ctx, inverseRaw); err != nil {
		t.Fatal(err)
	}
	if err = svc.undo(ctx, raw); err != nil {
		t.Fatal(err)
	}
	restored, err := svc.provider(ctx, p.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := cipher.Decrypt(restored.APIKeyEnc)
	if err != nil || plain != "first-private-provider-key" {
		t.Fatal("credential undo failed")
	}

	// Test health metadata, enabling and default swaps through the real database.
	healthRouter := gin.New()
	healthRouter.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() })
	svc.Mount(healthRouter.Group(cfg.App.BasePath))
	svc.MountPublic(healthRouter.Group(cfg.App.BasePath))
	health := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, cfg.App.BasePath+"/system/ai-provider/test", strings.NewReader(fmt.Sprintf(`{"providerId":%d}`, p.ProviderID)))
	req.Header.Set("Content-Type", "application/json")
	healthRouter.ServeHTTP(health, req)
	var healthResult struct {
		Code int `json:"code"`
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	_ = json.Unmarshal(health.Body.Bytes(), &healthResult)
	if healthResult.Code != 200 || !healthResult.Data.OK {
		t.Fatalf("provider test metadata: %s", health.Body.String())
	}
	tested, err := svc.provider(ctx, p.ProviderID)
	if err != nil || tested.LastTestedAt == nil || !tested.SupportsToolCalling {
		t.Fatal("test result not saved")
	}
	if err = db.Model(&model.AIProvider{}).Where("provider_id = ?", p.ProviderID).Update("status", "0").Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.saveProvider(ctx, ProviderInput{Name: "second", Vendor: "custom", BaseURL: upstream.URL, APIKey: "second-config-key", DefaultModel: "test-model"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.AIProvider{}).Where("provider_id = ?", second.ProviderID).Updates(map[string]any{"status": "0", "supports_tool_calling": true, "last_tested_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	setDefault := func(id int64) capability.Recorder {
		t.Helper()
		r := capability.NewRecorder()
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := svc.providerDefault(capability.WithRecorder(dbx.WithTx(ctx, tx), r), ProviderID{ProviderID: id})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	setDefault(second.ProviderID)
	defaultRec := setDefault(p.ProviderID)
	defaultUndo, _ := defaultRec.UndoOverride()
	if err = svc.undo(ctx, defaultUndo); err != nil {
		t.Fatalf("default swap undo violates uniqueness: %v", err)
	}
	tempValue := 1.234
	tempIn := providerInput(tested)
	tempIn.Temperature = &tempValue
	tempRec := capability.NewRecorder()
	if _, err = svc.saveProvider(capability.WithRecorder(ctx, tempRec), tempIn, false); err != nil {
		t.Fatal(err)
	}
	tempUndo, _ := tempRec.UndoOverride()
	if err = svc.undo(ctx, tempUndo); err != nil {
		t.Fatalf("numeric setting undo failed: %v", err)
	}
	out, err := svc.StoreReader(ctx, "报告.txt", strings.NewReader("stored-data"), "text/plain", "attach")
	if err != nil {
		t.Fatal(err)
	}
	id := out["uploadId"].(string)
	file, err := svc.file(ctx, id)
	if err != nil || file.StorageID <= 0 || file.CreateBy != "resource-test" {
		t.Fatalf("file scope: %+v err=%v", file, err)
	}
	router := gin.New()
	svc.MountPublic(router.Group(cfg.App.BasePath))
	request := httptest.NewRequest(http.MethodGet, out["url"].(string), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 200 || w.Body.String() != "stored-data" {
		t.Fatalf("local download: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, cfg.App.BasePath+"/public/files/"+id+"?expires=1&signature=invalid", nil))
	if w.Code != 401 {
		t.Fatalf("unsigned file accepted %d", w.Code)
	}
	var storageRow model.StorageConfig
	if err = db.First(&storageRow).Error; err != nil {
		t.Fatal(err)
	}
	storageRec := capability.NewRecorder()
	storageIn := storageInput(storageRow)
	storageIn.Name = "renamed"
	if _, err = svc.saveStorage(capability.WithRecorder(ctx, storageRec), storageIn, false); err != nil {
		t.Fatal(err)
	}
	undoRaw, _ := storageRec.UndoOverride()
	if err = svc.undo(ctx, undoRaw); err != nil {
		t.Fatalf("storage undo: %v", err)
	}
	if os.Getenv("INTENT_ADMIN_MINIO_TEST_ENDPOINT") != "" {
		testResourceS3(t, svc, ctx, healthRouter, storageRow.StorageID)
	}
	testNotificationsAndFilePermissions(t, svc, ctx)
	nextCipher, _ := secret.New("abcdef0123456789abcdef0123456789")
	if err = Rekey(ctx, db, cipher, nextCipher); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&persisted, "provider_id = ?", p.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	plain, err = nextCipher.Decrypt(persisted.APIKeyEnc)
	if err != nil || plain != "first-private-provider-key" {
		t.Fatal("rekey failed")
	}
	raw, _ = json.Marshal(persisted)
	if strings.Contains(string(raw), persisted.APIKeyEnc) {
		t.Fatal("model JSON leaks ciphertext")
	}
}

func testResourceS3(t *testing.T, svc *Service, ctx context.Context, router http.Handler, localID int64) {
	t.Helper()
	endpoint := os.Getenv("INTENT_ADMIN_MINIO_TEST_ENDPOINT")
	access, key := os.Getenv("INTENT_ADMIN_MINIO_TEST_ACCESS_KEY"), os.Getenv("INTENT_ADMIN_MINIO_TEST_SECRET_KEY")
	if access == "" || key == "" {
		t.Fatal("S3 credentials missing")
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, key, ""), Secure: false, Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "intent-resource-" + uuid.NewString()
	if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.RemoveBucket(context.Background(), bucket) }()
	public := os.Getenv("INTENT_ADMIN_MINIO_TEST_PUBLIC_ENDPOINT")
	if public == "" {
		public = endpoint
	}
	view, err := svc.saveStorage(ctx, StorageInput{Name: "S3 integration", Driver: "s3", Preset: "minio", Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, AccessKeyID: access, SecretKey: key, UseSSL: false, PathStyle: true, BaseURL: "http://" + public + "/" + bucket, URLMode: "presigned", PresignExpireSec: 60}, true)
	if err != nil {
		t.Fatal(err)
	}
	if view.SecretKey == key || view.Status != "1" {
		t.Fatal("unsafe S3 creation")
	}
	request := httptest.NewRequest(http.MethodPost, svc.cfg.App.BasePath+"/system/storage/test", strings.NewReader(fmt.Sprintf(`{"storageId":%d}`, view.StorageID)))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	var result struct {
		Code int `json:"code"`
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Code != 200 || !result.Data.OK {
		t.Fatalf("S3 configuration test failed: %s", w.Body.String())
	}
	ready, err := svc.storageConfig(ctx, view.StorageID)
	if err != nil || !storageReady(ready) {
		t.Fatal("S3 test state not persisted")
	}
	if err = svc.dbFor(ctx).Model(&model.StorageConfig{}).Where("storage_id = ?", view.StorageID).Update("status", "0").Error; err != nil {
		t.Fatal(err)
	}
	setDefault := func(id int64) {
		t.Helper()
		if err := svc.dbFor(ctx).Transaction(func(tx *gorm.DB) error {
			_, err := svc.storageDefault(dbx.WithTx(ctx, tx), StorageID{StorageID: id})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	setDefault(view.StorageID)
	defer setDefault(localID)
	out, err := svc.StoreReader(ctx, "S3报告.txt", strings.NewReader("stored-on-s3"), "text/plain", "attach")
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.file(ctx, out["uploadId"].(string))
	if err != nil || file.StorageID != view.StorageID {
		t.Fatal("upload did not retain original storage id")
	}
	driver, err := svc.driver(ready)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = driver.Delete(context.Background(), file.ObjectKey)
		_ = svc.dbFor(ctx).Model(&model.Upload{}).Where("upload_id = ?", file.UploadID).Updates(map[string]any{"status": "2", "del_flag": "2"}).Error
	}()
	avatarID := testResourceAvatar(t, svc, ctx, router)
	avatarFile, err := svc.file(ctx, avatarID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = driver.Delete(context.Background(), avatarFile.ObjectKey)
		_ = svc.dbFor(ctx).Model(&model.Upload{}).Where("upload_id = ?", avatarID).Updates(map[string]any{"status": "2", "del_flag": "2"}).Error
	}()
	setDefault(localID)
	target, err := svc.urlFor(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || string(body) != "stored-on-s3" {
		t.Fatalf("S3 file access after default switch failed: %d", res.StatusCode)
	}
}

func testResourceAvatar(t *testing.T, svc *Service, ctx context.Context, router http.Handler) string {
	t.Helper()
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	part, err := writer.CreateFormFile("avatarfile", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(imageBytes.Bytes())
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, svc.cfg.App.BasePath+"/system/user/profile/avatar", &requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	var result struct {
		Code int `json:"code"`
		Data struct {
			UploadID string `json:"uploadId"`
			URL      string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 200 {
		t.Fatalf("avatar upload failed: %s", w.Body.String())
	}
	target, _ := url.Parse(result.Data.URL)
	if target.Query().Get("expires") != "0" {
		t.Fatal("avatar stored as expiring S3 URL")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, result.Data.URL, nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), imageBytes.Bytes()) {
		t.Fatalf("avatar content inaccessible: %d", w.Code)
	}
	return result.Data.UploadID
}
