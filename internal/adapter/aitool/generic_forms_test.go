package aitool

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/capability"
)

type formPages struct{}

func (formPages) Routes(context.Context) ([]auth.Route, error) {
	return []auth.Route{{Path: "/test/form", Component: "test/form/index", Meta: auth.RouteMeta{Title: "表单测试"}}}, nil
}

func TestGenericCreateAndUpdateFormsNeverExecuteBusinessWrites(t *testing.T) {
	type input struct {
		Name   string `json:"name"`
		APIKey string `json:"apiKey"`
	}
	var writes atomic.Int64
	for _, operation := range []string{"list", "create", "update"} {
		method := map[string]string{"list": "GET", "create": "POST", "update": "PUT"}[operation]
		endpoint := "/test/form"
		if operation == "list" {
			endpoint += "/list"
		}
		mutating := operation != "list"
		capability.Register(capability.Capability[input, struct{}]{
			Meta:   capability.Meta{Name: "test.genericForm." + operation, Summary: "测试表单", Perm: "test:form:edit", Risk: capability.RiskMedium, AIEnabled: false, Mutating: mutating, HTTP: &capability.HTTPBinding{Method: method, Path: endpoint}},
			Fn:     func(context.Context, input) (struct{}, error) { writes.Add(1); return struct{}{}, nil },
			DryRun: func(context.Context, input) (*capability.Preview, error) { return &capability.Preview{}, nil },
		})
	}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Perms: []string{"test:form:edit"}, MaxRisk: capability.RiskLow}
	ctx := capability.WithActor(context.Background(), a)
	for _, mode := range []string{"create", "update"} {
		events := 0
		kit := &Tools{Navigation: formPages{}, Emit: func(kind string, data any) {
			if kind != "form_fill" {
				t.Fatalf("unexpected event %s", kind)
			}
			payload := data.(map[string]any)
			if payload["mode"] != mode || payload["route"] != "/test/form" {
				t.Fatal("incorrect form target")
			}
			events++
		}}
		out, err := kit.FillForm(ctx, FormInput{Name: "test.genericForm." + mode, RecordID: "42", Values: map[string]any{"name": "example", "apiKey": "test-key-only"}})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out)
		if events != 1 || writes.Load() != 0 || strings.Contains(string(raw), "test-key-only") || len(kit.FormSecrets) != 1 {
			t.Fatal("form handoff executed a write or exposed a credential in its result")
		}
	}
	kit := &Tools{Navigation: formPages{}, Emit: func(string, any) { t.Fatal("invalid request emitted form data") }}
	for _, in := range []FormInput{
		{Name: "test.genericForm.update", Values: map[string]any{"name": "missing target"}},
		{Name: "test.genericForm.create", Values: map[string]any{"unknown": "blocked"}},
		{Name: "test.genericForm.list", Values: map[string]any{"name": "not a form"}},
	} {
		if _, err := kit.FillForm(ctx, in); err == nil {
			t.Fatal("invalid form request was accepted")
		}
	}
	a.Perms = nil
	if _, err := kit.FillForm(ctx, FormInput{Name: "test.genericForm.create", Values: map[string]any{"name": "denied"}}); err == nil {
		t.Fatal("missing permission was accepted")
	}
}

func TestLocalFormManifestOnlyFillsDeclaredControls(t *testing.T) {
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Perms: []string{"test:form:fill"}}
	ctx := capability.WithActor(context.Background(), a)
	events := 0
	kit := &Tools{Navigation: formPages{}, PageContext: map[string]any{"forms": []uiForm{{Name: "ui.search", Summary: "搜索表单", Route: "/test/form", Permission: "test:form:fill", Fields: []string{"name"}}}}, Emit: func(kind string, data any) {
		if kind != "form_fill" || data.(map[string]any)["formId"] != "ui.search" {
			t.Fatal("incorrect local form handoff")
		}
		events++
	}}
	if _, err := kit.FillForm(ctx, FormInput{Name: "ui.search", Values: map[string]any{"name": "example"}}); err != nil {
		t.Fatal(err)
	}
	if events != 1 || kit.Proposed.Load() {
		t.Fatal("local form did not remain a UI-only handoff")
	}
	kit.Emit = func(string, any) { t.Fatal("invalid local form emitted values") }
	for _, in := range []FormInput{{Name: "ui.search", Values: map[string]any{"password": "blocked"}}, {Name: "ui.missing", Values: map[string]any{"name": "blocked"}}} {
		if _, err := kit.FillForm(ctx, in); err == nil {
			t.Fatal("undeclared local form input accepted")
		}
	}
	a.Perms = nil
	if _, err := kit.FillForm(ctx, FormInput{Name: "ui.search", Values: map[string]any{"name": "denied"}}); err == nil {
		t.Fatal("local form ignored permission revocation")
	}
}
