package v0

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// captureStdout runs fn while redirecting os.Stdout to a pipe and returns
// everything that was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	// save original stdout so it can be restored on return
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open pipe: %v", err)
	}
	os.Stdout = w

	// drain the read end concurrently so the writer can't block on a full pipe
	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	fn()

	// restore stdout and close the write end so the copier sees EOF
	_ = w.Close()
	os.Stdout = orig
	wg.Wait()
	_ = r.Close()

	return buf.String()
}

// TestErrorPrintsMessageAndError covers Error() emitting both the caller's
// message and the wrapped error when err is non-nil.
func TestErrorPrintsMessageAndError(t *testing.T) {
	// invoke Error with both a message and a concrete error
	out := captureStdout(t, func() {
		Error("something failed", errors.New("boom"))
	})

	// output should carry the Error: prefix, the message, and the error text
	if !strings.Contains(out, "Error:") {
		t.Errorf("expected Error: prefix, got %q", out)
	}
	if !strings.Contains(out, "something failed") {
		t.Errorf("expected message in output, got %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("expected wrapped err in output, got %q", out)
	}
}

// TestErrorNilErrPrintsMessageOnly covers Error() omitting the err suffix
// when err is nil.
func TestErrorNilErrPrintsMessageOnly(t *testing.T) {
	// invoke Error with a message and nil err
	out := captureStdout(t, func() {
		Error("just a message", nil)
	})

	// output should still carry the Error: prefix and the caller's message
	if !strings.Contains(out, "Error:") {
		t.Errorf("expected Error: prefix, got %q", out)
	}
	if !strings.Contains(out, "just a message") {
		t.Errorf("expected message in output, got %q", out)
	}
}

// TestPrefixedOutputHelpers covers Info/Notice/Warning/Complete each emitting
// their labeled prefix followed by the message.
func TestPrefixedOutputHelpers(t *testing.T) {
	// each case pairs a helper function with the substring expected in stdout
	cases := []struct {
		name   string
		fn     func(string)
		msg    string
		prefix string
	}{
		{name: "Info", fn: Info, msg: "info body", prefix: "Info:"},
		{name: "Notice", fn: Notice, msg: "notice body", prefix: "Notice:"},
		{name: "Warning", fn: Warning, msg: "warn body", prefix: "Warning:"},
		{name: "Complete", fn: Complete, msg: "done body", prefix: "Complete:"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the helper under test
			out := captureStdout(t, func() {
				tc.fn(tc.msg)
			})

			// output must contain both the labeled prefix and the caller's message
			if !strings.Contains(out, tc.prefix) {
				t.Errorf("expected prefix %q, got %q", tc.prefix, out)
			}
			if !strings.Contains(out, tc.msg) {
				t.Errorf("expected message %q, got %q", tc.msg, out)
			}
		})
	}
}

