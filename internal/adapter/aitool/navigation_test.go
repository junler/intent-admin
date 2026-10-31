package aitool

import (
	"context"
	"testing"

	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type menuNavigation struct{ menus []model.Menu }

func (n menuNavigation) Routes(ctx context.Context) ([]auth.Route, error) {
	a, _ := capability.ActorFrom(ctx)
	return auth.BuildRoutes(n.menus, a.Perms), nil
}

func TestNavigationUsesAuthorizedMenuWithoutExposingConfigurationWrites(t *testing.T) {
	source := menuNavigation{menus: []model.Menu{
		{MenuID: 1, MenuType: "M", Path: "system", Status: "0", DelFlag: "0"},
		{MenuID: 2, ParentID: 1, MenuType: "C", MenuName: "模型服务", Path: "ai-provider", Component: "system/ai-provider/index", Perms: "system:aiProvider:list", Status: "0", DelFlag: "0", IsFrame: "1"},
		{MenuID: 3, ParentID: 1, MenuType: "C", Path: "secret", Component: "secret/index", Perms: "secret:list", Status: "0", DelFlag: "0", IsFrame: "1"},
		{MenuID: 4, MenuType: "C", Path: "https://example.com", Component: "external", Status: "0", DelFlag: "0", IsFrame: "0"},
		{MenuID: 5, MenuType: "C", Path: "disabled", Component: "disabled/index", Status: "1", DelFlag: "0", IsFrame: "1"},
	}}
	// Directories also use a local frame flag in ordinary menu data.
	source.menus[0].IsFrame = "1"
	a := &capability.Actor{Source: capability.SourceAI, UserID: 7, Perms: []string{"system:aiProvider:list"}, MaxRisk: capability.RiskLow}
	ctx := capability.WithActor(context.Background(), a)
	events := 0
	kit := &Tools{Navigation: source, Emit: func(kind string, data any) {
		if kind != "navigate" || data.(map[string]any)["route"] != "/system/ai-provider" {
			t.Fatalf("unexpected event: %s %+v", kind, data)
		}
		events++
	}}
	pages, err := kit.pages(ctx)
	if err != nil || len(pages) != 1 || pages[0].Route != "/system/ai-provider" {
		t.Fatalf("pages=%+v, err=%v", pages, err)
	}
	page, err := kit.Navigate(ctx, "/system/ai-provider")
	if err != nil || page.Title != "模型服务" || events != 1 || !kit.Navigated.Load() {
		t.Fatalf("navigation=%+v, events=%d, err=%v", page, events, err)
	}
	// Recheck the menu source for each request, including after a permission change.
	a.Perms = nil
	for _, route := range []string{"/system/ai-provider", "/system/secret", "/system", "https://example.com", "//example.com", "/disabled", "/system/ai-provider?delete=1"} {
		if _, err := kit.Navigate(ctx, route); errcode.From(err).Code != 404 {
			t.Errorf("accepted unauthorized route %q: %v", route, err)
		}
	}
	if events != 1 {
		t.Fatal("rejected navigation emitted an event")
	}
	if _, err := kit.pages(context.Background()); errcode.From(err).Code != 401 {
		t.Fatalf("missing actor accepted: %v", err)
	}
}

func TestPageSuggestionDoesNotNavigateAndRechecksPermissions(t *testing.T) {
	source := menuNavigation{menus: []model.Menu{{MenuID: 1, MenuType: "C", MenuName: "用户管理", Path: "/system/user", Component: "system/user/index", Perms: "system:user:list", Status: "0", DelFlag: "0", IsFrame: "1"}}}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Perms: []string{"system:user:list"}}
	ctx := capability.WithActor(context.Background(), a)
	kit := &Tools{Navigation: source, Emit: func(kind string, data any) { t.Fatalf("suggestion emitted %s: %+v", kind, data) }}
	page, err := kit.SuggestPage(ctx, "/system/user")
	if err != nil || page.Title != "用户管理" || kit.Navigated.Load() || len(kit.Evidence) != 1 {
		t.Fatalf("page=%+v, navigated=%v, evidence=%+v, err=%v", page, kit.Navigated.Load(), kit.Evidence, err)
	}
	a.Perms = nil
	if _, err := kit.SuggestPage(ctx, "/system/user"); errcode.From(err).Code != 404 || len(kit.Evidence) != 1 {
		t.Fatalf("unauthorized suggestion accepted: %v, evidence=%+v", err, kit.Evidence)
	}
}
