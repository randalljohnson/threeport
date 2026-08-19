package v0

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// dryRunDB opens a gorm handle that builds statements without running them,
// so the create callbacks and their hooks are exercised against the postgres
// grammar CockroachDB speaks. SkipDefaultTransaction matters here: the
// wrapping transaction gorm opens around a create dials the database, which
// dry run alone does not prevent.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			DSN:                  "postgres://u:p@127.0.0.1:26257/threeport_api?sslmode=disable",
			PreferSimpleProtocol: true,
		}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true},
	)
	require.NoError(t, err, "opening the dry-run handle")
	return db
}

func dedupTestEvent() *Event {
	now := time.Now()
	return &Event{
		Reason:              strPtr("ScriptFailed"),
		Note:                strPtr("create script failed with exit code 1"),
		Type:                strPtr("Warning"),
		ReportingController: strPtr("MachineWorkloadController"),
		Count:               uintPtr(1),
		EventTime:           &now,
		LastObservedTime:    &now,
		ObjectType:          strPtr("threeport.io/v0.MachineWorkloadInstance"),
		ObjectID:            uintPtr(42),
	}
}

// TestEventCreateUpsertsOnDedupKey pins the write contract: an event create
// carries an ON CONFLICT clause targeting the dedup index, so a repeat of an
// event already on file bumps its count rather than adding a row. Without it a
// permanently failing object writes a fresh row every controller requeue.
func TestEventCreateUpsertsOnDedupKey(t *testing.T) {
	db := dryRunDB(t)

	stmt := db.Create(dedupTestEvent()).Statement
	sql := stmt.SQL.String()

	assert.Contains(t, sql, "ON CONFLICT ON CONSTRAINT idx_events_dedup",
		"create must target the dedup index by name; CockroachDB rejects an inline expression target:\n%s", sql)
	assert.Contains(t, strings.ToLower(sql), "count\"=v0_events.count + 1",
		"a repeat must increment the running count:\n%s", sql)
	assert.Contains(t, sql, "excluded.last_observed_time",
		"a repeat must record the newest sighting:\n%s", sql)
	assert.NotContains(t, sql, "excluded.event_time",
		"event_time must keep meaning first observed, so a repeat leaves it alone:\n%s", sql)
}

// TestEventCreateWritesSubjectColumns pins the subject onto the row. The dedup
// key spans the subject and the content, and an index cannot span two tables,
// so the subject has to be here for dedup to be expressible at all.
func TestEventCreateWritesSubjectColumns(t *testing.T) {
	db := dryRunDB(t)

	stmt := db.Create(dedupTestEvent()).Statement
	sql := stmt.SQL.String()

	assert.Contains(t, sql, "object_type", "subject type belongs on the event row:\n%s", sql)
	assert.Contains(t, sql, "object_id", "subject id belongs on the event row:\n%s", sql)
	assert.NotContains(t, sql, "object_name",
		"object_name resolves at read time from object_id and must not be stored:\n%s", sql)
}
