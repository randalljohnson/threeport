package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
)

// testLifecycleConfig returns a LifecycleConfig with production-default
// stale ack threshold and semaphore capacity but test-friendly persist
// settings, for per-test overrides via setLifecycleConfig.
func testLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		StaleAckThreshold: 240 * time.Second,
		RefreshInterval:   time.Hour,
		SemaphoreCapacity: 5,
		PersistRetries:    1,
		PersistRetryDelay: time.Millisecond,
	}
}

// TestCheckStaleAck_Boundary pins the strict greater-than comparison in
// stale ack detection: an ack aged exactly at the threshold is not yet
// stale, only ages strictly beyond the threshold are.
func TestCheckStaleAck_Boundary(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	restoreClock := setLifecycleClock(clk)
	t.Cleanup(restoreClock)

	restoreConfig := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreConfig)

	cases := []struct {
		name      string
		ackAge    time.Duration
		wantStale bool
	}{
		{
			name:      "ack aged 239s is not stale",
			ackAge:    239 * time.Second,
			wantStale: false,
		},
		{
			name:      "ack aged exactly 240s is not stale",
			ackAge:    240 * time.Second,
			wantStale: false,
		},
		{
			name:      "ack aged 240s plus 100ms is stale",
			ackAge:    240*time.Second + 100*time.Millisecond,
			wantStale: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ackTimestamp := clk.Now().Add(-tc.ackAge)
			assert.Equal(t, tc.wantStale, checkStaleAck(ackTimestamp))
		})
	}
}

// TestVerifyState_NilEmptyInvalid pins the three early-reject branches
// of state verification: nil pointer, zero-length bytes, and bytes that
// fail JSON parsing.
func TestVerifyState_NilEmptyInvalid(t *testing.T) {
	cases := []struct {
		name       string
		state      *datatypes.JSON
		wantErrSub string
	}{
		{
			name:       "nil state is rejected",
			state:      nil,
			wantErrSub: "state is nil",
		},
		{
			name:       "empty state is rejected",
			state:      jsonPtr(""),
			wantErrSub: "state is empty",
		},
		{
			name:       "non-JSON bytes are rejected",
			state:      jsonPtr("this is not json {{"),
			wantErrSub: "not valid JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyState(tc.state, newTestLogger())
			assert.ErrorContains(t, err, tc.wantErrSub)
		})
	}
}

// TestVerifyState_CheckpointFormat_CountsResources pins that resources
// nested under checkpoint.latest.resources are counted and a non-empty
// list passes verification.
func TestVerifyState_CheckpointFormat_CountsResources(t *testing.T) {
	state := jsonPtr(`{"checkpoint":{"latest":{"resources":[{"urn":"a"},{"urn":"b"},{"urn":"c"}]}}}`)
	assert.NoError(t, verifyState(state, newTestLogger()))
}

// TestVerifyState_DeploymentFormat pins that resources under
// deployment.resources are counted and a non-empty list passes
// verification.
func TestVerifyState_DeploymentFormat(t *testing.T) {
	state := jsonPtr(`{"deployment":{"resources":[{"urn":"a"}]}}`)
	assert.NoError(t, verifyState(state, newTestLogger()))
}

