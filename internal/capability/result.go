package capability

import "context"

// CallResult carries execution metadata without changing capability DTOs.
// Each adapter allocates one per call; parallel calls never share it.
type CallResult struct{ ChangesetID string }
type callResultKey struct{}

func WithCallResult(ctx context.Context, result *CallResult) context.Context {
	return context.WithValue(ctx, callResultKey{}, result)
}
func callResult(ctx context.Context) *CallResult {
	result, _ := ctx.Value(callResultKey{}).(*CallResult)
	return result
}
