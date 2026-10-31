package changeset

import (
	"context"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestRuntimeWithoutHistoryDoesNotAccessStorage(t *testing.T) {
	s := NewWithoutHistory(nil)
	ctx := context.Background()
	actor := &capability.Actor{UserID: 1, ChangesetID: "old-history"}
	if id, err := s.Begin(ctx, actor, "操作"); err != nil || id != "" {
		t.Fatalf("created history group: id=%q err=%v", id, err)
	}
	if id, err := s.Persist(ctx, nil, actor, nil, nil); err != nil || id != "" {
		t.Fatalf("persisted history: id=%q err=%v", id, err)
	}
	if err := s.Finish(ctx, actor.ChangesetID, "partial"); err != nil {
		t.Fatalf("updated retired history: %v", err)
	}
}
