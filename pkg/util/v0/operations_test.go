package v0

import (
	"errors"
	"strings"
	"testing"
)

// newNoopOperation() returns an Operation whose hooks all succeed and record
// invocations into the provided call log.
func newNoopOperation(name string, calls *[]string) Operation {
	return Operation{
		Name: name,
		Get: func() error {
			*calls = append(*calls, name+":get")
			return nil
		},
		Create: func() error {
			*calls = append(*calls, name+":create")
			return nil
		},
		Replace: func(s string) error {
			*calls = append(*calls, name+":replace:"+s)
			return nil
		},
		Delete: func() error {
			*calls = append(*calls, name+":delete")
			return nil
		},
	}
}

// TestAppendOperation covers that AppendOperation appends operations to the
// stack in the order they are added.
func TestAppendOperation(t *testing.T) {
	// setup: build two operations
	var calls []string
	ops := &Operations{}
	a := newNoopOperation("a", &calls)
	b := newNoopOperation("b", &calls)

	// action: append both
	ops.AppendOperation(a)
	ops.AppendOperation(b)

	// assert: stack length and order preserved
	if len(ops.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops.Operations))
	}
	if ops.Operations[0].Name != "a" || ops.Operations[1].Name != "b" {
		t.Fatalf("expected [a, b], got [%s, %s]", ops.Operations[0].Name, ops.Operations[1].Name)
	}
}

