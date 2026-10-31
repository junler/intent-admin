package aitool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestFillUserFormIsAnAuthorizedHandoffWithoutCreatingAPlan(t *testing.T) {
	source := menuNavigation{menus: []model.Menu{{MenuID: 1, MenuType: "C", MenuName: "用户管理", Path: "/system/user", Component: "system/user/index", Perms: "system:user:list", Status: "0", DelFlag: "0", IsFrame: "1"}}}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Perms: []string{"system:user:list", "system:user:add"}, MaxRisk: capability.RiskLow}
	ctx := capability.WithActor(context.Background(), a)
	const password = "test-pass-42"
	events := 0
	kit := &Tools{Navigation: source, Emit: func(kind string, data any) {
		if kind != "form_fill" {
			t.Fatalf("unexpected event %s", kind)
		}
		raw, _ := json.Marshal(data)
		if !strings.Contains(string(raw), password) {
			t.Fatal("the active browser did not receive the form value")
		}
		events++
	}}
	out, err := kit.FillUserForm(ctx, UserForm{UserName: "test_user", NickName: "测试用户", Password: password})
	if err != nil || events != 1 || !kit.FilledForm.Load() || kit.Proposed.Load() {
		t.Fatal("expected a form handoff without a business plan")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), password) {
		t.Fatal("tool result retained a credential")
	}
	for _, perms := range [][]string{{"system:user:list"}, {"system:user:add"}} {
		a.Perms = perms
		if _, err := kit.FillUserForm(ctx, UserForm{Password: password}); err == nil {
			t.Fatal("form fill accepted without both page and create permissions")
		}
	}
	a.Perms = []string{"system:user:list", "system:user:add"}
	for _, in := range []UserForm{{Password: "short"}, {Sex: "01"}, {Remark: strings.Repeat("x", 501)}} {
		if _, err := kit.FillUserForm(ctx, in); errcode.From(err).Code != 400 {
			t.Fatal("invalid form fields were accepted")
		}
	}
	if events != 1 {
		t.Fatal("rejected input emitted form values")
	}
}
