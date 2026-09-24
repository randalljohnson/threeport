package v0

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	aurora "github.com/logrusorgru/aurora"
)

// captureStdout redirects os.Stdout for the duration of fn and returns whatever fn wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	<-done
	_ = r.Close()
	return buf.String()
}

// TestCliOutputError covers the two branches of CliOutputError: with and without an error argument.
func TestCliOutputError(t *testing.T) {
	cases := []struct {
		name     string
		message  string
		err      error
		wantSubs []string
	}{
		{
			name:    "with error appends error text on new line",
			message: "boom happened",
			err:     errors.New("underlying cause"),
			wantSubs: []string{
				"Error: boom happened",
				"underlying cause",
			},
		},
		{
			name:    "nil error prints message only",
			message: "just a message",
			err:     nil,
			wantSubs: []string{
				"Error: just a message",
			},
		},
		{
			name:    "empty message still prefixed with Error:",
			message: "",
			err:     nil,
			wantSubs: []string{
				"Error: ",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the function under test and capture stdout
			got := captureStdout(t, func() {
				CliOutputError(tc.message, tc.err)
			})

			// each expected substring must appear in the captured output
			for _, sub := range tc.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("output %q missing substring %q", got, sub)
				}
			}

			// nil-error branch must NOT contain a second-line error rendering
			if tc.err == nil && strings.Contains(got, "\n<nil>") {
				t.Errorf("nil error should not render as <nil> in output: %q", got)
			}
		})
	}
}

// TestCliOutputError_MatchesAuroraRedFormat asserts the produced output matches the aurora Red format used by production callers.
func TestCliOutputError_MatchesAuroraRedFormat(t *testing.T) {
	// capture actual output
	got := captureStdout(t, func() {
		CliOutputError("msg", errors.New("cause"))
	})

	// build the expected rendering the same way the production function does
	want := fmt.Sprintln(aurora.Red(fmt.Sprintf("Error: %s\n%s", "msg", errors.New("cause"))))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCliOutputInfo covers the plain Info prefix format and trailing newline.
func TestCliOutputInfo(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    string
	}{
		{name: "simple message", message: "hello", want: "Info: hello\n"},
		{name: "empty message keeps prefix", message: "", want: "Info: \n"},
		{name: "multiline message preserves newlines", message: "line1\nline2", want: "Info: line1\nline2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// capture the function output
			got := captureStdout(t, func() {
				CliOutputInfo(tc.message)
			})
			// exact match on info format
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCliOutputNotice asserts Notice prefix is rendered through aurora.Blue.
func TestCliOutputNotice(t *testing.T) {
	// capture output
	got := captureStdout(t, func() {
		CliOutputNotice("heads up")
	})

	// the message content must appear
	if !strings.Contains(got, "Notice: heads up") {
		t.Errorf("output %q missing Notice prefix", got)
	}

	// output must match the same aurora.Blue rendering as production
	want := fmt.Sprintln(aurora.Blue(fmt.Sprintf("Notice: %s", "heads up")))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCliOutputWarning asserts Warning prefix is rendered through aurora.Yellow.
func TestCliOutputWarning(t *testing.T) {
	// capture output
	got := captureStdout(t, func() {
		CliOutputWarning("be careful")
	})

	// the message content must appear
	if !strings.Contains(got, "Warning: be careful") {
		t.Errorf("output %q missing Warning prefix", got)
	}

	// output must match aurora.Yellow rendering
	want := fmt.Sprintln(aurora.Yellow(fmt.Sprintf("Warning: %s", "be careful")))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCliOutputComplete asserts Complete prefix is rendered through aurora.Green.
func TestCliOutputComplete(t *testing.T) {
	// capture output
	got := captureStdout(t, func() {
		CliOutputComplete("all done")
	})

	// the message content must appear
	if !strings.Contains(got, "Complete: all done") {
		t.Errorf("output %q missing Complete prefix", got)
	}

	// output must match aurora.Green rendering
	want := fmt.Sprintln(aurora.Green(fmt.Sprintf("Complete: %s", "all done")))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCliOutputHandlersEmptyMessage covers boundary case where every helper receives an empty string.
func TestCliOutputHandlersEmptyMessage(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string)
		want string
	}{
		{name: "info", fn: CliOutputInfo, want: "Info: "},
		{name: "notice", fn: CliOutputNotice, want: "Notice: "},
		{name: "warning", fn: CliOutputWarning, want: "Warning: "},
		{name: "complete", fn: CliOutputComplete, want: "Complete: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// call each helper with empty input
			got := captureStdout(t, func() {
				tc.fn("")
			})
			// the prefix must still be present
			if !strings.Contains(got, tc.want) {
				t.Errorf("empty-message output %q missing prefix %q", got, tc.want)
			}
			// output must end in a newline from Println/Printf
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("output %q missing trailing newline", got)
			}
		})
	}
}
