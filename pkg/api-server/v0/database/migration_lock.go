package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/pressly/goose/v3/lock"
)

const (
	// migrationLockWait bounds how long a migrator waits for a lock another
	// migrator already holds. It has to outlast a whole migration run, since a
	// rolling update starts a second migrator while the first is still working,
	// and stay short enough that a wedged migrator fails its pod rather than
	// stalling every later rollout.
	migrationLockWait = 10 * time.Minute

	// migrationLockRowId is the primary key of the single row migrators contend
	// for.
	migrationLockRowId = 1

	// migrationLockSetupAttempts caps how many times a migrator retries creating
	// the lock table and its row, since two creating it at once fails one of them.
	migrationLockSetupAttempts = 5

	// migrationLockSetupInterval is the pause between those attempts.
	migrationLockSetupInterval = 2 * time.Second
)

// lockTableNamePattern matches a bare lowercase SQL identifier. A lock table
// name reaches the database inside the statement text rather than as a bound
// parameter, so reject any name that could carry SQL of its own.
var lockTableNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// MigrationLocker lets one database migrator run at a time. It holds the lock by
// writing a single row inside an open transaction, using only statements any
// PostgreSQL-compatible database understands, because CockroachDB implements no
// advisory locks and the session locker goose ships calls for one.
//
// Writing the row is what makes it a lock; reading it would not. CockroachDB
// holds a row taken with SELECT ... FOR UPDATE only in memory on the range
// leaseholder, so a lease transfer or range split during a long migration can
// drop it and let a second migrator through. An UPDATE leaves a replicated write
// intent that survives both, and blocks a second migrator until this transaction
// ends.
//
// Two constraints on the connection this locker is given:
//
//   - Its pool must allow more than one connection, since a non-transactional Go
//     migration would otherwise need the connection the lock sits on.
//   - Migrations must not run on this connection. The ones registered today reach
//     the database through a handle carried on the context, so they never touch
//     it; a transactional or SQL migration would run inside the transaction
//     opened here.
//
// One instance holds one transaction, so give every migration provider its own.
type MigrationLocker struct {
	tableName string
	wait      time.Duration
	tx        *sql.Tx
}

var _ lock.SessionLocker = (*MigrationLocker)(nil)

// NewMigrationLocker returns a session locker that contends for a row in the
// named table. Every module migrates the same database, so each names its own
// lock table to keep its migrator runs independent.
func NewMigrationLocker(tableName string) (*MigrationLocker, error) {
	if !lockTableNamePattern.MatchString(tableName) {
		return nil, fmt.Errorf(
			"lock table name %s is not a bare lowercase SQL identifier",
			tableName,
		)
	}

	return &MigrationLocker{
		tableName: tableName,
		wait:      migrationLockWait,
	}, nil
}

// SessionLock takes the migration lock on the given connection, waiting for
// whichever migrator holds it to finish. The transaction it opens stays open
// after this call returns, since holding it is what holds the lock.
func (l *MigrationLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if l.tx != nil {
		return errors.New("migration lock is already held")
	}

	if err := l.ensureLockRow(ctx, conn); err != nil {
		return err
	}

	// the lock outlives this call, so the transaction must not inherit a context
	// that gets cancelled the moment the caller returns
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return fmt.Errorf("failed to open migration lock transaction: %w", err)
	}

	// bound the wait and nothing else: the write intent blocks this statement
	// until the migrator holding it commits, and a migrator that never commits
	// would otherwise hold up every later run forever
	waitCtx, cancel := context.WithTimeout(ctx, l.wait)
	defer cancel()

	// stamping locked_at is what leaves the intent; the value itself is only
	// read by a human wondering when a stuck migrator took the lock
	var lockedRowId int
	if err := tx.QueryRowContext(waitCtx, fmt.Sprintf(
		"UPDATE %s SET locked_at = now() WHERE id = %d RETURNING id",
		l.tableName,
		migrationLockRowId,
	)).Scan(&lockedRowId); err != nil {
		return errors.Join(
			fmt.Errorf("failed to take migration lock within %s: %w", l.wait, err),
			tx.Rollback(),
		)
	}

	l.tx = tx

	return nil
}

// SessionUnlock releases the migration lock by ending the transaction that holds
// it.
func (l *MigrationLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	if l.tx == nil {
		return errors.New("migration lock is not held")
	}

	tx := l.tx
	l.tx = nil

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to release migration lock: %w", err)
	}

	return nil
}

// ensureLockRow creates the lock table and the row every migrator contends for,
// outside the locking transaction since the row must exist before anything can
// lock it.
func (l *MigrationLocker) ensureLockRow(ctx context.Context, conn *sql.Conn) error {
	createTable := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id INT PRIMARY KEY, locked_at TIMESTAMPTZ)",
		l.tableName,
	)
	insertRow := fmt.Sprintf(
		"INSERT INTO %s (id) VALUES (%d) ON CONFLICT (id) DO NOTHING",
		l.tableName,
		migrationLockRowId,
	)

	var setupErr error
	for attempt := 0; attempt < migrationLockSetupAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(ctx.Err(), setupErr)
			case <-time.After(migrationLockSetupInterval):
			}
		}
		if _, err := conn.ExecContext(ctx, createTable); err != nil {
			setupErr = fmt.Errorf("failed to create migration lock table: %w", err)
			continue
		}
		if _, err := conn.ExecContext(ctx, insertRow); err != nil {
			setupErr = fmt.Errorf("failed to create migration lock row: %w", err)
			continue
		}

		return nil
	}

	return setupErr
}
