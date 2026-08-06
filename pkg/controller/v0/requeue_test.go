package controller

import (
	"testing"
	"time"
)

// TestSetRequeueDelayNeverExceedsTheMaximum asserts that the delay grows with
// the notification's age but is never returned above the maximum, including
// for the ages whose doubling lands above it.
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
			// place the notification's creation the requested age in the past
			creationTime := time.Now().Unix() - test.elapsed

			delay := SetRequeueDelay(&creationTime)

			if delay != test.expected {
				t.Errorf(
					"expected a delay of %d for an age of %ds, got %d: %s",
					test.expected, test.elapsed, delay, test.explanation,
				)
			}

			// the maximum is a ceiling on every input, not only on the ones
			// this table names
			if delay > DefaultMaxRequeueDelay {
				t.Errorf("expected the delay never to exceed %d, got %d", DefaultMaxRequeueDelay, delay)
			}
		})
	}
}

// TestSetRequeueDelayHoldsTheCeilingAcrossEveryAge asserts the ceiling over a
// contiguous range of ages, so a future change to how the delay is derived
// cannot reintroduce a value above the maximum for some age in the middle.
func TestSetRequeueDelayHoldsTheCeilingAcrossEveryAge(t *testing.T) {
	for elapsed := int64(0); elapsed <= 120; elapsed++ {
		creationTime := time.Now().Unix() - elapsed

		delay := SetRequeueDelay(&creationTime)

		if delay < DefaultInitialRequeueDelay {
			t.Fatalf("expected a delay of at least %d for an age of %ds, got %d", DefaultInitialRequeueDelay, elapsed, delay)
		}
		if delay > DefaultMaxRequeueDelay {
			t.Fatalf("expected a delay of at most %d for an age of %ds, got %d", DefaultMaxRequeueDelay, elapsed, delay)
		}
	}
}
