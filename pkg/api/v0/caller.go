package v0

import "context"

// CallerIdentity is the mTLS peer identity extracted from a request's
// client cert.
type CallerIdentity struct {
	CommonName         string
	Organization       string
	OrganizationalUnit string
}

// callerIdentityKey is the context key the CallerIdentity is stored under.
type callerIdentityKey struct{}

// Caller returns the CallerIdentity attached to ctx, or the zero value
// when no peer cert was on the request.
func Caller(ctx context.Context) CallerIdentity {
	id, _ := ctx.Value(callerIdentityKey{}).(CallerIdentity)
	return id
}

// WithCaller returns a child context carrying id as the mTLS peer identity.
func WithCaller(ctx context.Context, id CallerIdentity) context.Context {
	return context.WithValue(ctx, callerIdentityKey{}, id)
}
