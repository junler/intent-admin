package capability

import "context"

type invocationKey struct{}

func withInvocation(ctx context.Context, args any) context.Context {
	return context.WithValue(ctx, invocationKey{}, args)
}

// InvocationArguments returns the already-redacted arguments for the active
// execution, for changeset provenance. Credentials never reach this value.
func InvocationArguments(ctx context.Context) any { return ctx.Value(invocationKey{}) }
