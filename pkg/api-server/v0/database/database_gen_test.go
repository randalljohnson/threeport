package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm/logger"
)

// newObservedLogger returns a zap logger paired with an observer that captures
// every entry written to it so tests can assert on structured fields.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core), logs
}

// clearDbEnv unsets every DB_* env var GetDsn() reads and registers a Cleanup
// so the pre-test values are restored when the test ends.
func clearDbEnv(t *testing.T) {
	t.Helper()
	keys := []string{"DB_HOST", "DB_NAME", "DB_PORT", "DB_SSL_MODE", "DB_USER"}
	prev := make(map[string]*string, len(keys))
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			s := v
			prev[k] = &s
		} else {
			prev[k] = nil
		}
		os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for k, v := range prev {
			if v == nil {
				os.Unsetenv(k)
				continue
			}
			os.Setenv(k, *v)
		}
	})
}

// setEnv sets an env var directly; clearDbEnv already handles restoration via
// t.Cleanup, so tests don't need t.Setenv's snapshot machinery.
func setEnv(t *testing.T, key, val string) {
	t.Helper()
	os.Setenv(key, val)
}

// TestGetDsn_ReturnsErrorWhenEnvMissing covers GetDsn()'s "missing required
// environment variable" branch: when no DB_* vars are set, every required var
// is reported in the returned error and the DSN string is degenerate.
func TestGetDsn_ReturnsErrorWhenEnvMissing(t *testing.T) {
	// clear all DB_* env vars so the loop hits the missing branch for each
	clearDbEnv(t)

	// call under test with the default (non-root) user
	dsn, err := GetDsn(false)

	// error must list every required env var
	if err == nil {
		t.Fatalf("expected error listing missing env vars, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"DB_HOST", "DB_NAME", "DB_PORT", "DB_SSL_MODE", "DB_USER"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to mention %s, got: %s", want, msg)
		}
	}
	// dsn is still produced with empty substitutions and the certs dir
	if !strings.Contains(dsn, ThreeportApiDbCertsDir) {
		t.Errorf("expected dsn to embed certs dir %q, got: %s", ThreeportApiDbCertsDir, dsn)
	}
}

// TestGetDsn_BuildsDsnFromEnv covers GetDsn()'s happy path: every DB_* var is
// set, no error is returned, and the DSN embeds each value plus the certs dir.
func TestGetDsn_BuildsDsnFromEnv(t *testing.T) {
	// set every required env var with a distinct sentinel so the format
	// string's positional args can be checked individually
	clearDbEnv(t)
	setEnv(t, "DB_HOST", "host-val")
	setEnv(t, "DB_NAME", "name-val")
	setEnv(t, "DB_PORT", "12345")
	setEnv(t, "DB_SSL_MODE", "verify-full")
	setEnv(t, "DB_USER", "user-val")

	// call under test with the default user (rootDbUser=false)
	dsn, err := GetDsn(false)

	// no missing-env error and every sentinel appears in the dsn
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"host=host-val",
		"dbname=name-val",
		"port=12345",
		"sslmode=verify-full",
		"user=user-val",
		ThreeportApiDbCertsDir,
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("expected dsn to contain %q, got: %s", want, dsn)
		}
	}
}

