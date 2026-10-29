package middleware

import "context"

type ridKey struct{}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ridKey{}, id)
}

// RequestIDFromContext reads the correlation id from a plain context, for code
// below the HTTP layer (services, tasks) that only has a context.
func RequestIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ridKey{}).(string)
	return s
}
