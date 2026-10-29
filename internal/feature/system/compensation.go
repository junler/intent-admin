package system

import (
	"context"

	"github.com/junler/intent-admin/internal/capability/undo"
)

// AuthorizeUndo is installed on the changeset service so rollback-of-rollback
// cannot bypass account, administrator, menu and reference policies.
func AuthorizeUndo(s *Service) func(context.Context, undo.Payload) error {
	return func(ctx context.Context, p undo.Payload) error {
		return s.GuardCompensation(ctx, p.Entity, p.ID, string(p.Op), p.Before, p.After, principal(ctx))
	}
}