// TestOperationsGet covers happy path (all get hooks run) and error path (first
// failure short-circuits and wraps the operation name).
func TestOperationsGet(t *testing.T) {
	t.Run("all succeed", func(t *testing.T) {
		// setup: two noop ops
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		ops.AppendOperation(newNoopOperation("b", &calls))

		// action: run all get hooks
		if err := ops.Get(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// assert: both hooks ran, in order
		want := []string{"a:get", "b:get"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("short-circuits on first error", func(t *testing.T) {
		// setup: middle op fails; third should never run
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		bad := newNoopOperation("b", &calls)
		bad.Get = func() error { return errors.New("boom") }
		ops.AppendOperation(bad)
		ops.AppendOperation(newNoopOperation("c", &calls))

		// action: run gets
		err := ops.Get()

		// assert: error surfaces failing op name and wraps original
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to get b") || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("unexpected error message: %v", err)
		}
		// assert: third op did not run
		if containsString(calls, "c:get") {
			t.Fatalf("expected c:get to be skipped, calls=%v", calls)
		}
	})
}

// TestOperationsCreate covers happy path (all create hooks run) and error path
// (failing op triggers reverse-order cleanup of prior successes).
func TestOperationsCreate(t *testing.T) {
	t.Run("all succeed", func(t *testing.T) {
		// setup: two noop ops
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		ops.AppendOperation(newNoopOperation("b", &calls))

		// action: run all create hooks
		if err := ops.Create(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// assert: both create hooks ran, no delete cleanups triggered
		want := []string{"a:create", "b:create"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("failure triggers reverse cleanup of successful predecessors", func(t *testing.T) {
		// setup: a and b succeed, c fails, d never runs
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		ops.AppendOperation(newNoopOperation("b", &calls))
		bad := newNoopOperation("c", &calls)
		bad.Create = func() error { return errors.New("create-fail") }
		ops.AppendOperation(bad)
		ops.AppendOperation(newNoopOperation("d", &calls))

		// action: run creates
		err := ops.Create()

		// assert: error names the failing op and includes the underlying cause
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to create c") || !strings.Contains(err.Error(), "create-fail") {
			t.Fatalf("unexpected error message: %v", err)
		}

		// assert: predecessors deleted in reverse order (b then a); c itself not deleted
		want := []string{"a:create", "b:create", "b:delete", "a:delete"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("failure on first op does not attempt cleanup", func(t *testing.T) {
		// setup: only op fails immediately
		var calls []string
		ops := &Operations{}
		bad := newNoopOperation("a", &calls)
		bad.Create = func() error { return errors.New("boom") }
		ops.AppendOperation(bad)

		// action: run creates
		err := ops.Create()

		// assert: error returned, no delete calls
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if containsString(calls, "a:delete") {
			t.Fatalf("expected no delete call, calls=%v", calls)
		}
	})

	t.Run("cleanup delete failure joins into multi-error", func(t *testing.T) {
		// setup: a succeeds, b fails on create, and a's delete also fails
		var calls []string
		ops := &Operations{}
		a := newNoopOperation("a", &calls)
		a.Delete = func() error { return errors.New("delete-fail") }
		ops.AppendOperation(a)
		bad := newNoopOperation("b", &calls)
		bad.Create = func() error { return errors.New("create-fail") }
		ops.AppendOperation(bad)

		// action: run creates
		err := ops.Create()

		// assert: multi-error carries both messages
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "create-fail") {
			t.Fatalf("expected create failure in error, got %q", msg)
		}
		if !strings.Contains(msg, "delete-fail") {
			t.Fatalf("expected delete failure in error, got %q", msg)
		}
	})
}

// TestOperationsReplace covers happy path with argument propagation and
// early-return on failure.
func TestOperationsReplace(t *testing.T) {
	t.Run("all succeed with arg propagation", func(t *testing.T) {
		// setup: two ops that record replace arg
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		ops.AppendOperation(newNoopOperation("b", &calls))

		// action: replace with a name
		if err := ops.Replace("target"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// assert: both hooks got the arg
		want := []string{"a:replace:target", "b:replace:target"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("short-circuits on first error", func(t *testing.T) {
		// setup: middle op fails
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		bad := newNoopOperation("b", &calls)
		bad.Replace = func(string) error { return errors.New("boom") }
		ops.AppendOperation(bad)
		ops.AppendOperation(newNoopOperation("c", &calls))

		// action: run replaces
		err := ops.Replace("x")

		// assert: error wraps the failing op name and cause
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to replace b") || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("unexpected error message: %v", err)
		}
		// assert: third op skipped
		if containsString(calls, "c:replace:x") {
			t.Fatalf("expected c:replace to be skipped, calls=%v", calls)
		}
	})
}

// TestOperationsDelete covers reverse-order deletion, empty-stack behavior, and
// multi-error accumulation across failing deletes.
func TestOperationsDelete(t *testing.T) {
	t.Run("all succeed in reverse order", func(t *testing.T) {
		// setup: three noop ops
		var calls []string
		ops := &Operations{}
		ops.AppendOperation(newNoopOperation("a", &calls))
		ops.AppendOperation(newNoopOperation("b", &calls))
		ops.AppendOperation(newNoopOperation("c", &calls))

		// action: delete all
		if err := ops.Delete(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// assert: deletes fire in reverse insertion order
		want := []string{"c:delete", "b:delete", "a:delete"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	})

	t.Run("empty stack returns nil", func(t *testing.T) {
		// setup: empty operations
		ops := &Operations{}

		// action + assert: no operations, no error
		if err := ops.Delete(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("delete failures accumulate but do not stop iteration", func(t *testing.T) {
		// setup: a and c fail on delete, b succeeds
		var calls []string
		ops := &Operations{}
		a := newNoopOperation("a", &calls)
		a.Delete = func() error {
			calls = append(calls, "a:delete")
			return errors.New("a-fail")
		}
		ops.AppendOperation(a)
		ops.AppendOperation(newNoopOperation("b", &calls))
		c := newNoopOperation("c", &calls)
		c.Delete = func() error {
			calls = append(calls, "c:delete")
			return errors.New("c-fail")
		}
		ops.AppendOperation(c)

		// action: delete all
		err := ops.Delete()

		// assert: every delete ran despite errors
		want := []string{"c:delete", "b:delete", "a:delete"}
		if !stringsEqual(calls, want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}

		// assert: both failures reported
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "failed to delete a") || !strings.Contains(msg, "a-fail") {
			t.Fatalf("expected a failure in error, got %q", msg)
		}
		if !strings.Contains(msg, "failed to delete c") || !strings.Contains(msg, "c-fail") {
			t.Fatalf("expected c failure in error, got %q", msg)
		}
	})
}

// stringsEqual reports whether two string slices are element-wise equal.
func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// containsString reports whether target appears in slice.
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
