package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/capability"
)

func testNotificationsAndFilePermissions(t *testing.T, s *Service, ctx context.Context) {
	t.Helper()
	suffix := uuid.NewString()[:8]
	alice := "alice-" + suffix
	bob := "bob-" + suffix
	actor := func(name string, perms ...string) *capability.Actor {
		return &capability.Actor{UserID: 1001, UserName: name, Type: capability.ActorHuman, Source: capability.SourceHTTP, MaxRisk: capability.RiskCritical, Perms: perms}
	}
	aliceActor, bobActor := actor(alice, "demo:item:export"), actor(bob, "demo:item:export")
	ownerCtx := capability.WithActor(ctx, aliceActor)
	otherCtx := capability.WithActor(ctx, bobActor)
	ownFile, err := s.StoreReader(ownerCtx, "own-export.txt", strings.NewReader("alice export bytes"), "text/plain", "export")
	if err != nil {
		t.Fatal(err)
	}
	otherFile, err := s.StoreReader(otherCtx, "other-export.txt", strings.NewReader("bob export bytes"), "text/plain", "export")
	if err != nil {
		t.Fatal(err)
	}
	ownID, otherID := ownFile["uploadId"].(string), otherFile["uploadId"].(string)
	normalTitle := "公告-" + suffix
	disabledTitle := "停用公告-" + suffix
	rows := []map[string]any{{"notice_title": "导出完成：本人", "notice_content": fmt.Sprintf("文件编号 %s，请下载。", ownID), "create_by": alice, "status": "0"}, {"notice_title": "导出完成：其他人", "notice_content": fmt.Sprintf("文件编号 %s，请下载。", otherID), "create_by": bob, "status": "0"}, {"notice_title": "导出完成：伪造引用", "notice_content": fmt.Sprintf("文件编号 %s，请下载。", otherID), "create_by": alice, "status": "0"}, {"notice_title": normalTitle, "notice_content": "正常公告", "create_by": "system", "status": "0"}, {"notice_title": disabledTitle, "notice_content": "不可见", "create_by": "system", "status": "1"}}
	if err = s.dbFor(ctx).Table("sys_notice").Create(rows).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.dbFor(ctx).Table("sys_notice").Where("create_by IN ? OR notice_title IN ?", []string{alice, bob}, []string{normalTitle, disabledTitle}).Delete(nil).Error
	}()
	routerFor := func(a *capability.Actor) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(capability.WithActor(c.Request.Context(), a))
			c.Next()
		})
		s.Mount(r.Group(s.cfg.App.BasePath))
		return r
	}
	aliceRouter, bobRouter := routerFor(aliceActor), routerFor(bobActor)
	fetch := func(r http.Handler) []Notification {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, s.cfg.App.BasePath+"/notifications", nil))
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				Rows  []Notification `json:"rows"`
				Total int64          `json:"total"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Code != 200 {
			t.Fatalf("notification request failed: %s", w.Body.String())
		}
		return envelope.Data.Rows
	}
	notices := fetch(aliceRouter)
	if len(notices) != 2 {
		t.Fatalf("non-admin saw %d notices, want own completion notices only", len(notices))
	}
	enriched := false
	for _, n := range notices {
		if n.CreateBy != alice {
			t.Fatal("notification leaked another user's export")
		}
		if n.NoticeTitle == "导出完成：伪造引用" && (n.UploadID != "" || n.DownloadURL != "") {
			t.Fatal("unowned file was enriched from a forged notice")
		}
		if n.UploadID == ownID {
			enriched = true
			if n.DownloadURL != "/system/upload/"+ownID+"/download" {
				t.Fatal("download URL must be relative to API base")
			}
		}
	}
	if !enriched {
		t.Fatal("own export completion missing download metadata")
	}
	normalSeen := false
	for _, n := range fetch(routerFor(actor(alice, "system:notice:list"))) {
		if n.CreateBy == bob || n.NoticeTitle == disabledTitle {
			t.Fatal("notice grant exposed another export or disabled announcement")
		}
		if n.NoticeTitle == normalTitle {
			normalSeen = true
		}
	}
	if !normalSeen {
		t.Fatal("notice permission did not include announcements")
	}
	ownDownload := httptest.NewRecorder()
	aliceRouter.ServeHTTP(ownDownload, httptest.NewRequest(http.MethodGet, s.cfg.App.BasePath+"/system/upload/"+ownID+"/download", nil))
	if ownDownload.Code != 200 || ownDownload.Body.String() != "alice export bytes" {
		t.Fatal("export owner cannot download without file-admin permissions")
	}
	forbidden := httptest.NewRecorder()
	bobRouter.ServeHTTP(forbidden, httptest.NewRequest(http.MethodGet, s.cfg.App.BasePath+"/system/upload/"+ownID+"/download", nil))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("unowned download allowed: %d", forbidden.Code)
	}
	managerCtx := capability.WithActor(ctx, actor("manager-"+suffix, "system:upload:list", "system:upload:remove"))
	list, err := s.fileList(managerCtx, FileListInput{})
	if err != nil {
		t.Fatal(err)
	}
	foundOwn, foundOther := false, false
	for _, file := range list.Rows {
		foundOwn = foundOwn || file.UploadID == ownID
		foundOther = foundOther || file.UploadID == otherID
	}
	if !foundOwn || !foundOther {
		t.Fatal("global upload permission was incorrectly restricted to ownership")
	}
	payload := json.RawMessage(fmt.Sprintf(`{"uploadId":%q}`, ownID))
	if _, err = capability.InvokeJSON(ownerCtx, "system.upload.remove", payload); err == nil {
		t.Fatal("export permission unexpectedly grants file removal")
	}
	if _, err = capability.InvokeJSON(managerCtx, "system.upload.remove", payload); err != nil {
		t.Fatalf("global upload manager cannot remove another user's file: %v", err)
	}

	if err = s.CleanupExports(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	undoPayload, _ := json.Marshal(undoData{Rows: []undoRow{{Table: "sys_upload", IDField: "upload_id", ID: ownID, Before: map[string]any{"del_flag": "0"}, After: map[string]any{"del_flag": "2"}}}})
	if err = s.undo(ctx, undoPayload); err == nil || errcode.From(err).Code != errcode.Conflict {
		t.Fatalf("permanently purged export was falsely restored: %v", err)
	}
}