// sampleObj is a minimal struct used to exercise the marshal paths of
// YamlObjectOutput and JsonObjectOutput.
type sampleObj struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TestYamlObjectOutputSingleObject covers YamlObjectOutput() marshaling a
// non-slice value directly.
func TestYamlObjectOutputSingleObject(t *testing.T) {
	// single struct value takes the non-slice branch
	out := captureStdout(t, func() {
		if err := YamlObjectOutput(sampleObj{Name: "a", Count: 1}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// yaml output should carry the field names as yaml keys
	if !strings.Contains(out, "name: a") {
		t.Errorf("expected yaml field name, got %q", out)
	}
	if !strings.Contains(out, "count: 1") {
		t.Errorf("expected yaml field count, got %q", out)
	}
}

// TestYamlObjectOutputSingleElementSlice covers YamlObjectOutput() unwrapping
// a slice of length one to marshal only that single element.
func TestYamlObjectOutputSingleElementSlice(t *testing.T) {
	// slice with exactly one element takes the unwrap-single branch
	out := captureStdout(t, func() {
		if err := YamlObjectOutput([]sampleObj{{Name: "solo", Count: 7}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// unwrapped output should NOT contain a leading `- ` list marker
	if strings.Contains(out, "- name:") {
		t.Errorf("expected single object, got list output %q", out)
	}
	if !strings.Contains(out, "name: solo") {
		t.Errorf("expected yaml name field, got %q", out)
	}
}

// TestYamlObjectOutputMultiElementSlice covers YamlObjectOutput() marshaling
// the full slice when it contains more than one element.
func TestYamlObjectOutputMultiElementSlice(t *testing.T) {
	// slice with two elements takes the multi-element branch
	out := captureStdout(t, func() {
		err := YamlObjectOutput([]sampleObj{
			{Name: "a", Count: 1},
			{Name: "b", Count: 2},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// yaml list output should contain list markers and both names
	if !strings.Contains(out, "- ") {
		t.Errorf("expected yaml list markers, got %q", out)
	}
	if !strings.Contains(out, "name: a") || !strings.Contains(out, "name: b") {
		t.Errorf("expected both list names in %q", out)
	}
}

// TestYamlObjectOutputEmptySlice covers YamlObjectOutput() marshaling an
// empty slice as an empty yaml list, not a single object.
func TestYamlObjectOutputEmptySlice(t *testing.T) {
	// empty slice: len != 1 so takes the multi-element branch
	out := captureStdout(t, func() {
		if err := YamlObjectOutput([]sampleObj{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// empty slice should marshal to something like `[]` or `null`
	trim := strings.TrimSpace(out)
	if trim != "[]" && trim != "null" {
		t.Errorf("expected empty list or null, got %q", trim)
	}
}

// TestYamlObjectOutputMarshalError covers YamlObjectOutput() returning an
// error when the underlying marshal cannot represent the value.
func TestYamlObjectOutputMarshalError(t *testing.T) {
	// channel values are unrepresentable in json/yaml, forcing a marshal error
	captureStdout(t, func() {
		err := YamlObjectOutput(make(chan int))
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}

// TestYamlObjectOutputMarshalErrorSingleSlice covers YamlObjectOutput()
// propagating an error from the single-element slice branch.
func TestYamlObjectOutputMarshalErrorSingleSlice(t *testing.T) {
	// slice-of-channels of length 1 exercises the single-element error path
	captureStdout(t, func() {
		err := YamlObjectOutput([]chan int{make(chan int)})
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}

// TestYamlObjectOutputMarshalErrorMultiSlice covers YamlObjectOutput()
// propagating an error from the multi-element slice branch.
func TestYamlObjectOutputMarshalErrorMultiSlice(t *testing.T) {
	// slice-of-channels of length 2 exercises the multi-element error path
	captureStdout(t, func() {
		err := YamlObjectOutput([]chan int{make(chan int), make(chan int)})
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}

// TestJsonObjectOutputSingleObject covers JsonObjectOutput() marshaling a
// non-slice value directly with indentation.
func TestJsonObjectOutputSingleObject(t *testing.T) {
	// single struct value takes the non-slice branch
	out := captureStdout(t, func() {
		if err := JsonObjectOutput(sampleObj{Name: "a", Count: 1}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// json output uses two-space indent and quoted keys
	if !strings.Contains(out, `"name": "a"`) {
		t.Errorf("expected json name field, got %q", out)
	}
	if !strings.Contains(out, `"count": 1`) {
		t.Errorf("expected json count field, got %q", out)
	}
	if !strings.Contains(out, "  ") {
		t.Errorf("expected indented json, got %q", out)
	}
}

// TestJsonObjectOutputSingleElementSlice covers JsonObjectOutput() unwrapping
// a slice of length one to marshal only that element.
func TestJsonObjectOutputSingleElementSlice(t *testing.T) {
	// slice with exactly one element takes the unwrap-single branch
	out := captureStdout(t, func() {
		if err := JsonObjectOutput([]sampleObj{{Name: "solo", Count: 7}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// unwrapped output should be a bare object, not a json array
	trim := strings.TrimSpace(out)
	if strings.HasPrefix(trim, "[") {
		t.Errorf("expected single object, got list output %q", trim)
	}
	if !strings.Contains(out, `"name": "solo"`) {
		t.Errorf("expected json name field, got %q", out)
	}
}

// TestJsonObjectOutputMultiElementSlice covers JsonObjectOutput() marshaling
// the full slice when it contains more than one element.
func TestJsonObjectOutputMultiElementSlice(t *testing.T) {
	// slice with two elements takes the multi-element branch
	out := captureStdout(t, func() {
		err := JsonObjectOutput([]sampleObj{
			{Name: "a", Count: 1},
			{Name: "b", Count: 2},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// json list output should be an array containing both names
	trim := strings.TrimSpace(out)
	if !strings.HasPrefix(trim, "[") {
		t.Errorf("expected json array, got %q", trim)
	}
	if !strings.Contains(out, `"name": "a"`) || !strings.Contains(out, `"name": "b"`) {
		t.Errorf("expected both list names in %q", out)
	}
}

// TestJsonObjectOutputEmptySlice covers JsonObjectOutput() emitting an empty
// json array for an empty slice.
func TestJsonObjectOutputEmptySlice(t *testing.T) {
	// empty slice: len != 1 so takes the multi-element branch
	out := captureStdout(t, func() {
		if err := JsonObjectOutput([]sampleObj{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// empty slice should marshal to `[]`
	trim := strings.TrimSpace(out)
	if trim != "[]" {
		t.Errorf("expected empty array, got %q", trim)
	}
}

// TestJsonObjectOutputMarshalError covers JsonObjectOutput() returning the
// error from the non-slice marshal branch.
func TestJsonObjectOutputMarshalError(t *testing.T) {
	// channel values are unrepresentable in json, forcing a marshal error
	captureStdout(t, func() {
		err := JsonObjectOutput(make(chan int))
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}

// TestJsonObjectOutputMarshalErrorSingleSlice covers JsonObjectOutput()
// propagating an error from the single-element slice branch.
func TestJsonObjectOutputMarshalErrorSingleSlice(t *testing.T) {
	// slice-of-channels of length 1 exercises the single-element error path
	captureStdout(t, func() {
		err := JsonObjectOutput([]chan int{make(chan int)})
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}

// TestJsonObjectOutputMarshalErrorMultiSlice covers JsonObjectOutput()
// propagating an error from the multi-element slice branch.
func TestJsonObjectOutputMarshalErrorMultiSlice(t *testing.T) {
	// slice-of-channels of length 2 exercises the multi-element error path
	captureStdout(t, func() {
		err := JsonObjectOutput([]chan int{make(chan int), make(chan int)})
		if err == nil {
			t.Fatalf("expected marshal error, got nil")
		}
	})
}
