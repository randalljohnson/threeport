package v0

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	echolog "github.com/labstack/gommon/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newObservedLogger builds a *zap.Logger paired with an observer so tests can
// assert what was logged without relying on stderr.
func newObservedLogger(level zapcore.Level) (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(level)
	return zap.New(core), logs
}

// TestNewLoggerVerbose covers NewLogger() building a development logger when verbose is true.
func TestNewLoggerVerbose(t *testing.T) {
	// build a verbose logger; the development config path must succeed
	logger, err := NewLogger(true)
	if err != nil {
		t.Fatalf("NewLogger(true) unexpected error: %v", err)
	}

	// exercise the returned logger to confirm it's usable (would panic on a zero value)
	logger.Info("verbose smoke test")
	if err := logger.Sync(); err != nil {
		// sync on stderr can return EINVAL on some platforms; only fail on unexpected errors
		if !strings.Contains(err.Error(), "invalid argument") &&
			!strings.Contains(err.Error(), "inappropriate ioctl") {
			t.Fatalf("Sync() unexpected error: %v", err)
		}
	}
}

// TestNewLoggerProduction covers NewLogger() building a production logger when verbose is false.
func TestNewLoggerProduction(t *testing.T) {
	// build a production logger; the default branch path must succeed
	logger, err := NewLogger(false)
	if err != nil {
		t.Fatalf("NewLogger(false) unexpected error: %v", err)
	}

	// exercise the returned logger
	logger.Info("production smoke test")
	if err := logger.Sync(); err != nil {
		if !strings.Contains(err.Error(), "invalid argument") &&
			!strings.Contains(err.Error(), "inappropriate ioctl") {
			t.Fatalf("Sync() unexpected error: %v", err)
		}
	}
}

// TestSensitiveStrings covers SensitiveStrings() returning the expected suppression set.
func TestSensitiveStrings(t *testing.T) {
	got := SensitiveStrings()

	// verify the well-known sensitive token is present
	found := false
	for _, s := range got {
		if s == "PRIVATE KEY" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SensitiveStrings() = %v, expected to contain %q", got, "PRIVATE KEY")
	}
}

// TestSuppressSensitiveCoreSuppresses covers Write() dropping entries whose message contains a sensitive string.
func TestSuppressSensitiveCoreSuppresses(t *testing.T) {
	// wrap an observer core with the suppressor
	inner, logs := observer.New(zapcore.DebugLevel)
	suppressor := &SuppressSensitiveCore{
		Core:             inner,
		sensitiveStrings: []string{"PRIVATE KEY"},
	}

	// writing a message that embeds the sensitive token should be dropped
	entry := zapcore.Entry{Level: zapcore.InfoLevel, Message: "here is my PRIVATE KEY, oops"}
	if err := suppressor.Write(entry, nil); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	// the inner core must have received nothing
	if n := logs.Len(); n != 0 {
		t.Fatalf("expected 0 log entries after suppression, got %d", n)
	}
}

// TestSuppressSensitiveCorePassesThrough covers Write() forwarding entries that don't match any sensitive string.
func TestSuppressSensitiveCorePassesThrough(t *testing.T) {
	// wrap an observer core with the suppressor
	inner, logs := observer.New(zapcore.DebugLevel)
	suppressor := &SuppressSensitiveCore{
		Core:             inner,
		sensitiveStrings: []string{"PRIVATE KEY"},
	}

	// a benign message must reach the inner core intact
	entry := zapcore.Entry{Level: zapcore.InfoLevel, Message: "benign message"}
	if err := suppressor.Write(entry, nil); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	// the inner core must have observed exactly one entry
	if n := logs.Len(); n != 1 {
		t.Fatalf("expected 1 log entry passed through, got %d", n)
	}
	if got := logs.All()[0].Message; got != "benign message" {
		t.Fatalf("passed-through message = %q, want %q", got, "benign message")
	}
}

