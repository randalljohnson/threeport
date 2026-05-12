package v0

import "context"

// callerCNKey is the context key the mTLS peer common name is stored under.
type callerCNKey struct{}

// CallerCN returns the mTLS peer common name attached to ctx, or "" when
// no peer cert was on the request.
func CallerCN(ctx context.Context) string {
	cn, _ := ctx.Value(callerCNKey{}).(string)
	return cn
}

// WithCallerCN returns a child context carrying cn as the mTLS peer common name.
func WithCallerCN(ctx context.Context, cn string) context.Context {
	return context.WithValue(ctx, callerCNKey{}, cn)
}
