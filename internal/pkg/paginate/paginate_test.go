package paginate

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       Query
		page, limit int
		offset      int
	}{
		{"defaults", Query{}, 1, 10, 0},
		{"negative", Query{PageNum: -2, PageSize: -1}, 1, 10, 0},
		{"page", Query{PageNum: 3, PageSize: 20}, 3, 20, 40},
		{"list ceiling", Query{PageNum: 2, PageSize: 1000}, 2, 100, 100},
		{"export", Query{PageNum: 1, PageSize: 5000, Unbounded: true}, 1, 5000, 0},
		{"export ceiling", Query{PageSize: 100000, Unbounded: true}, 1, ExportPageSize, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offset, limit := tc.query.Normalize()
			if offset != tc.offset || limit != tc.limit || tc.query.PageNum != tc.page || tc.query.PageSize != tc.limit {
				t.Fatalf("Normalize() = (%d, %d), query = %+v", offset, limit, tc.query)
			}
		})
	}
}

func dryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 user=postgres dbname=unused sslmode=disable"), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestScopesGenerateBoundedWhitelistedSQL(t *testing.T) {
	db := dryDB(t)
	for _, tc := range []struct {
		name, field, direction, want string
	}{
		{"default", "", "", "ORDER BY create_time DESC"},
		{"mapped ascending", "createdAt", "asc", "ORDER BY create_time ASC"},
		{"uppercase", "createdAt", "ASC", "ORDER BY create_time ASC"},
		{"invalid direction", "createdAt", "asc; DROP TABLE demo_item", "ORDER BY create_time DESC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := Query{PageNum: 3, PageSize: 20, OrderByColumn: tc.field, IsAsc: tc.direction}
			order, err := q.OrderScope(map[string]string{"createdAt": "create_time"}, "create_time")
			if err != nil {
				t.Fatal(err)
			}
			sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
				return tx.Table("demo_item").Scopes(order, q.Scope()).Find(&[]map[string]any{})
			})
			if !strings.Contains(sql, tc.want) || !strings.Contains(sql, "LIMIT 20 OFFSET 40") || strings.Contains(sql, "DROP") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
		})
	}
}

func TestOrderScopeRejectsUnknownColumns(t *testing.T) {
	for _, column := range []string{"unknown", "name; DROP TABLE demo_item", "create_time"} {
		q := Query{OrderByColumn: column}
		_, err := q.OrderScope(map[string]string{"createdAt": "create_time"}, "create_time")
		if err == nil || errcode.From(err).Code != errcode.BadRequest {
			t.Fatalf("column %q should be rejected, got %v", column, err)
		}
	}
}