// TestGetDsn_RootUserOverridesDbUser covers GetDsn()'s rootDbUser=true branch:
// the DB_USER env var is ignored and the DSN uses the literal "root" user.
func TestGetDsn_RootUserOverridesDbUser(t *testing.T) {
	// set every var including DB_USER; the assertion below proves the flag
	// wins over the env var
	clearDbEnv(t)
	setEnv(t, "DB_HOST", "h")
	setEnv(t, "DB_NAME", "n")
	setEnv(t, "DB_PORT", "1")
	setEnv(t, "DB_SSL_MODE", "s")
	setEnv(t, "DB_USER", "ignored-user")

	// call under test with rootDbUser=true
	dsn, err := GetDsn(true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(dsn, "user=root") {
		t.Errorf("expected root user override, got: %s", dsn)
	}
	// certs path also picks the root user via the positional format arg
	if !strings.Contains(dsn, "client.root.crt") || !strings.Contains(dsn, "client.root.key") {
		t.Errorf("expected root cert refs in dsn, got: %s", dsn)
	}
}

// TestSuppressSensitive_ReplacesMessageWithSensitiveString covers
// suppressSensitive()'s hit path: a message containing one of the
// log.SensitiveStrings values is replaced by a suppression notice.
func TestSuppressSensitive_ReplacesMessageWithSensitiveString(t *testing.T) {
	// input carries the "PRIVATE KEY" sentinel so the suppression loop fires
	in := "here is a PRIVATE KEY dump"

	// call under test
	got := suppressSensitive(in)

	// suppression notice replaces the original message
	if got == in {
		t.Fatalf("expected message to be suppressed, got the original: %s", got)
	}
	if !strings.Contains(got, "PRIVATE KEY") {
		t.Errorf("expected suppression notice to name the sensitive string, got: %s", got)
	}
}

// TestSuppressSensitive_PassesThroughSafeMessage covers suppressSensitive()'s
// pass-through path: an ordinary message contains no sensitive strings and is
// returned unchanged.
func TestSuppressSensitive_PassesThroughSafeMessage(t *testing.T) {
	// input carries no sensitive tokens
	in := "harmless log line"

	// call under test
	got := suppressSensitive(in)

	// message returned unchanged
	if got != in {
		t.Errorf("expected pass-through of %q, got %q", in, got)
	}
}

// TestZapLogger_LogModeReturnsSameLogger covers LogMode()'s stub behavior: it
// ignores the level argument and returns the receiver so callers can chain.
func TestZapLogger_LogModeReturnsSameLogger(t *testing.T) {
	// build a ZapLogger over a no-op zap logger
	zl := &ZapLogger{Logger: zap.NewNop()}

	// call under test with an arbitrary level
	out := zl.LogMode(logger.Warn)

	// return value is the same receiver
	if out != zl {
		t.Errorf("expected LogMode to return receiver, got %T", out)
	}
}

// TestZapLogger_InfoForwardsKeyValuePairs covers Info()'s pair-decoding happy
// path: each string key + value pair becomes a structured zap field on the
// emitted entry.
func TestZapLogger_InfoForwardsKeyValuePairs(t *testing.T) {
	// observed core so the test can read back the emitted entry
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// call under test with two well-formed pairs
	zl.Info(context.Background(), "hello", "k1", "v1", "k2", 42)

	// exactly one Info entry emitted with both pairs as fields
	entries := logs.FilterMessage("hello").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Info entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["k1"] != "v1" {
		t.Errorf("expected field k1=v1, got %v", fields["k1"])
	}
	if fields["k2"] != int64(42) {
		t.Errorf("expected field k2=42, got %v", fields["k2"])
	}
}

// TestZapLogger_InfoWarnsOnOddArgCount covers Info()'s odd-arg-count branch:
// a trailing unpaired arg triggers a warning and the loop breaks.
func TestZapLogger_InfoWarnsOnOddArgCount(t *testing.T) {
	// observed core to inspect emitted entries
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// call under test with a dangling third arg
	zl.Info(context.Background(), "msg", "k1", "v1", "dangling")

	// a warn entry surfaces the odd-arg-count condition
	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) == 0 {
		t.Fatalf("expected a warn entry for odd arg count")
	}
	if !strings.Contains(warns[0].Message, "odd number of arguments") {
		t.Errorf("expected odd-arg-count warning, got: %s", warns[0].Message)
	}
}

// TestZapLogger_InfoWarnsOnNonStringKey covers Info()'s non-string-key branch:
// when the key argument is not a string, a warning is emitted and the pair is
// skipped rather than added as a field.
func TestZapLogger_InfoWarnsOnNonStringKey(t *testing.T) {
	// observed core
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// call under test with an int key
	zl.Info(context.Background(), "msg", 123, "value")

	// a warn entry flags the non-string key
	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) == 0 {
		t.Fatalf("expected a warn entry for non-string key")
	}
	if !strings.Contains(warns[0].Message, "key is not a string") {
		t.Errorf("expected non-string-key warning, got: %s", warns[0].Message)
	}
	// the Info entry has no field from the bad pair
	infos := logs.FilterMessage("msg").All()
	if len(infos) != 1 {
		t.Fatalf("expected one Info entry, got %d", len(infos))
	}
	if len(infos[0].Context) != 0 {
		t.Errorf("expected no fields from skipped pair, got %v", infos[0].Context)
	}
}

// TestZapLogger_InfoStringifiesPointerKey covers Info()'s pointer-key path:
// when data[i] is a pointer, it's replaced with its %+v rendering before the
// string type assertion runs; a field is added under the rendered key.
func TestZapLogger_InfoStringifiesPointerKey(t *testing.T) {
	// observed core
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// pointer key; %+v on a Go pointer renders as "0x..." address text
	key := "pkey"
	zl.Info(context.Background(), "msg", &key, "v")

	// entry emitted; exactly one field is added and its value is "v"
	entries := logs.FilterMessage("msg").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Info entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if len(fields) != 1 {
		t.Fatalf("expected exactly one field from pointer key, got %v", fields)
	}
	for k, v := range fields {
		if v != "v" {
			t.Errorf("expected field value v, got %v", v)
		}
		if !strings.HasPrefix(k, "0x") {
			t.Errorf("expected pointer-derived key to start with 0x, got %q", k)
		}
	}
}

// TestZapLogger_WarnForwardsKeyValuePairs covers Warn()'s pair-decoding happy
// path: each string key + value pair becomes a structured field on the emitted
// warning entry.
func TestZapLogger_WarnForwardsKeyValuePairs(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// call under test with one well-formed pair
	zl.Warn(context.Background(), "warned", "k", "v")

	entries := logs.FilterMessage("warned").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Warn entry, got %d", len(entries))
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Errorf("expected WarnLevel, got %v", entries[0].Level)
	}
	if entries[0].ContextMap()["k"] != "v" {
		t.Errorf("expected field k=v, got %v", entries[0].ContextMap())
	}
}

