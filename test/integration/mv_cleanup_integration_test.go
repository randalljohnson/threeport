//go:build integration

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
)

// connectCRDB opens a gorm connection to the CockroachDB used by the API
// server; skips t if no DSN is configured.
func connectCRDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CRDB_DSN")
	if dsn == "" {
		t.Skip("skipping materialized-view test: CRDB_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skipping materialized-view test: cannot connect to CRDB (%v)", err)
	}
	return db
}

// TestCleanupMaterializedViewsDropsOldViews creates a paginated_* view whose
// timestamp is older than the TTL and asserts the cleanup goroutine removes
// it on its next tick.
func TestCleanupMaterializedViewsDropsOldViews(t *testing.T) {
	db := connectCRDB(t)
	logger := zap.NewNop()

	// setup: create a materialized view with a timestamp older than the
	// 1-minute TTL so the sweeper's TTL check will select it
	oldTS := time.Now().Add(-2 * time.Hour).Format("20060102150405")
	viewName := fmt.Sprintf("%s_%s_integration_old", apiserver_lib.PaginationViewPrefix, oldTS)
	if err := db.Exec(fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS %s AS SELECT 1 AS c", viewName)).Error; err != nil {
		t.Skipf("skipping: cannot create materialized view (%v)", err)
	}
	defer db.Exec(fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", viewName))

	// action: start the cleanup goroutine with a fast tick and 1-minute TTL
	apiserver_lib.CleanupMaterializedViews(db, logger, 1, 1)

	// assert: the old view is gone within a few ticks
	waitForResource(t, 20*time.Second, 1*time.Second, "materialized view drop", func() error {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?", viewName).Scan(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("view %s still present", viewName)
		}
		return nil
	})
}

// TestCleanupMaterializedViewsKeepsFreshViews creates a view whose timestamp
// is well inside the TTL and asserts the sweeper leaves it alone.
func TestCleanupMaterializedViewsKeepsFreshViews(t *testing.T) {
	db := connectCRDB(t)
	logger := zap.NewNop()

	// setup: fresh timestamp so the sweeper should skip
	freshTS := time.Now().Format("20060102150405")
	viewName := fmt.Sprintf("%s_%s_integration_fresh", apiserver_lib.PaginationViewPrefix, freshTS)
	if err := db.Exec(fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS %s AS SELECT 1 AS c", viewName)).Error; err != nil {
		t.Skipf("skipping: cannot create materialized view (%v)", err)
	}
	defer db.Exec(fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", viewName))

	// action: run the sweeper with a 60-minute TTL; the fresh view is well
	// inside the window and must survive
	apiserver_lib.CleanupMaterializedViews(db, logger, 1, 60)

	// assert: after a few ticks the view is still there
	time.Sleep(5 * time.Second)
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?", viewName).Scan(&count).Error; err != nil {
		t.Fatalf("failed to check materialized view: %v", err)
	}
	if count != 1 {
		t.Fatalf("fresh materialized view %s should not have been dropped (count=%d)", viewName, count)
	}
}

// TestCleanupMaterializedViewsHandlesEmptyState asserts the sweeper does not
// panic or error when there are no paginated_* views to sweep, so a cold
// start of the API server is safe.
func TestCleanupMaterializedViewsHandlesEmptyState(t *testing.T) {
	db := connectCRDB(t)
	logger := zap.NewNop()

	// action: start the sweeper against a DB with no paginated_* views
	apiserver_lib.CleanupMaterializedViews(db, logger, 1, 1)

	// assert: give it a couple ticks and confirm we can still query the DB
	time.Sleep(3 * time.Second)
	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil {
		t.Fatalf("db unreachable after sweeper start: %v", err)
	}
	if one != 1 {
		t.Fatalf("basic sanity query failed: got %d", one)
	}
}
