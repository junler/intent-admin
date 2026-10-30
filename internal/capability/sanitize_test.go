package capability

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
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

func TestAuditTruncationPreservesUTF8AtEveryByteLimit(t *testing.T) {
	input := map[string]any{"label": "中文审批结果😀，确认后执行"}
	for limit := 0; limit < 90; limit++ {
		result := TruncateJSON(input, limit)
		if len(result) > limit || !utf8.ValidString(result) {
			t.Fatalf("invalid audit text at byte limit %d: %q", limit, result)
		}
	}
	if got := TruncateJSON(input, -1); got != "" {
		t.Fatalf("negative limit: %q", got)
	}
}
