package capability

import "context"

type requestKey struct{}

// WithRequestKey binds a caller-supplied retry identifier to this invocation.
// Nested capabilities do not inherit it: a receipt describes one operation.
func WithRequestKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, requestKey{}, key)
}

func RequestKey(ctx context.Context) string {
	key, _ := ctx.Value(requestKey{}).(string)
	return key
}
