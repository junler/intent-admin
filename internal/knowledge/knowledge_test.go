package knowledge

import (
	"context"
	"slices"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func register(t *testing.T, name, path string) {
	t.Helper()
	capability.Register(capability.Capability[struct{}, struct{}]{
		Meta: capability.Meta{Name: name, Summary: "查询", Risk: capability.RiskLow,
			HTTP: &capability.HTTPBinding{Method: "GET", Path: path}},
		Fn: func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }})
}

func TestConventionTable(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"shop.order.list", "shop_order", true},
		{"demo.item.update", "demo_item", true},
		{"toplevel", "", false},
		{"two.parts", "", false},
	} {
		got, ok := ConventionTable(tc.name)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ConventionTable(%q) = %q,%v want %q,%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// A module that names its capabilities after its table and registers an entity
// is covered without touching any central list.
func TestCheckCoverageFollowsTheNamingConvention(t *testing.T) {
	register(t, "warehouse.stock.list", "/warehouse/stock/list")
	if missing := CheckCoverage(); !slices.Contains(missing, "warehouse.stock.list") {
		t.Fatalf("uncovered module not reported: %v", missing)
	}

	RegisterEntity(EntityDef{Table: "warehouse_stock", Summary: "库存", KeyField: "stock_id", LabelField: "sku"})
	if missing := CheckCoverage(); slices.Contains(missing, "warehouse.stock.list") {
		t.Fatalf("registered entity still reported missing: %v", missing)
	}
}

// The system and monitor namespaces do not follow the convention, so they rely
// on the explicit map. Those must not be reported as gaps.
func TestCheckCoverageAcceptsTheLegacyNamespaces(t *testing.T) {
	register(t, "system.user.list", "/system/user/list")
	register(t, "monitor.operlog.list", "/monitor/operlog/list")
	missing := CheckCoverage()
	for _, name := range []string{"system.user.list", "monitor.operlog.list"} {
		if slices.Contains(missing, name) {
			t.Fatalf("%s reported missing despite an explicit mapping: %v", name, missing)
		}
	}
}
