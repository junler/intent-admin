package capability

import (
	"fmt"
	"strings"
)

// configPrefixes are the capability namespaces that must never be writable by
// an AI or an external MCP client.
//
// The reason is specific, not general caution: these capabilities carry
// base_url / endpoint / token fields. If a model could change them, a single
// prompt injection would redirect every subsequent conversation and every
// uploaded file to an attacker's server while the system kept looking healthy.
// Token management is here too — a restricted token must not be able to mint
// an unrestricted one.
var configPrefixes = []string{
	"system.aiProvider.",
	"system.storage.",
	"system.token.",
}

// SelfCheck enforces the invariants that cannot be checked by a linter.
// bootstrap calls it after all capabilities are registered and refuses to
// start the process on failure: relying on reviewers to remember this rule is
// not good enough when the failure mode is silent data exfiltration.
func SelfCheck() error {
	var problems []string

	for _, e := range All() {
		if e.Mutating && isConfigCapability(e.Name) && (e.AIEnabled || e.MCPEnabled) {
			problems = append(problems, fmt.Sprintf(
				"%s 是配置类写操作，必须 AIEnabled=false 且 MCPEnabled=false（当前 AI=%v, MCP=%v）",
				e.Name, e.AIEnabled, e.MCPEnabled))
		}
		if e.Mutating && isConfigCapability(e.Name) && e.Risk.Level() < RiskHigh.Level() {
			problems = append(problems, fmt.Sprintf(
				"%s 是配置类写操作，Risk 至少应为 high（当前 %s）", e.Name, e.Risk))
		}
		// An AI-exposed capability with no summary or examples is a capability
		// the model will misuse. Warn rather than fail: it degrades quality,
		// not safety.
		if e.AIEnabled && e.Mutating && len(e.Examples) == 0 {
			warnf("capability %q 对 AI 开放且是写操作，但没有 Examples，模型拼参数的成功率会明显下降", e.Name)
		}
		if e.Export != nil && e.Mutating {
			problems = append(problems, fmt.Sprintf("%s 是写操作，不应声明 Export", e.Name))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("Capability 自检失败（见 docs/implementation-plan.md §4.10）:\n  - %s",
		strings.Join(problems, "\n  - "))
}

func isConfigCapability(name string) bool {
	for _, p := range configPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