// TestVerifyState_NoResources pins the no-resources rejection branch:
// valid JSON whose checkpoint and deployment resource lists are empty
// or missing fails verification.
func TestVerifyState_NoResources(t *testing.T) {
	cases := []struct {
		name  string
		state *datatypes.JSON
	}{
		{
			name:  "both formats missing",
			state: jsonPtr(`{"other":"content"}`),
		},
		{
			name:  "checkpoint format with empty resources",
			state: jsonPtr(`{"checkpoint":{"latest":{"resources":[]}}}`),
		},
		{
			name:  "deployment format with empty resources",
			state: jsonPtr(`{"deployment":{"resources":[]}}`),
		},
		{
			name:  "both formats present with empty resources",
			state: jsonPtr(`{"checkpoint":{"latest":{"resources":[]}},"deployment":{"resources":[]}}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyState(tc.state, newTestLogger())
			assert.ErrorContains(t, err, "state contains no resources")
		})
	}
}

// TestInventoryCleared_Table pins which inventory values count as
// cleared: nil, zero-length, empty object, and JSON null are cleared;
// an object with content is not.
func TestInventoryCleared_Table(t *testing.T) {
	cases := []struct {
		name      string
		inventory *datatypes.JSON
		want      bool
	}{
		{
			name:      "nil inventory is cleared",
			inventory: nil,
			want:      true,
		},
		{
			name:      "zero-length inventory is cleared",
			inventory: jsonPtr(""),
			want:      true,
		},
		{
			name:      "empty object inventory is cleared",
			inventory: jsonPtr("{}"),
			want:      true,
		},
		{
			name:      "json null inventory is cleared",
			inventory: jsonPtr("null"),
			want:      true,
		},
		{
			name:      "populated inventory is not cleared",
			inventory: validStackState(),
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, inventoryCleared(tc.inventory))
		})
	}
}

// TestHasExistingState_Table pins which state values count as restorable
// existing state: nil, zero-length, empty object, and JSON null do not;
// an object with content does.
func TestHasExistingState_Table(t *testing.T) {
	cases := []struct {
		name  string
		state *datatypes.JSON
		want  bool
	}{
		{
			name:  "nil state is not existing state",
			state: nil,
			want:  false,
		},
		{
			name:  "zero-length state is not existing state",
			state: jsonPtr(""),
			want:  false,
		},
		{
			name:  "empty object state is not existing state",
			state: jsonPtr("{}"),
			want:  false,
		},
		{
			name:  "json null state is not existing state",
			state: jsonPtr("null"),
			want:  false,
		},
		{
			name:  "populated state is existing state",
			state: validStackState(),
			want:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasExistingState(tc.state))
		})
	}
}

// TestPersistFailure_SucceedsFirstTry pins the immediate-return branch:
// a persist function that succeeds on its first call is invoked exactly
// once and the retry delay is never waited out.
func TestPersistFailure_SucceedsFirstTry(t *testing.T) {
	config := testLifecycleConfig()
	config.PersistRetries = 3
	config.PersistRetryDelay = 10 * time.Second
	restore := setLifecycleConfig(config)
	t.Cleanup(restore)

	calls := 0
	persist := func() error {
		calls++
		return nil
	}

	start := time.Now()
	persistFailure(persist, newTestLogger())
	elapsed := time.Since(start)

	assert.Equal(t, 1, calls)
	assert.Less(t, elapsed, time.Second,
		"first-try success must return without waiting the retry delay")
}

// TestPersistFailure_Exhaustion pins the retry-exhaustion branch: a
// persist function that always errors is retried up to the configured
// count and the call returns normally afterward.
func TestPersistFailure_Exhaustion(t *testing.T) {
	config := testLifecycleConfig()
	config.PersistRetries = 3
	config.PersistRetryDelay = time.Millisecond
	restore := setLifecycleConfig(config)
	t.Cleanup(restore)

	calls := 0
	persist := func() error {
		calls++
		return errors.New("persist always fails")
	}

	persistFailure(persist, newTestLogger())

	assert.Equal(t, 3, calls)
}

// TestDefaultLifecycleConfig_ProductionValues pins the tunables the control
// plane actually runs on. Changing any of them changes provisioning timing
// or capacity in production, so the change has to be deliberate enough to
// update this test alongside it.
func TestDefaultLifecycleConfig_ProductionValues(t *testing.T) {
	assert.Equal(t, 240*time.Second, defaultLifecycleConfig.StaleAckThreshold)
	assert.Equal(t, 60*time.Second, defaultLifecycleConfig.RefreshInterval)
	assert.Equal(t, 5, defaultLifecycleConfig.SemaphoreCapacity)
	assert.Equal(t, 30, defaultLifecycleConfig.PersistRetries)
	assert.Equal(t, 10*time.Second, defaultLifecycleConfig.PersistRetryDelay)
}

// TestInfraSemaphore_CapacityMatchesDefaultConfig proves the pool that gates
// concurrent operations and the tunable that documents it are one value. Two
// independent literals would let a capacity change land in the config and
// never reach the pool.
func TestInfraSemaphore_CapacityMatchesDefaultConfig(t *testing.T) {
	assert.Equal(t, defaultLifecycleConfig.SemaphoreCapacity, cap(currentSemaphore()))
}
