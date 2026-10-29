package capability

import (
	"encoding/json"
	"strings"
)

// sensitiveFields are redacted from capability outputs, audit parameters,
// changeset snapshots and spreadsheet exports.
//
// Matching is on the lowercased field name containing one of these fragments,
// so `apiKeyEnc`, `api_key_enc` and `SecretAccessKey` are all caught without
// enumerating every spelling. Over-redacting a harmless field is a cosmetic
// bug; under-redacting a credential is a breach.
var sensitiveFields = []string{
	"password",
	"passwd",
	"salt",
	"token",
	"secret",
	"api_key",
	"apikey",
	"private_key",
	"privatekey",
	"credential",
	"_enc",
}

// Redacted replaces a sensitive value in serialised output.
const Redacted = "***"

// IsSensitive reports whether a field name should be redacted.
func IsSensitive(name string) bool {
	lower := strings.ToLower(name)
	for _, frag := range sensitiveFields {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// Sanitize walks a JSON-like value and replaces sensitive leaves with
// Redacted. It operates on the decoded representation rather than the Go
// struct so it applies uniformly to maps, structs and nested slices.
func Sanitize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsSensitive(k) {
				if val == nil || val == "" {
					out[k] = val
				} else {
					out[k] = Redacted
				}
				continue
			}
			out[k] = Sanitize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Sanitize(val)
		}
		return out
	default:
		return v
	}
}

// SanitizeValue round-trips an arbitrary Go value through JSON and redacts it.
// Used for audit parameters and API responses where the concrete type varies.
func SanitizeValue(v any) any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		// Unmarshalable values cannot be inspected, so they must not be
		// recorded either.
		return Redacted
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Redacted
	}
	return Sanitize(decoded)
}

// SanitizeMap redacts a map captured by the Recorder, dropping values of
// sensitive fields entirely. Changeset snapshots record that a credential
// changed without storing either version of it.
func SanitizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitive(k) {
			out[k] = Redacted
			continue
		}
		out[k] = Sanitize(v)
	}
	return out
}

// TruncateJSON serialises v and clips it to max bytes, for audit columns that
// have a fixed width. The marker makes a truncated payload obvious rather than
// looking like malformed JSON.
func TruncateJSON(v any, max int) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(raw) <= max {
		return string(raw)
	}
	if max <= 3 {
		return string(raw[:max])
	}
	return string(raw[:max-3]) + "..."
}
