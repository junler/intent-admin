package capability

import "slices"

// Op is the kind of change a Preview entry describes.
type Op string

const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
	// OpSkip records a row the operation will deliberately leave alone, with a
	// reason. Surfacing skips is as important as surfacing changes: "stop 4
	// users, skip 1 because it is a superadmin" is a materially different
	// outcome from "stop 5 users".
	OpSkip Op = "skip"
)

// FieldDiff is one field changing value. BeforeText/AfterText carry the
// human-readable rendering for dictionary-backed fields, so the UI can show
// "正常 → 停用" instead of "0 → 1" without knowing the dictionary.
type FieldDiff struct {
	Field      string `json:"field"`
	Label      string `json:"label,omitempty"`
	Before     any    `json:"before"`
	After      any    `json:"after"`
	BeforeText string `json:"beforeText,omitempty"`
	AfterText  string `json:"afterText,omitempty"`
}

// Change is one affected record.
type Change struct {
	Entity string      `json:"entity"`
	ID     string      `json:"id"`
	Label  string      `json:"label,omitempty"`
	Op     Op          `json:"op"`
	Fields []FieldDiff `json:"fields,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

// Evidence lets the UI build a "go and check" link: the route plus the filter
// state that reproduces exactly the rows this preview is talking about.
type Evidence struct {
	Route   string         `json:"route"`
	Filters map[string]any `json:"filters,omitempty"`
}

// MaxPreviewChanges caps how many individual rows a preview enumerates. Beyond
// this the counts stay accurate but the list is truncated — a 50k-row diff is
// not reviewable anyway, and shipping it would blow up the response.
const MaxPreviewChanges = 200

// Preview is the result of a dry run: what would happen if this capability ran
// with these arguments, without having run it.
type Preview struct {
	Summary    string    `json:"summary"`
	Affected   int       `json:"affected"`
	Skipped    int       `json:"skipped"`
	Changes    []Change  `json:"changes,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	Warnings   []string  `json:"warnings,omitempty"`
	Evidence   *Evidence `json:"evidence,omitempty"`
	Reversible bool      `json:"reversible"`
	// Capability and Risk are filled in by the executor so a preview is
	// self-describing when it travels to the frontend or into a plan.
	Capability string `json:"capability,omitempty"`
	Risk       Risk   `json:"risk,omitempty"`
}

// AddChange appends a change, maintaining the counters and the truncation flag.
func (p *Preview) AddChange(c Change) {
	if c.Op == OpSkip {
		p.Skipped++
	} else {
		p.Affected++
	}
	if len(p.Changes) < MaxPreviewChanges {
		p.Changes = append(p.Changes, c)
		return
	}
	p.Truncated = true
}

// Warn appends a warning, ignoring duplicates so repeated per-row conditions
// (e.g. "跳过超管账号") surface once rather than 200 times.
func (p *Preview) Warn(msg string) {
	if slices.Contains(p.Warnings, msg) {
		return
	}
	p.Warnings = append(p.Warnings, msg)
}