// TestZapLogger_WarnHandlesEdgeCases covers Warn()'s odd-arg-count and
// non-string-key branches in a single call.
func TestZapLogger_WarnHandlesEdgeCases(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// odd-arg-count case
	zl.Warn(context.Background(), "odd", "k1", "v1", "dangling")
	// non-string-key case
	zl.Warn(context.Background(), "bad-key", 7, "v")

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	// two Warn calls plus their internal odd-arg / non-string-key warnings
	if len(warns) < 2 {
		t.Fatalf("expected multiple warn entries, got %d", len(warns))
	}
}

// TestZapLogger_ErrorForwardsKeyValuePairs covers Error()'s pair-decoding happy
// path: each string key + value pair becomes a field on the emitted error
// entry.
func TestZapLogger_ErrorForwardsKeyValuePairs(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// call under test with a well-formed pair
	zl.Error(context.Background(), "boom", "k", "v")

	entries := logs.FilterMessage("boom").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Error entry, got %d", len(entries))
	}
	if entries[0].Level != zapcore.ErrorLevel {
		t.Errorf("expected ErrorLevel, got %v", entries[0].Level)
	}
	if entries[0].ContextMap()["k"] != "v" {
		t.Errorf("expected field k=v, got %v", entries[0].ContextMap())
	}
}

// TestZapLogger_ErrorHandlesEdgeCases covers Error()'s odd-arg-count and
// non-string-key branches; each is expected to emit an internal warning.
func TestZapLogger_ErrorHandlesEdgeCases(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	zl.Error(context.Background(), "odd", "k1", "v1", "dangling")
	zl.Error(context.Background(), "bad-key", 7, "v")

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) < 2 {
		t.Fatalf("expected internal warn entries for edge cases, got %d", len(warns))
	}
}

// TestZapLogger_TraceEmitsDebugEntry covers Trace()'s no-error path: it calls
// the sql-producing closure, emits a Debug entry, and attaches sql/rows/elapsed
// fields.
func TestZapLogger_TraceEmitsDebugEntry(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	// begin is set 1ms in the past so elapsed is nonzero
	begin := time.Now().Add(-time.Millisecond)
	fc := func() (string, int64) { return "SELECT 1", 3 }

	// call under test with err=nil
	zl.Trace(context.Background(), begin, fc, nil)

	// exactly one Debug entry with the expected fields
	entries := logs.FilterMessage("gorm query").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Debug entry, got %d", len(entries))
	}
	if entries[0].Level != zapcore.DebugLevel {
		t.Errorf("expected DebugLevel, got %v", entries[0].Level)
	}
	fields := entries[0].ContextMap()
	if fields["sql"] != "SELECT 1" {
		t.Errorf("expected sql field, got %v", fields["sql"])
	}
	if fields["rows"] != int64(3) {
		t.Errorf("expected rows=3, got %v", fields["rows"])
	}
	if fields["type"] != "sql" {
		t.Errorf("expected type=sql, got %v", fields["type"])
	}
	// no error field since err was nil
	if _, ok := fields["error"]; ok {
		t.Errorf("expected no error field, got %v", fields["error"])
	}
}

// TestZapLogger_TraceAttachesErrorField covers Trace()'s error path: the err
// argument is attached as an "error" field on the emitted entry.
func TestZapLogger_TraceAttachesErrorField(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	begin := time.Now().Add(-time.Millisecond)
	fc := func() (string, int64) { return "SELECT 1", 0 }
	sentinel := errors.New("query failed")

	// call under test with a non-nil err
	zl.Trace(context.Background(), begin, fc, sentinel)

	entries := logs.FilterMessage("gorm query").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Debug entry, got %d", len(entries))
	}
	if entries[0].ContextMap()["error"] != sentinel.Error() {
		t.Errorf("expected error field %q, got %v", sentinel.Error(), entries[0].ContextMap()["error"])
	}
}

// TestZapLogger_TraceSuppressesSensitiveSql covers the Trace + suppressSensitive
// integration: an SQL string containing "PRIVATE KEY" is replaced by the
// suppression notice before landing in the emitted entry's sql field.
func TestZapLogger_TraceSuppressesSensitiveSql(t *testing.T) {
	log, logs := newObservedLogger()
	zl := &ZapLogger{Logger: log}

	begin := time.Now().Add(-time.Millisecond)
	// SQL carries the sensitive sentinel
	fc := func() (string, int64) { return "INSERT PRIVATE KEY data", 1 }

	// call under test
	zl.Trace(context.Background(), begin, fc, nil)

	entries := logs.FilterMessage("gorm query").All()
	if len(entries) != 1 {
		t.Fatalf("expected one Debug entry, got %d", len(entries))
	}
	sql, _ := entries[0].ContextMap()["sql"].(string)
	if strings.Contains(sql, "INSERT PRIVATE KEY data") {
		t.Errorf("expected sensitive sql to be suppressed, got: %s", sql)
	}
	if !strings.Contains(sql, "PRIVATE KEY") {
		t.Errorf("expected suppression notice to reference the sensitive token, got: %s", sql)
	}
}
