package tptdev

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

// TestRegistryNeedsStart covers every state the Docker daemon reports for a
// container, so a state needing an unpause or a recreate is never mistaken for
// one a start call resolves.
func TestRegistryNeedsStart(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{
			name:   "an exited container is started",
			status: container.StateExited,
			want:   true,
		},
		{
			name:   "a container that never ran is started",
			status: container.StateCreated,
			want:   true,
		},
		{
			name:   "a running container is left alone",
			status: container.StateRunning,
			want:   false,
		},
		{
			name:   "a paused container needs an unpause rather than a start",
			status: container.StatePaused,
			want:   false,
		},
		{
			name:   "a restarting container is already on its way up",
			status: container.StateRestarting,
			want:   false,
		},
		{
			name:   "a container being removed cannot be started",
			status: container.StateRemoving,
			want:   false,
		},
		{
			name:   "a dead container cannot be started",
			status: container.StateDead,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := registryNeedsStart(tt.status); got != tt.want {
				t.Errorf(
					"registryNeedsStart(%q) = %v, want %v",
					tt.status,
					got,
					tt.want,
				)
			}
		})
	}
}
