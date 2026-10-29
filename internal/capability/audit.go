package capability

import "strings"

// Configuration keys can make an otherwise generic configValue a credential.
// Redact values for this namespace before recording any attempt, even when
// authorization or validation rejects it before the feature can inspect it.
func auditParameters(name string, in any) any {
	value := SanitizeValue(in)
	if !strings.HasPrefix(name, "system.config.") {
		return value
	}
	var redact func(any)
	redact = func(node any) {
		switch node := node.(type) {
		case map[string]any:
			for key, item := range node {
				if key == "configValue" || key == "config_value" {
					node[key] = Redacted
				} else {
					redact(item)
				}
			}
		case []any:
			for _, item := range node {
				redact(item)
			}
		}
	}
	redact(value)
	return value
}
