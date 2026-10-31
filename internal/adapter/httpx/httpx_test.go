package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/xuri/excelize/v2"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/service"
)

func TestBindCombinesURIQueryAndBody(t *testing.T) {
	DisableGinValidation()
	var in struct {
		ID   int64  `uri:"id" json:"id"`
		Name string `form:"name" json:"name"`
		Note string `form:"note" json:"note"`
		paginate.Query
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "7"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/items/7?name=query&note=kept&pageNum=3&Unbounded=true", strings.NewReader(`{"name":"body","Unbounded":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	if err := Bind(c, &in); err != nil {
		t.Fatal(err)
	}
	if in.ID != 7 || in.Name != "body" || in.Note != "kept" || in.PageNum != 3 || in.Unbounded {
		t.Fatalf("bound input = %+v", in)
	}
}

func TestBindRejectsMalformedInput(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, id, body string
	}{
		{"URI", "PUT", "/items/bad", "bad", ""},
		{"query", "GET", "/items?pageNum=bad", "", ""},
		{"JSON", "PUT", "/items", "", `{"name":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			if tc.id != "" {
				c.Params = gin.Params{{Key: "id", Value: tc.id}}
			}
			var in struct {
				ID int64 `uri:"id"`
				paginate.Query
			}
			err := Bind(c, &in)
			if err == nil || errcode.From(err).Code != errcode.BadRequest {
				t.Fatalf("Bind() error = %v", err)
			}
		})
	}
}

func TestBindCommaSeparatedDemoIDs(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "itemIds", Value: "1,3,5"}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/demo/item/remove/1,3,5", nil)
	var in service.DemoRemoveIn
	if err := Bind(c, &in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.ItemIDs, []int64{1, 3, 5}) {
		t.Fatalf("IDs = %v", in.ItemIDs)
	}
}

func TestWidenForExport(t *testing.T) {
	for _, maxRows := range []int{0, 1000, paginate.ExportPageSize + 1} {
		in := struct{ Nested struct{ paginate.Query } }{}
		in.Nested.PageNum = 5
		in.Nested.PageSize = 10
		if err := widenForExport(&in, maxRows); err != nil {
			t.Fatal(err)
		}
		want := maxRows
		if want <= 0 || want > paginate.ExportPageSize {
			want = paginate.ExportPageSize
		}
		if in.Nested.PageNum != 1 || in.Nested.PageSize != want || !in.Nested.Unbounded {
			t.Fatalf("export pagination = %+v", in.Nested.Query)
		}
	}
	if err := widenForExport(&struct{}{}, 10); err != ErrNoPagination {
		t.Fatalf("missing pagination: %v", err)
	}
}

type adapterInput struct {
	paginate.Query
	ID   int64  `uri:"id" json:"id" validate:"required,gt=0"`
	Name string `form:"name" json:"name"`
}

type exportRow struct {
	Name   string `json:"name"`
	Amount int64  `json:"amount"`
}

type adapterOutput struct {
	Rows []exportRow `json:"rows"`
}

var testSequence atomic.Int64

func TestMountedEndpointsUseExecutor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	DisableGinValidation()
	v := validator.New()
	capability.Install(capability.Hooks{Validate: v.Struct})
	t.Cleanup(func() { capability.Install(capability.Hooks{}) })
	name := fmt.Sprintf("httpx.test%d", testSequence.Add(1))
	writes := 0
	capability.Register(capability.Capability[adapterInput, capability.Empty]{
		Meta: capability.Meta{
			Name: name, Summary: "测试修改", Perm: "test:write", Risk: capability.RiskMedium, Mutating: true,
			HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/items/update/:id"},
		},
		Fn: func(context.Context, adapterInput) (capability.Empty, error) {
			writes++
			return capability.Empty{}, nil
		},
		DryRun: func(context.Context, adapterInput) (*capability.Preview, error) {
			return &capability.Preview{Affected: 1}, nil
		},
	})
	e, _ := capability.Lookup(name)
	r := gin.New()
	Mount(r.Group("/api"), []*capability.Entry{e}, Options{})
	full := &capability.Actor{Source: capability.SourceHTTP, Perms: []string{"test:write"}, MaxRisk: capability.RiskCritical}
	low := &capability.Actor{Source: capability.SourceHTTP, Perms: []string{"test:write"}, MaxRisk: capability.RiskLow}
	for _, tc := range []struct {
		name, method, path, body string
		actor                    *capability.Actor
		code, wantWrites         int
	}{
		{"anonymous", "PUT", "/api/items/update/7", `{}`, nil, 401, 0},
		{"permission", "PUT", "/api/items/update/7", `{}`, &capability.Actor{MaxRisk: capability.RiskCritical}, 403, 0},
		{"validation", "PUT", "/api/items/update/0", `{}`, full, 400, 0},
		{"risk", "PUT", "/api/items/update/7", `{}`, low, 423, 0},
		{"preview is read only", "POST", "/api/items/update/preview", `{"id":7}`, low, 200, 0},
		{"execute", "PUT", "/api/items/update/7", `{"name":"changed"}`, full, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(r, tc.method, tc.path, tc.body, tc.actor)
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tc.code || writes != tc.wantWrites {
				t.Fatalf("response = %s, writes = %d", w.Body.String(), writes)
			}
			if w.Code != errcode.HTTPStatus(tc.code) {
				t.Fatalf("HTTP status = %d", w.Code)
			}
		})
	}
}

func call(r http.Handler, method, path, body string, actor *capability.Actor) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if actor != nil {
		req = req.WithContext(capability.WithActor(req.Context(), actor))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestExportAndTemplateEndpoints(t *testing.T) {
	capability.Install(capability.Hooks{})
	t.Cleanup(func() { capability.Install(capability.Hooks{}) })
	var received adapterInput
	name := fmt.Sprintf("httpx.export%d", testSequence.Add(1))
	capability.Register(capability.Capability[adapterInput, adapterOutput]{
		Meta: capability.Meta{
			Name: name, Summary: "测试列表", Perm: "test:list", Risk: capability.RiskLow, AIEnabled: true,
			HTTP: &capability.HTTPBinding{Method: "GET", Path: "/items/list"},
			Export: &capability.ExportSpec{SheetName: "数据", Perm: "test:export", Columns: []capability.ExportColumn{
				{Field: "name", Label: "名称"}, {Field: "amount", Label: "金额", Format: capability.FormatMoney},
			}},
		},
		Fn: func(_ context.Context, in adapterInput) (adapterOutput, error) {
			received = in
			return adapterOutput{Rows: []exportRow{{Name: in.Name, Amount: 1990}}}, nil
		},
	})
	e, _ := capability.Lookup(name)
	r := gin.New()
	Mount(r.Group("/api"), []*capability.Entry{e}, Options{ExportMaxRows: 1000})
	full := &capability.Actor{Source: capability.SourceHTTP, Perms: []string{"test:list", "test:export"}, MaxRisk: capability.RiskCritical}
	for _, tc := range []struct {
		name, method, path string
		actor              *capability.Actor
		status             int
	}{
		{"anonymous export", "POST", "/api/items/list/export", nil, 401},
		{"missing export permission", "POST", "/api/items/list/export", &capability.Actor{Perms: []string{"test:list"}}, 403},
		{"AI export denied", "POST", "/api/items/list/export", &capability.Actor{Source: capability.SourceAI, Perms: full.Perms}, 403},
		{"anonymous template", "GET", "/api/items/list/template", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(r, tc.method, tc.path, `{}`, tc.actor)
			if w.Code != tc.status {
				t.Fatalf("HTTP status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
	for _, template := range []bool{false, true} {
		method, path := "POST", "/api/items/list/export"
		if template {
			method, path = "GET", "/api/items/list/template"
		}
		w := call(r, method, path, `{"name":"筛选结果","pageNum":5,"pageSize":10}`, full)
		if w.Code != 200 || w.Header().Get("Content-Type") != xlsxContentType {
			t.Fatalf("workbook response: %s", w.Body.String())
		}
		f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := f.GetRows("数据")
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := [][]string{{"名称", "金额"}}
		if !template {
			want = append(want, []string{"筛选结果", "19.90"})
			if received.Name != "筛选结果" || received.PageNum != 1 || received.PageSize != 1000 || !received.Unbounded {
				t.Fatalf("export did not preserve filter/widen pagination: %+v", received)
			}
		}
		if !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %v, want %v", rows, want)
		}
	}
}

func TestDemoRoutesAndOpenAPIAgree(t *testing.T) {
	if _, ok := capability.Lookup("demo.item.list"); !ok {
		demo.Register(nil, nil)
	}
	entries := []*capability.Entry{}
	for _, name := range []string{"demo.item.list", "demo.item.update", "demo.item.remove"} {
		e, _ := capability.Lookup(name)
		entries = append(entries, e)
	}
	r := gin.New()
	Mount(r.Group("/api"), entries, Options{})
	doc := OpenAPI(entries, "/api", "test")
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("OpenAPI version = %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	if len(r.Routes()) != 7 {
		t.Fatalf("routes = %v", r.Routes())
	}
	for _, route := range r.Routes() {
		item, ok := paths[normalizePath(route.Path)].(map[string]any)
		if !ok || item[strings.ToLower(route.Method)] == nil {
			t.Fatalf("missing operation: %s %s", route.Method, route.Path)
		}
	}
	for path, name := range map[string]string{
		"/api/demo/item/update/preview": "demo.item.update.preview",
		"/api/demo/item/remove/preview": "demo.item.remove.preview",
	} {
		op := paths[path].(map[string]any)["post"].(map[string]any)
		if op["operationId"] != name {
			t.Fatalf("operation at %s = %v", path, op)
		}
	}
}
