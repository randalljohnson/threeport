package v0

import "testing"

// TestResolveKindAPIHostPort exercises the kind host port resolution helper
// across the cases callers depend on: an explicit override taking
// precedence regardless of auth, and no override falling back to the
// auth-derived default for both auth states.
func TestResolveKindAPIHostPort(t *testing.T) {
	tests := []struct {
		name              string
		authEnabled       bool
		apiServerHostPort int
		wantPort          int
	}{
		{
			name:              "explicit override takes precedence with auth enabled",
			authEnabled:       true,
			apiServerHostPort: 8443,
			wantPort:          8443,
		},
		{
			name:              "explicit override takes precedence with auth disabled",
			authEnabled:       false,
			apiServerHostPort: 8443,
			wantPort:          8443,
		},
		{
			name:              "no override falls back to auth-enabled default",
			authEnabled:       true,
			apiServerHostPort: 0,
			wantPort:          443,
		},
		{
			name:              "no override falls back to auth-disabled default",
			authEnabled:       false,
			apiServerHostPort: 0,
			wantPort:          80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// action: resolve the host port for the given auth state and override
			gotPort := ResolveKindAPIHostPort(tt.authEnabled, tt.apiServerHostPort)

			// assertion: resolved port matches expectation
			if gotPort != tt.wantPort {
				t.Errorf("expected port %d, got %d", tt.wantPort, gotPort)
			}
		})
	}
}
