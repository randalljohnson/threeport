package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestBoundEventFilterClauseEmpty verifies an unfiltered list request adds no
// predicate at all, so the paginated query keeps the shape it had before a
// filter was ever bound.
func TestBoundEventFilterClauseEmpty(t *testing.T) {
	clause, values := boundEventFilterClause(&v0.Event{})

	assert.Equal(t, "", clause)
	assert.Empty(t, values)
}

// TestBoundEventFilterClauseColumns verifies each bindable event column
// becomes a placeholder predicate carrying its value out of band. Before this,
// the paginated branches dropped these filters entirely and returned rows the
// client had asked to exclude.
func TestBoundEventFilterClauseColumns(t *testing.T) {
	tests := []struct {
		name   string
		filter v0.Event
		column string
		value  interface{}
	}{
		{"note", v0.Event{Note: util.Ptr("boom")}, "note", "boom"},
		{"count", v0.Event{Count: util.Ptr(uint(3))}, "count", uint(3)},
		{"type", v0.Event{Type: util.Ptr("Warning")}, "type", "Warning"},
		{
			"reporting controller",
			v0.Event{ReportingController: util.Ptr("kubernetes-workload-controller")},
			"reporting_controller",
			"kubernetes-workload-controller",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clause, values := boundEventFilterClause(&test.filter)

			assert.Equal(t, " AND v0_events."+test.column+" = ?", clause)
			require.Len(t, values, 1)
			assert.Equal(t, test.value, values[0])
		})
	}
}

// TestBoundEventFilterClauseCombines verifies several bound columns AND
// together in a stable order, with one placeholder per value.
func TestBoundEventFilterClauseCombines(t *testing.T) {
	clause, values := boundEventFilterClause(&v0.Event{
		Note:                util.Ptr("boom"),
		Type:                util.Ptr("Warning"),
		ReportingController: util.Ptr("kubernetes-workload-controller"),
	})

	assert.Equal(t, 3, strings.Count(clause, " AND "))
	assert.Equal(t, 3, strings.Count(clause, "?"))
	assert.Len(t, values, 3)
	assert.Equal(t, []interface{}{"boom", "Warning", "kubernetes-workload-controller"}, values)
}

// TestBoundEventFilterClauseKeepsValuesOutOfSQL is the reason this renders to
// placeholders rather than interpolating. A filter value carrying SQL has to
// reach the driver as data, never as query text.
func TestBoundEventFilterClauseKeepsValuesOutOfSQL(t *testing.T) {
	hostile := "'; DROP TABLE v0_events; --"

	clause, values := boundEventFilterClause(&v0.Event{Note: util.Ptr(hostile)})

	assert.NotContains(t, clause, "DROP TABLE", "the value must not reach the SQL text")
	assert.Equal(t, " AND v0_events.note = ?", clause)
	require.Len(t, values, 1)
	assert.Equal(t, hostile, values[0], "the value travels as a bind parameter")
}

// TestBoundEventFilterClauseSkipsReason verifies reason is left to the
// handler's own reason and reasonprefix handling, so a reason filter is not
// applied twice with two different predicates.
func TestBoundEventFilterClauseSkipsReason(t *testing.T) {
	clause, values := boundEventFilterClause(&v0.Event{Reason: util.Ptr("FailedCreate")})

	assert.Equal(t, "", clause)
	assert.Empty(t, values)
}
