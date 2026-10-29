package capability

import "fmt"

// Risk answers "may an AI do this on my behalf?", which is a separate question
// from permission ("am I allowed to do this at all?"). Both must pass.
//
// The zero value is intentionally not a valid risk: a capability that forgets
// to declare one is treated as RiskCritical by Register, so the unsafe default
// is the restrictive one.
type Risk string

const (
	// RiskLow — reads, cache refreshes, previews. AI executes without asking.
	RiskLow Risk = "low"
	// RiskMedium — single-record writes, enable/disable. AI asks once.
	RiskMedium Risk = "medium"
	// RiskHigh — bulk writes, deletes, grant changes, rollbacks. AI must show
	// the diff before the user confirms.
	RiskHigh Risk = "high"
	// RiskCritical — delete a role, touch a superadmin, purge logs, change
	// one's own permissions. AI may only propose; a human must execute it in
	// the UI.
	RiskCritical Risk = "critical"
)

var riskOrder = map[Risk]int{
	RiskLow:      0,
	RiskMedium:   1,
	RiskHigh:     2,
	RiskCritical: 3,
}

// ParseRisk validates a configured risk ceiling.
func ParseRisk(s string) (Risk, error) {
	r := Risk(s)
	if _, ok := riskOrder[r]; !ok {
		return "", fmt.Errorf("无效的风险等级 %q，可选 low/medium/high/critical", s)
	}
	return r, nil
}

// Valid reports whether r is one of the four defined levels.
func (r Risk) Valid() bool {
	_, ok := riskOrder[r]
	return ok
}

// Level returns the numeric ordering, with unknown values mapping to the most
// restrictive level so comparisons fail closed.
func (r Risk) Level() int {
	if l, ok := riskOrder[r]; ok {
		return l
	}
	return riskOrder[RiskCritical]
}

// AtMost reports whether r is within the ceiling.
func (r Risk) AtMost(ceiling Risk) bool { return r.Level() <= ceiling.Level() }

// Label renders the Chinese label used in UI and error messages.
func (r Risk) Label() string {
	switch r {
	case RiskLow:
		return "低风险"
	case RiskMedium:
		return "中风险"
	case RiskHigh:
		return "高风险"
	case RiskCritical:
		return "极高风险"
	default:
		return string(r)
	}
}
