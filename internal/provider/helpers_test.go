package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// withInfraConcurrency sizes the infrastructure concurrency semaphore for a
// test and registers a drain that runs at cleanup so no still-running kick
// outlives the test's semaphore channel. It returns nothing: tests read the
// cap back through inFlightCount and the semaphore length.
func withInfraConcurrency(t *testing.T, capacity int) {
	t.Helper()
	SetInfraConcurrency(capacity)
	t.Cleanup(func() { waitForInFlightDrain(t) })
}

// waitForInFlightDrain polls until no infrastructure step is executing and
// every semaphore slot has been released, so a test's cleanup cannot resize
// the semaphore out from under a kick that still holds a slot.
func waitForInFlightDrain(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return inFlightCount() == 0 && len(infraSemaphore) == 0
	}, 10*time.Second, 2*time.Millisecond, "infrastructure steps did not drain")
}

// TestHasExistingState_Table covers which state values count as restorable
// existing state: nil, zero-length, empty object, and JSON null do not; an
// object with content does.
func TestHasExistingState_Table(t *testing.T) {
	cases := []struct {
		name  string
		state *datatypes.JSON
		want  bool
	}{
		{name: "nil state is not existing state", state: nil, want: false},
		{name: "zero-length state is not existing state", state: jsonPtr(""), want: false},
		{name: "empty object state is not existing state", state: jsonPtr("{}"), want: false},
		{name: "json null state is not existing state", state: jsonPtr("null"), want: false},
		{name: "populated state is existing state", state: validStackState(), want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasExistingState(tc.state))
		})
	}
}

// TestCountManagedResources_EmptyAndPlaceholder covers the absent branches:
// nil, zero-length, empty object, and JSON null all report zero managed
// resources, which the observe path treats as absent infrastructure.
func TestCountManagedResources_EmptyAndPlaceholder(t *testing.T) {
	cases := []struct {
		name  string
		state *datatypes.JSON
	}{
		{name: "nil state", state: nil},
		{name: "zero-length state", state: jsonPtr("")},
		{name: "empty object state", state: jsonPtr("{}")},
		{name: "json null state", state: jsonPtr("null")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count, err := countManagedResources(tc.state)
			require.NoError(t, err)
			assert.Equal(t, 0, count)
		})
	}
}

// TestCountManagedResources_IgnoresRootAndPendingDelete covers the two
// exclusions in the count: the synthetic root stack pseudo-resource and any
// resource already marked for deletion are not counted, so a state holding
// only those reports zero.
func TestCountManagedResources_IgnoresRootAndPendingDelete(t *testing.T) {
	// a deployment with only the root stack resource counts as zero
	rootOnly := jsonPtr(`{"deployment":{"resources":[{"urn":"urn:root","type":"pulumi:pulumi:Stack"}]}}`)
	count, err := countManagedResources(rootOnly)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "the root stack pseudo-resource is not a managed resource")

	// a deployment whose only real resource is pending delete counts as zero
	pendingDelete := jsonPtr(`{"deployment":{"resources":[{"urn":"urn:root","type":"pulumi:pulumi:Stack"},{"urn":"urn:r","type":"fake:R","delete":true}]}}`)
	count, err = countManagedResources(pendingDelete)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "a resource pending deletion is on its way out, not present")
}

// TestCountManagedResources_CountsLiveResources covers the present branch: a
// deployment with one live managed resource alongside the root stack reports
// a count of one.
func TestCountManagedResources_CountsLiveResources(t *testing.T) {
	state := jsonPtr(`{"deployment":{"resources":[{"urn":"urn:root","type":"pulumi:pulumi:Stack"},{"urn":"urn:r","type":"fake:R"}]}}`)
	count, err := countManagedResources(state)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
