package auth

import (
	"testing"

	"github.com/junler/intent-admin/internal/model"
)

func TestRetiredRoutesStayUnavailableWithStaleMenus(t *testing.T) {
	menus := []model.Menu{
		{MenuID: 1, MenuType: "M", Path: "/changes"},
		{MenuID: 2, ParentID: 1, MenuType: "C", Path: "/changesets"},
		{MenuID: 3, MenuType: "C", Path: "/audit"},
		{MenuID: 4, MenuType: "C", Path: "/ai/approvals"},
		{MenuID: 5, MenuType: "C", Path: "/custom-history", Component: "changesets/index"},
		{MenuID: 6, MenuType: "C", Path: "/custom-audit", Component: "/audit/index"},
		{MenuID: 7, MenuType: "C", Path: "/custom-review", Component: "ai/approvals/index"},
		{MenuID: 8, MenuType: "C", Path: "/system/user", Component: "system/user/index"},
	}
	for i := range menus {
		menus[i].Status, menus[i].DelFlag = "0", "0"
	}
	routes := BuildRoutes(menus, []string{"*:*:*"})
	if len(routes) != 1 || routes[0].Path != "/system/user" {
		t.Fatalf("retired pages leaked into authorized routes: %+v", routes)
	}
}
