package capability

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigAuditNeverRecordsCredentialValues(t *testing.T) {
	input := map[string]any{"configId": 1, "configValue": "private-initial-password"}
	data, _ := json.Marshal(auditParameters("system.config.update", input))
	if strings.Contains(string(data), "private-initial-password") || !strings.Contains(string(data), "configId") {
		t.Fatalf("unsafe audit: %s", data)
	}
	if input["configValue"] != "private-initial-password" {
		t.Fatal("audit redaction mutated execution input")
	}
}
