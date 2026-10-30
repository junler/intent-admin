package capability

import (
	"context"
	"slices"
)

// Source identifies which entry point a call arrived through. It is recorded
// on every audit row so "who changed this" distinguishes a human clicking a
// button from an agent acting on an instruction.
type Source string

const (
	SourceHTTP Source = "http" // a human operating the UI
	SourceAI   Source = "ai"   // the in-app agent
	SourceMCP  Source = "mcp"  // an external MCP client
	SourceTask Source = "task" // the scheduler
)

// Actor types, recorded as sys_oper_log.actor_type.
const (
	ActorHuman     = "human"
	ActorAgent     = "agent"
	ActorMCPClient = "mcp_client"
	ActorSystem    = "system"
)

// SuperPerm is the wildcard permission granted to the superadmin role.
const SuperPerm = "*:*:*"

// Actor is the authenticated subject of a call, carried in the context.
type Actor struct {
	Type     string
	Source   Source
	UserID   int64
	UserName string
	Perms    []string
	MaxRisk  Risk
	// ReadOnly is a delegated scope ceiling, independent of permission names.
	ReadOnly  bool
	IP        string
	UserAgent string

	// TokenID is set when Source is SourceMCP. OAuth access-token IDs are
	// negative so their rate limits and confirmations cannot collide with PATs.
	TokenID int64
	// Intent is the user's original natural-language request, set for the AI
	// and MCP entry points. It is what makes the audit trail readable months
	// later: the capability name says what happened, Intent says why.
	Intent string
	// PlanID links this call to the plan it belongs to.
	PlanID string
	// ChangesetID groups the writes of one plan (or one UI action) together.
	ChangesetID string
	// ApprovalMode records how the user authorised this: auto, confirmed,
	// diff_confirmed or manual.
	ApprovalMode string
	// ApprovedBy identifies the distinct human who approved an immutable plan.
	ApprovedBy string
}

// IsSuper reports whether the actor holds the wildcard permission.
func (a *Actor) IsSuper() bool {
	return slices.Contains(a.Perms, SuperPerm)
}

// Can reports whether the actor satisfies a permission string. An empty perm
// means "any authenticated caller".
func (a *Actor) Can(perm string) bool {
	if perm == "" {
		return true
	}
	for _, p := range a.Perms {
		if p == SuperPerm || p == perm {
			return true
		}
	}
	return false
}

type actorKey struct{}

// WithActor returns a context carrying the actor.
func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom extracts the actor from the context.
func ActorFrom(ctx context.Context) (*Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(*Actor)
	return a, ok && a != nil
}

// SystemActor builds the actor used by the scheduler and bootstrap code. It is
// capped at RiskHigh rather than RiskCritical so an automated job can never
// perform the irreversible operations reserved for a human in the UI.
func SystemActor(name string) *Actor {
	return &Actor{
		Type:         ActorSystem,
		Source:       SourceTask,
		UserName:     name,
		Perms:        []string{SuperPerm},
		MaxRisk:      RiskHigh,
		ApprovalMode: "auto",
	}
}
