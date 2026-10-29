package capability

import (
	"context"
	"encoding/json"
	"strconv"
)

// Recorder collects what a capability actually changed, so the executor can
// persist a changeset and make the operation reversible.
//
// Service code calls this explicitly while writing. Capturing before/after
// automatically via GORM callbacks was considered and rejected: it is
// unreliable for bulk updates, raw SQL and cascading soft deletes. One extra
// line per write buys determinism.
type Recorder interface {
	Create(entity, id, label string, after map[string]any)
	Update(entity, id, label string, before, after map[string]any)
	Delete(entity, id, label string, before map[string]any)
	Skip(entity, id, label, reason string)
	// SetUndo supplies a custom compensation payload for operations the
	// generic undo helpers cannot express.
	SetUndo(data any)
	// Items returns everything recorded so far.
	Items() []RecordedItem
	// UndoOverride returns the custom payload set by SetUndo, if any.
	UndoOverride() (json.RawMessage, bool)
}

// RecordedItem is one row-level change, persisted as sys_changeset_item.
type RecordedItem struct {
	Entity string
	ID     string
	Label  string
	Op     Op
	Before map[string]any
	After  map[string]any
	Reason string
}

type recorder struct {
	items []RecordedItem
	undo  json.RawMessage
}

// NewRecorder returns a fresh collector.
func NewRecorder() Recorder { return &recorder{} }

func (r *recorder) Create(entity, id, label string, after map[string]any) {
	r.items = append(r.items, RecordedItem{Entity: entity, ID: id, Label: label, Op: OpCreate, After: after})
}

func (r *recorder) Update(entity, id, label string, before, after map[string]any) {
	r.items = append(r.items, RecordedItem{Entity: entity, ID: id, Label: label, Op: OpUpdate, Before: before, After: after})
}

func (r *recorder) Delete(entity, id, label string, before map[string]any) {
	r.items = append(r.items, RecordedItem{Entity: entity, ID: id, Label: label, Op: OpDelete, Before: before})
}

func (r *recorder) Skip(entity, id, label, reason string) {
	r.items = append(r.items, RecordedItem{Entity: entity, ID: id, Label: label, Op: OpSkip, Reason: reason})
}

func (r *recorder) SetUndo(data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	r.undo = raw
}

func (r *recorder) Items() []RecordedItem { return r.items }

func (r *recorder) UndoOverride() (json.RawMessage, bool) {
	return r.undo, len(r.undo) > 0
}

type recorderKey struct{}

// WithRecorder returns a context carrying the recorder.
func WithRecorder(ctx context.Context, r Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// RecorderFrom returns the recorder for the current call. It never returns nil:
// a capability invoked outside the executor (for example from a unit test)
// gets a throwaway collector rather than a panic.
func RecorderFrom(ctx context.Context) Recorder {
	if r, ok := ctx.Value(recorderKey{}).(Recorder); ok && r != nil {
		return r
	}
	return NewRecorder()
}

// ID renders an identifier for the recorder, which stores IDs as strings so a
// single changeset can span tables with different key types.
func ID(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case int:
		return strconv.Itoa(n)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case uint:
		return strconv.FormatUint(uint64(n), 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
