package capability

import (
	"encoding/json"
	"testing"
)

func TestSanitizeValuePreservesLargeIntegers(t *testing.T) {
	value := struct {
		ID     int64          `json:"id"`
		Nested map[string]any `json:"nested"`
	}{ID: 9007199254740993, Nested: map[string]any{"ids": []int64{9007199254740993}, "apiKey": "private"}}
	raw, err := json.Marshal(SanitizeValue(value))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":9007199254740993,"nested":{"apiKey":"***","ids":[9007199254740993]}}` {
		t.Fatalf("sanitized identifiers changed: %s", raw)
	}
}
