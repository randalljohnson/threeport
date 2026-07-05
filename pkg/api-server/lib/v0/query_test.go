package v0

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestLiveRowsFilterEmptyStringAlias covers the boundary case not exercised in
// object_lookup_test.go's TestLiveRowsFilter table: an empty-string alias still
// gets suffixed with .deleted_at IS NULL, which callers can rely on when they
// hand-format the alias.
func TestLiveRowsFilterEmptyStringAlias(t *testing.T) {
	// empty string alias yields the bare .deleted_at IS NULL fragment
	assert.Equal(t, ".deleted_at IS NULL", LiveRowsFilter(""))
}

// liveRowsTestModel is a minimal soft-deletable model used to verify that
// LiveRowsFilter suppresses soft-deleted rows when injected into a raw-table
// query that bypasses gorm's automatic filter.
type liveRowsTestModel struct {
	ID        uint `gorm:"primaryKey"`
	Name      string
	DeletedAt gorm.DeletedAt
}

// TestLiveRowsFilterAgainstDatabase asserts that the fragment returned by
// LiveRowsFilter, when composed into a .Table() query, hides soft-deleted
// rows the same way gorm's automatic filter would.
func TestLiveRowsFilterAgainstDatabase(t *testing.T) {
	// spin up an in-memory sqlite db and seed one live + one soft-deleted row
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&liveRowsTestModel{}))
	require.NoError(t, db.Create(&liveRowsTestModel{Name: "alive"}).Error)
	require.NoError(t, db.Create(&liveRowsTestModel{Name: "dead"}).Error)
	require.NoError(t, db.Delete(&liveRowsTestModel{}, "name = ?", "dead").Error)

	// action: query via .Table() (which bypasses gorm's automatic deleted_at
	// filter) and inject the LiveRowsFilter fragment
	var names []string
	err = db.Table("live_rows_test_models AS lr").
		Where(LiveRowsFilter("lr")).
		Pluck("name", &names).Error
	require.NoError(t, err)

	// only the live row is returned; the soft-deleted row is filtered out
	assert.Equal(t, []string{"alive"}, names)
}

// TestQueryScopes covers the URL-driven scope selection: no scope by default,
// an Unscoped scope when includedeleted=true, and no scope for any other value.
func TestQueryScopes(t *testing.T) {
	testCases := []struct {
		name           string
		target         string
		wantScopeCount int
	}{
		{
			// no query params: handlers get gorm's default (soft-delete filter on)
			name:           "no query params returns no scopes",
			target:         "/",
			wantScopeCount: 0,
		},
		{
			// explicit true opts into Unscoped so soft-deleted rows are visible
			name:           "includedeleted true adds one scope",
			target:         "/?includedeleted=true",
			wantScopeCount: 1,
		},
		{
			// only the literal "true" activates; "false" leaves default behavior
			name:           "includedeleted false returns no scopes",
			target:         "/?includedeleted=false",
			wantScopeCount: 0,
		},
		{
			// case-sensitive match: True is not the same as true
			name:           "includedeleted True case-sensitive miss",
			target:         "/?includedeleted=True",
			wantScopeCount: 0,
		},
		{
			// unrelated query params are ignored
			name:           "unrelated query param returns no scopes",
			target:         "/?name=foo",
			wantScopeCount: 0,
		},
		{
			// empty value doesn't match "true"
			name:           "includedeleted empty value returns no scopes",
			target:         "/?includedeleted=",
			wantScopeCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// build an echo context around the request so QueryScopes sees the
			// parsed query params
			e := echo.New()
			req := httptest.NewRequest("GET", tc.target, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			scopes := QueryScopes(c)

			// scope count matches expectation for this query-param shape
			assert.Len(t, scopes, tc.wantScopeCount)
		})
	}
}

// TestQueryScopesUnscopedBehavior asserts that the scope returned for
// includedeleted=true is actually gorm's Unscoped mode, i.e. it disables the
// automatic soft-delete filter that a standard model-based query respects.
func TestQueryScopesUnscopedBehavior(t *testing.T) {
	// setup: seed one live and one soft-deleted row so a default query would
	// omit the deleted row
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&liveRowsTestModel{}))
	require.NoError(t, db.Create(&liveRowsTestModel{Name: "alive"}).Error)
	require.NoError(t, db.Create(&liveRowsTestModel{Name: "dead"}).Error)
	require.NoError(t, db.Delete(&liveRowsTestModel{}, "name = ?", "dead").Error)

	// action: build an echo context with includedeleted=true so QueryScopes
	// returns the Unscoped scope
	e := echo.New()
	req := httptest.NewRequest("GET", "/?includedeleted=true", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	scopes := QueryScopes(c)
	require.Len(t, scopes, 1)

	// apply the returned scope and query; expect both rows because Unscoped
	// bypasses the soft-delete filter
	var withScopeRows []liveRowsTestModel
	require.NoError(t, db.Scopes(scopes...).Find(&withScopeRows).Error)
	assert.Len(t, withScopeRows, 2, "unscoped scope should surface soft-deleted rows")

	// control: a fresh query without the scope should hide the deleted row
	var defaultRows []liveRowsTestModel
	require.NoError(t, db.Find(&defaultRows).Error)
	assert.Len(t, defaultRows, 1, "default query hides soft-deleted rows")
}

// TestQueryParamIncludeDeletedConstant pins the wire-visible query parameter
// name so a rename in the const shows up as a test failure alongside the
// swagger/api client that would drift with it.
func TestQueryParamIncludeDeletedConstant(t *testing.T) {
	// exact-string check on the exported const
	assert.Equal(t, "includedeleted", QueryParamIncludeDeleted)
}
