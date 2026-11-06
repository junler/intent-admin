package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/internal/testutil"
)

func TestAttachmentOwnershipAndConcurrency(t *testing.T) {
	db := testutil.Postgres(t)
	cfg, err := config.Load("../../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.New(cfg.App.EncryptKey)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db, cfg: cfg, cipher: cipher}
	revoked := false
	s.RegisterAttachmentPolicy("test.record", AttachmentPolicy{Authorize: func(ctx context.Context, id string, write bool) error {
		a, _ := capability.ActorFrom(ctx)
		if revoked || !a.Can("record:read") || (write && !a.Can("record:write")) {
			return errcode.Forbiddenf("record denied")
		}
		if id != "1" && id != "2" {
			return errcode.NotFoundf("missing record")
		}
		return nil
	}})
	var storageID int64
	if err := db.Raw("INSERT INTO sys_storage_config(name,driver,local_dir) VALUES('attachment test','local',?) RETURNING storage_id", t.TempDir()).Scan(&storageID).Error; err != nil {
		t.Fatal(err)
	}
	files := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range files {
		owner := "owner"
		if i == 2 {
			owner = "someone-else"
		}
		if err := db.Exec("INSERT INTO sys_upload(upload_id,file_name,object_key,storage_id,create_by,biz_type) VALUES(?,?,?,?,?,'business')", id, "private.txt", id, storageID, owner).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, UserName: "owner", Source: capability.SourceHTTP, Perms: []string{"record:read", "record:write"}})
	list, err := s.attachmentList(ctx, AttachmentTarget{"test.record", "1"})
	if err != nil {
		t.Fatal(err)
	}
	in := AttachmentInput{AttachmentTarget: AttachmentTarget{"test.record", "1"}, Revision: list.Revision, UploadIDs: []string{files[0]}}
	apply := func(ctx context.Context, in AttachmentInput) error {
		return dbx.NewTxRunner(db)(ctx, func(ctx context.Context) error {
			_, err := s.replaceAttachments(capability.WithRecorder(ctx, capability.NewRecorder()), in)
			return err
		})
	}
	foreign := in
	foreign.UploadIDs = []string{files[2]}
	if err := apply(ctx, foreign); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("foreign upload accepted: %v", err)
	}
	if err := apply(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := apply(ctx, in); errcode.From(err).Code != errcode.Conflict {
		t.Fatalf("stale attachment list accepted: %v", err)
	}
	var file model.Upload
	db.First(&file, "upload_id=?", files[0])
	if !s.canReadFile(ctx, file) {
		t.Fatal("authorized record reader cannot read attachment")
	}
	outsider := capability.WithActor(context.Background(), &capability.Actor{UserName: "owner", Perms: []string{"system:upload:list", "system:upload:query"}})
	if s.canReadFile(outsider, file) {
		t.Fatal("file ownership or generic file permission bypassed record authorization")
	}
	revoked = true
	if s.canReadFile(ctx, file) {
		t.Fatal("revocation did not take effect")
	}
	revoked = false
	// Files claimed by business records never get public signed download access.
	gin.SetMode(gin.TestMode)
	router := gin.New()
	s.MountPublic(router.Group(cfg.App.BasePath))
	path := fmt.Sprintf("%s/public/files/%s?expires=0&signature=%s", cfg.App.BasePath, file.UploadID, s.signature(file.UploadID, "0"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("business file became public: %d %s", w.Code, w.Body.String())
	}

	// The actual multipart route accepts the private business type without a
	// generic file-management grant and never returns a public signed URL.
	if err := db.Exec(`UPDATE sys_storage_config SET status='0',is_default=true,allow_ext='[".txt"]'::jsonb,max_size=20971520,last_tested_at=now(),last_test_result='{"ok":true}'::jsonb WHERE storage_id=?`, storageID).Error; err != nil {
		t.Fatal(err)
	}
	uploadRouter := gin.New()
	uploadRouter.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() })
	uploadRouter.POST("/upload", s.attachmentUpload)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "route-proof.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("private upload content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload?entity=test.record&recordId=1", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	result := httptest.NewRecorder()
	uploadRouter.ServeHTTP(result, request)
	var envelope struct {
		Code int
		Data struct {
			UploadID string `json:"uploadId"`
			URL      string `json:"url"`
		}
	}
	if err := json.Unmarshal(result.Body.Bytes(), &envelope); err != nil || envelope.Code != 200 {
		t.Fatalf("private multipart upload rejected: %s / %v", result.Body.String(), err)
	}
	var stored model.Upload
	if err := db.First(&stored, "upload_id=?", envelope.Data.UploadID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BizType != "business" || stored.CreateBy != "owner" || envelope.Data.URL != cfg.App.BasePath+"/system/upload/"+stored.UploadID+"/download" {
		t.Fatal("upload lost privacy or ownership")
	}

	// Two different records race for one unclaimed file. Exactly one may bind it.
	lists := make([]AttachmentList, 2)
	for i := range 2 {
		lists[i], err = s.attachmentList(ctx, AttachmentTarget{"test.record", fmt.Sprint(i + 1)})
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- apply(ctx, AttachmentInput{AttachmentTarget: AttachmentTarget{"test.record", fmt.Sprint(i + 1)}, Revision: lists[i].Revision, UploadIDs: []string{files[1]}})
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent claims succeeded %d times", success)
	}
	var count int64
	db.Table("sys_attachment").Where("upload_id=?", files[1]).Count(&count)
	if count != 1 {
		t.Fatalf("file linked %d times", count)
	}
}
