package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// Entry is the type-erased registry record for one capability.
type Entry struct {
	Meta
	InputSchema  *jsonschema.Schema
	OutputSchema *jsonschema.Schema

	newInput func() any
	invoke   func(ctx context.Context, in any) (any, error)
	dryRun   func(ctx context.Context, in any) (*Preview, error)
	undo     func(ctx context.Context, undo json.RawMessage) error
}

// NewInput allocates a pointer to a zero input value, for the HTTP binder and
// the JSON decoder to fill.
func (e *Entry) NewInput() any { return e.newInput() }

// HasDryRun reports whether a preview is available.
func (e *Entry) HasDryRun() bool { return e.dryRun != nil }

// HasUndo reports whether compensation is available.
func (e *Entry) HasUndo() bool { return e.undo != nil }

// VisibleTo reports whether this capability should appear in the catalogue for
// a given entry point and risk ceiling.
//
// Catalogue trimming is a security feature, not a convenience: a model that
// never sees a capability cannot try to call it, which removes a whole class
// of probing behaviour.
func (e *Entry) VisibleTo(src Source, perms []string, maxRisk Risk) bool {
	switch src {
	case SourceAI:
		if !e.AIEnabled {
			return false
		}
	case SourceMCP:
		if !e.MCPEnabled {
			return false
		}
	}
	if !e.Risk.AtMost(maxRisk) {
		return false
	}
	a := Actor{Perms: perms}
	return a.Can(e.Perm)
}

var (
	mu       sync.RWMutex
	entries  = map[string]*Entry{}
	warnings []string
)

func warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	mu.Lock()
	warnings = append(warnings, msg)
	mu.Unlock()
	slog.Warn(msg)
}

// Warnings returns the non-fatal problems collected during registration, for
// the startup banner.
func Warnings() []string {
	mu.RLock()
	defer mu.RUnlock()
	return append([]string(nil), warnings...)
}

func register(e *Entry) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := entries[e.Name]; dup {
		panic(fmt.Sprintf("capability %q 重复注册", e.Name))
	}
	entries[e.Name] = e
}

// Lookup returns the entry for a capability name.
func Lookup(name string) (*Entry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := entries[name]
	return e, ok
}

// All returns every registered capability, sorted by name.
func All() []*Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Count returns the number of registered capabilities.
func Count() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(entries)
}

// Visible returns the capabilities a caller may see, already trimmed by entry
// point, permissions and risk ceiling.
func Visible(src Source, perms []string, maxRisk Risk) []*Entry {
	out := make([]*Entry, 0, 32)
	for _, e := range All() {
		if e.VisibleTo(src, perms, maxRisk) {
			out = append(out, e)
		}
	}
	return out
}

// Search filters Visible by a keyword matched against name, summary and tags.
// An empty keyword returns the head of the visible list.
func Search(src Source, perms []string, maxRisk Risk, keyword string, limit int) []*Entry {
	if limit <= 0 {
		limit = 30
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	visible := Visible(src, perms, maxRisk)
	out := make([]*Entry, 0, limit)
	for _, e := range visible {
		if kw != "" {
			haystack := strings.ToLower(e.Name + " " + e.Summary + " " + strings.Join(e.Tags, " "))
			if e.HTTP != nil {
				haystack += " " + strings.ToLower(e.HTTP.Path)
			}
			if !strings.Contains(haystack, kw) {
				continue
			}
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Descriptor is the catalogue projection handed to the AI and to MCP clients.
type Descriptor struct {
	Name        string     `json:"name"`
	Summary     string     `json:"summary"`
	Tags        []string   `json:"tags,omitempty"`
	Risk        Risk       `json:"risk"`
	Mutating    bool       `json:"mutating"`
	Reversible  bool       `json:"reversible"`
	NeedConfirm bool       `json:"needConfirm"`
	Method      string     `json:"method,omitempty"`
	Path        string     `json:"path,omitempty"`
	Examples    []Example  `json:"examples,omitempty"`
	InputSchema any        `json:"inputSchema,omitempty"`
}

// Brief is the compact form used by list_capabilities, where dozens of entries
// share the model's context budget.
func (e *Entry) Brief() Descriptor {
	d := Descriptor{
		Name:        e.Name,
		Summary:     e.Summary,
		Tags:        e.Tags,
		Risk:        e.Risk,
		Mutating:    e.Mutating,
		Reversible:  e.Reversible,
		NeedConfirm: e.Mutating,
	}
	if e.HTTP != nil {
		d.Method, d.Path = e.HTTP.Method, e.HTTP.Path
	}
	return d
}

// Describe is the full form used by describe_capability, including the input
// schema and the examples.
func (e *Entry) Describe() Descriptor {
	d := e.Brief()
	d.Examples = e.Examples
	d.InputSchema = e.InputSchema
	return d
}
