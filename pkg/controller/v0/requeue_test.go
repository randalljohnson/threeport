package controller

import (
	"testing"
	"time"
)

// TestSetRequeueDelayNeverExceedsTheMaximum asserts the table ages return
// the expected delay and none of those values is above the maximum.
func TestSetRequeueDelayNeverExceedsTheMaximum(t *testing.T) {
	tests := []struct {
		name        string
		elapsed     int64
		expected    int64
		explanation string
	}{
		{
			name:        "an age below the initial delay returns the initial delay",
			elapsed:     0,
			expected:    DefaultInitialRequeueDelay,
			explanation: "a notification younger than the initial delay has nothing to double",
		},
		{
			name:        "a young age returns twice the elapsed time",
			elapsed:     3,
			expected:    6,
			explanation: "the delay grows with the age while the doubling stays under the maximum",
		},
		{
			name:        "an age whose doubling reaches the maximum returns the maximum",
			elapsed:     15,
			expected:    DefaultMaxRequeueDelay,
			explanation: "doubling lands exactly on the maximum",
		},
		{
			name:        "an age whose doubling passes the maximum returns the maximum",
			elapsed:     20,
			expected:    DefaultMaxRequeueDelay,
			explanation: "this is the range that previously returned above the maximum",
		},
		{
			name:        "an age above the maximum returns the maximum",
			elapsed:     600,
			expected:    DefaultMaxRequeueDelay,
			explanation: "a long-lived notification stays at the maximum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// backdate creation by this case's elapsed seconds
			creationTime := time.Now().Unix() - test.elapsed

			delay := SetRequeueDelay(&creationTime)

			if delay != test.expected {
				t.Errorf(
					"expected a delay of %d for an age of %ds, got %d: %s",
					test.expected, test.elapsed, delay, test.explanation,
				)
			}

			// reject a delay above the maximum on this case
			if delay > DefaultMaxRequeueDelay {
				t.Errorf("expected the delay never to exceed %d, got %d", DefaultMaxRequeueDelay, delay)
			}
		})
	}
}

// TestSetRequeueDelayHoldsTheCeilingAcrossEveryAge asserts that for each
// age from 0s through 120s the delay is at least the initial delay and at
// most the maximum.
func TestSetRequeueDelayHoldsTheCeilingAcrossEveryAge(t *testing.T) {
	for elapsed := int64(0); elapsed <= 120; elapsed++ {
		// backdate creation by this loop's elapsed seconds
		creationTime := time.Now().Unix() - elapsed

		delay := SetRequeueDelay(&creationTime)

		// reject a delay below the initial delay
		if delay < DefaultInitialRequeueDelay {
			t.Fatalf("expected a delay of at least %d for an age of %ds, got %d", DefaultInitialRequeueDelay, elapsed, delay)
		}
		// reject a delay above the maximum
		if delay > DefaultMaxRequeueDelay {
			t.Fatalf("expected a delay of at most %d for an age of %ds, got %d", DefaultMaxRequeueDelay, elapsed, delay)
		}
	}
}