// TestNewEchoLoggerReturnsEchoLogger covers NewEchoLogger() returning an implementation of echo.Logger with default INFO level.
func TestNewEchoLoggerReturnsEchoLogger(t *testing.T) {
	// build the adapter
	zl, _ := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl)

	// the returned value must satisfy the echo.Logger interface (compile-time via type assignment)
	var _ echo.Logger = l

	// default log level is INFO per the constructor
	if got := l.Level(); got != echolog.INFO {
		t.Fatalf("Level() = %v, want %v", got, echolog.INFO)
	}
}

// TestEchoLoggerOutputAndSetOutput covers Output() returning nil and SetOutput() being a no-op.
func TestEchoLoggerOutputAndSetOutput(t *testing.T) {
	zl, _ := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl)

	// Output() reports nil (no writer backing)
	if w := l.Output(); w != nil {
		t.Fatalf("Output() = %v, want nil", w)
	}

	// SetOutput() must not panic and must not change Output()'s nil answer
	var buf bytes.Buffer
	l.SetOutput(io.Writer(&buf))
	if w := l.Output(); w != nil {
		t.Fatalf("Output() after SetOutput() = %v, want nil", w)
	}
}

// TestEchoLoggerPrefix covers SetPrefix() persisting the value and Prefix() returning it.
func TestEchoLoggerPrefix(t *testing.T) {
	zl, _ := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl)

	// default prefix is the zero string
	if got := l.Prefix(); got != "" {
		t.Fatalf("initial Prefix() = %q, want empty", got)
	}

	// setting the prefix updates the getter
	l.SetPrefix("api")
	if got := l.Prefix(); got != "api" {
		t.Fatalf("Prefix() = %q, want %q", got, "api")
	}
}

// TestEchoLoggerSetLevel covers SetLevel() changing the level returned by Level().
func TestEchoLoggerSetLevel(t *testing.T) {
	zl, _ := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl)

	// change level and observe the getter
	l.SetLevel(echolog.WARN)
	if got := l.Level(); got != echolog.WARN {
		t.Fatalf("Level() = %v, want %v", got, echolog.WARN)
	}
}

// TestEchoLoggerSetHeader covers SetHeader() not panicking; the header is not surfaced via a getter.
func TestEchoLoggerSetHeader(t *testing.T) {
	zl, _ := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl)

	// SetHeader is a documented no-op for zap; call it to cover the branch
	l.SetHeader("X-Request-Id")
}

// TestEchoLoggerLevelMethods covers Print/Debug/Info/Warn/Error variants routing to the underlying zap logger at the matching level.
func TestEchoLoggerLevelMethods(t *testing.T) {
	// use an observer at DEBUG so every level under FATAL is captured
	zl, logs := newObservedLogger(zapcore.DebugLevel)
	l := NewEchoLogger(zl).(*EchoLogger)

	// each row exercises one adapter method and its expected level + message
	type call struct {
		name     string
		fn       func()
		wantLvl  zapcore.Level
		wantText string
	}
	calls := []call{
		{"Print", func() { l.Print("p1", "p2") }, zapcore.InfoLevel, "p1p2"},
		{"Printf", func() { l.Printf("hello %s", "world") }, zapcore.InfoLevel, "hello world"},
		{"Printj", func() { l.Printj(echolog.JSON{"k": "v"}) }, zapcore.InfoLevel, fmt.Sprint(echolog.JSON{"k": "v"})},
		{"Debug", func() { l.Debug("d1", "d2") }, zapcore.DebugLevel, "d1d2"},
		{"Debugf", func() { l.Debugf("dbg %d", 7) }, zapcore.DebugLevel, "dbg 7"},
		{"Debugj", func() { l.Debugj(echolog.JSON{"d": 1}) }, zapcore.DebugLevel, fmt.Sprint(echolog.JSON{"d": 1})},
		{"Info", func() { l.Info("i1") }, zapcore.InfoLevel, "i1"},
		{"Infof", func() { l.Infof("info %s", "x") }, zapcore.InfoLevel, "info x"},
		{"Infoj", func() { l.Infoj(echolog.JSON{"i": 1}) }, zapcore.InfoLevel, fmt.Sprint(echolog.JSON{"i": 1})},
		{"Warn", func() { l.Warn("w1") }, zapcore.WarnLevel, "w1"},
		{"Warnf", func() { l.Warnf("warn %s", "y") }, zapcore.WarnLevel, "warn y"},
		{"Warnj", func() { l.Warnj(echolog.JSON{"w": 1}) }, zapcore.WarnLevel, fmt.Sprint(echolog.JSON{"w": 1})},
		{"Error", func() { l.Error("e1") }, zapcore.ErrorLevel, "e1"},
		{"Errorf", func() { l.Errorf("err %s", "z") }, zapcore.ErrorLevel, "err z"},
		{"Errorj", func() { l.Errorj(echolog.JSON{"e": 1}) }, zapcore.ErrorLevel, fmt.Sprint(echolog.JSON{"e": 1})},
	}

	// invoke each adapter method and verify the matching entry landed on the observer
	before := 0
	for _, c := range calls {
		c.fn()
		entries := logs.All()
		if len(entries) != before+1 {
			t.Fatalf("%s: expected 1 new entry, got %d total (was %d)", c.name, len(entries), before)
		}
		got := entries[before]
		if got.Level != c.wantLvl {
			t.Errorf("%s: level = %v, want %v", c.name, got.Level, c.wantLvl)
		}
		if got.Message != c.wantText {
			t.Errorf("%s: message = %q, want %q", c.name, got.Message, c.wantText)
		}
		before++
	}
}

