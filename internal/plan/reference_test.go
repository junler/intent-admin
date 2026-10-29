package plan

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestResolveRestrictedReferences(t *testing.T) {
	steps := []Step{{Result: map[string]any{"rows": []any{map[string]any{"userId": 1}, map[string]any{"userId": 2}}, "total": 2}}}
	got, err := Resolve(json.RawMessage(`{"ids":"$steps[0].result.rows[*].userId","count":"$steps[0].result.total","selected":"$ctx.selectedIds"}`), steps, map[string]any{"selectedIds": []any{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"count":2,"ids":[1,2],"selected":["x"]}` {
		t.Fatal(string(got))
	}
	for _, input := range []string{`{"id":"$steps[1].result.id"}`, `{"id":"$steps[0].result.rows[0].userId"}`, `{"id":"$ctx.password"}`, `{"id":"$steps[0].result.nope"}`} {
		if _, err := Resolve(json.RawMessage(input), steps, nil); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	_, err = Resolve(json.RawMessage(`{"id":"$steps[0].result.id"}`), []Step{{Status: "pending"}}, nil)
	if !errors.Is(err, ErrPendingReference) {
		t.Fatal(err)
	}
}

func TestPlanRejectsPersistedCredentials(t *testing.T) {
	for _, raw := range []string{`{"password":"plaintext"}`, `{"jobParams":"{\"apiKey\":\"plaintext\"}"}`, `{"nested":{"secret_key":"plaintext"}}`} {
		if err := rejectSensitiveArguments(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted secret-bearing plan %s", raw)
		}
	}
	if err := rejectSensitiveArguments(json.RawMessage(`{"maxTokens":1024,"status":"0","tokenId":"session-opaque-id"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePreservesLargeIntegers(t *testing.T) {
	const id int64 = 9007199254740993
	steps := []Step{{Result: map[string]any{"rows": []any{map[string]any{"userId": id}}}}}
	got, err := Resolve(json.RawMessage(`{"direct":9007199254740993,"ids":"$steps[0].result.rows[*].userId","selected":"$ctx.selectedIds"}`), steps, map[string]any{"selectedIds": []int64{id}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"direct":9007199254740993,"ids":[9007199254740993],"selected":[9007199254740993]}` {
		t.Fatalf("resolved identifiers changed: %s", got)
	}
	if _, err := Resolve(json.RawMessage(`{} {}`), nil, nil); err == nil {
		t.Fatal("accepted multiple JSON values")
	}
}

func TestStoredPlanPreservesLargeIntegers(t *testing.T) {
	const id int64 = 9007199254740993
	original := &Plan{
		Steps:       []Step{{Result: map[string]any{"id": id}}},
		PageContext: map[string]any{"selectedIds": []int64{id}},
	}
	stored, err := fromRow(toRow(original))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(json.RawMessage(`{"id":"$steps[0].result.id","selected":"$ctx.selectedIds"}`), stored.Steps, stored.PageContext)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"id":9007199254740993,"selected":[9007199254740993]}` {
		t.Fatalf("stored identifiers changed: %s", got)
	}
}
