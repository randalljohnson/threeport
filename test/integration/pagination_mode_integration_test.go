//go:build integration

package main

import (
	"fmt"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
)

// TestPaginationModeAsOfSystemTimeReturnsResults asserts that the events
// endpoint answers under the as-of-system-time backend and returns a
// well-formed slice pointer.
func TestPaginationModeAsOfSystemTimeReturnsResults(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// action: force the as-of-system-time backend by threading the
	// pagination-mode query param into the events endpoint
	q := fmt.Sprintf("pagination-mode=%s&limit=5", apiserver_lib.PaginationModeAsOfSystemTime)
	events, err := client.GetEventsByQueryString(apiClient, apiAddr, q)

	// assert: the walk completes; empty result is valid
	if err != nil {
		t.Fatalf("as-of-system-time walk failed: %v", err)
	}
	if events == nil {
		t.Fatal("expected non-nil events slice pointer in as-of-system-time mode")
	}
}

// TestPaginationModeMaterializedViewReturnsResults asserts the equivalent
// answer under the materialized-view backend so the two selectors are wired
// through the handler.
func TestPaginationModeMaterializedViewReturnsResults(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	q := fmt.Sprintf("pagination-mode=%s&limit=5", apiserver_lib.PaginationModeMaterializedView)
	events, err := client.GetEventsByQueryString(apiClient, apiAddr, q)

	if err != nil {
		t.Fatalf("materialized-view walk failed: %v", err)
	}
	if events == nil {
		t.Fatal("expected non-nil events slice pointer in materialized-view mode")
	}
}

// TestPaginationModeSwitchReturnsConsistentIDSet compares the two backends
// against the same total limit and asserts the returned ID sets overlap.
// Since both modes read the same underlying rows, a caller switching between
// them should not see disjoint answers.
func TestPaginationModeSwitchReturnsConsistentIDSet(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// setup: walk both modes with the same limit so the comparable slice
	// is bounded and the two calls are close in time
	limit := "limit=20"
	aost, err := client.GetEventsByQueryString(apiClient, apiAddr, fmt.Sprintf("pagination-mode=%s&%s", apiserver_lib.PaginationModeAsOfSystemTime, limit))
	if err != nil {
		t.Fatalf("as-of-system-time walk failed: %v", err)
	}
	mv, err := client.GetEventsByQueryString(apiClient, apiAddr, fmt.Sprintf("pagination-mode=%s&%s", apiserver_lib.PaginationModeMaterializedView, limit))
	if err != nil {
		t.Fatalf("materialized-view walk failed: %v", err)
	}

	// no events at all is a valid state; skip if there's nothing to compare
	if aost == nil || mv == nil {
		t.Fatal("expected non-nil walk results")
	}
	if len(*aost) == 0 && len(*mv) == 0 {
		t.Skip("no events available; cross-mode consistency assertion is vacuous")
	}

	// assert: at least one ID should be present in both walks so the two
	// modes are demonstrably reading the same underlying rows
	aostIDs := map[uint]struct{}{}
	for _, e := range *aost {
		if e.ID != nil {
			aostIDs[*e.ID] = struct{}{}
		}
	}
	overlap := 0
	for _, e := range *mv {
		if e.ID == nil {
			continue
		}
		if _, ok := aostIDs[*e.ID]; ok {
			overlap++
		}
	}
	if overlap == 0 {
		t.Fatalf("expected overlap between pagination modes (aost=%d, mv=%d)", len(*aost), len(*mv))
	}
}