// TestEchoLoggerPanicVariants covers Panic/Panicf/Panicj routing to zap.Panic, which panics after logging.
func TestEchoLoggerPanicVariants(t *testing.T) {
	// each panic method must panic; the observer captures the entry en route
	cases := []struct {
		name string
		fn   func(l *EchoLogger)
	}{
		{"Panic", func(l *EchoLogger) { l.Panic("boom") }},
		{"Panicf", func(l *EchoLogger) { l.Panicf("boom %s", "hard") }},
		{"Panicj", func(l *EchoLogger) { l.Panicj(echolog.JSON{"k": "v"}) }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// build a fresh logger per case so state doesn't leak between panics
			zl, logs := newObservedLogger(zapcore.DebugLevel)
			l := NewEchoLogger(zl).(*EchoLogger)

			// invoke via a helper so recover() catches the panic zap raises
			func() {
				defer func() {
					if r := recover(); r == nil {
						t.Fatalf("%s did not panic", c.name)
					}
				}()
				c.fn(l)
			}()

			// exactly one entry must have been recorded at PanicLevel
			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("%s: expected 1 log entry, got %d", c.name, len(entries))
			}
			if entries[0].Level != zapcore.PanicLevel {
				t.Errorf("%s: level = %v, want %v", c.name, entries[0].Level, zapcore.PanicLevel)
			}
		})
	}
}

// TestEchoLoggerFatalVariants covers Fatal/Fatalf/Fatalj routing to zap.Fatal via a WriteThenGoexit hook so os.Exit is not called.
func TestEchoLoggerFatalVariants(t *testing.T) {
	// zap.Fatal exits the process by default; swap in WriteThenGoexit so the goroutine ends cleanly
	zl, logs := newObservedLogger(zapcore.DebugLevel)
	zl = zl.WithOptions(zap.OnFatal(zapcore.WriteThenGoexit))
	l := NewEchoLogger(zl).(*EchoLogger)

	// each fatal method runs on a goroutine so Goexit only ends the goroutine, not the test binary
	cases := []struct {
		name string
		fn   func()
	}{
		{"Fatal", func() { l.Fatal("dead") }},
		{"Fatalf", func() { l.Fatalf("dead %s", "now") }},
		{"Fatalj", func() { l.Fatalj(echolog.JSON{"k": "v"}) }},
	}

	before := 0
	for _, c := range cases {
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.fn()
		}()
		<-done

		// each fatal call must have recorded exactly one FatalLevel entry
		entries := logs.All()
		if len(entries) != before+1 {
			t.Fatalf("%s: expected 1 new entry, got %d total", c.name, len(entries))
		}
		if entries[before].Level != zapcore.FatalLevel {
			t.Errorf("%s: level = %v, want %v", c.name, entries[before].Level, zapcore.FatalLevel)
		}
		before++
	}
}
