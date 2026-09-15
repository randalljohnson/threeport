package machine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"

	"github.com/threeport/threeport/internal/provider"
)

// checkpointPayload returns a checkpoint-format state blob tagged with name
// so a read-back from the wrong instance is distinguishable. No top-level
// deployment key, so SetStackState writes the file instead of importing.
func checkpointPayload(name string) []byte {
	return []byte(fmt.Sprintf(
		`{"version":3,"checkpoint":{"stack":"gce/%s","instance":%q,"latest":{}}}`,
		name, name,
	))
}

// writeStateAtomically writes payload through a temp file then rename.
func writeStateAtomically(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0644); err != nil {
		return fmt.Errorf("failed to write temp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename state: %w", err)
	}
	return nil
}

// TestStateDirIsolation_N2000_RoundTrip covers 2000 GCE machine workspaces
// sharing one state dir root: each writes and reads only its own checkpoint.
func TestStateDirIsolation_N2000_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping N2000 scale test in -short mode")
	}

	const (
		n = 2000
		k = 64
	)
	// share one state dir root; isolation is the per-instance name
	root := t.TempDir()

	baselineGoroutines := runtime.NumGoroutine()

	var (
		inFlight  atomic.Int64
		peak      atomic.Int64
		failures  atomic.Int64
		firstErr  atomic.Value
		pathsMu   sync.Mutex
		pathsSeen = make(map[string]string, n)
	)

	recordErr := func(format string, args ...any) {
		failures.Add(1)
		firstErr.CompareAndSwap(nil, fmt.Sprintf(format, args...))
	}

	sem := make(chan struct{}, k)
	var wg sync.WaitGroup

	// launch n workers capped at k concurrent
	for idx := 0; idx < n; idx++ {
		name := fmt.Sprintf("vm-%05d", idx)
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			cur := inFlight.Add(1)
			defer inFlight.Add(-1)
			// record peak in-flight
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			if cur > k {
				recordErr("in-flight %d exceeded worker cap %d", cur, k)
				return
			}

			i := NewGceMachineInfra(name, provider.WithStateDirRoot(root))
			path, err := i.GetStateFilePath()
			if err != nil {
				recordErr("GetStateFilePath(%s): %v", name, err)
				return
			}

			// write checkpoint then read it back
			payload := checkpointPayload(name)
			if err := writeStateAtomically(path, payload); err != nil {
				recordErr("write(%s): %v", name, err)
				return
			}

			readBack, err := i.ReadStateFile()
			if err != nil {
				recordErr("ReadStateFile(%s): %v", name, err)
				return
			}
			if readBack == nil {
				recordErr("ReadStateFile(%s) returned nil", name)
				return
			}
			if string(*readBack) != string(payload) {
				recordErr("cross-talk: instance %s read back %s, want %s", name, *readBack, payload)
				return
			}

			// record path uniqueness
			pathsMu.Lock()
			if prev, ok := pathsSeen[path]; ok {
				recordErr("path collision: %s shared by %s and %s", path, prev, name)
			}
			pathsSeen[path] = name
			pathsMu.Unlock()
		}(name)
	}

	wg.Wait()

	// assert no worker failures
	if got := failures.Load(); got > 0 {
		if msg, ok := firstErr.Load().(string); ok {
			t.Fatalf("%d worker failures; first: %s", got, msg)
		}
		t.Fatalf("%d worker failures", got)
	}

	if len(pathsSeen) != n {
		t.Fatalf("expected %d distinct state paths, got %d", n, len(pathsSeen))
	}

	// assert each state dir holds only its json
	for path, name := range pathsSeen {
		dir := filepath.Dir(path)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}
		if len(entries) != 1 {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("instance %s state dir %s holds %d files: %v", name, dir, len(entries), names)
		}
		if entries[0].Name() != name+".json" {
			t.Fatalf("instance %s state dir holds %s, want %s.json", name, entries[0].Name(), name)
		}
	}

	// assert peak did not exceed the worker cap and in-flight drained
	if p := peak.Load(); p > k {
		t.Fatalf("peak in-flight %d exceeded worker cap %d", p, k)
	}
	if c := inFlight.Load(); c != 0 {
		t.Fatalf("in-flight counter did not drain to 0: %d", c)
	}

	assertGoroutinesDrain(t, baselineGoroutines)
}

// TestStateDirIsolation_PulumiBacked_RoundTrip covers SetStackState then
// ReadStateFile on many GCE workspaces sharing one state dir root.
func TestStateDirIsolation_PulumiBacked_RoundTrip(t *testing.T) {
	requirePulumi(t)

	const n = 50
	root := t.TempDir()

	var (
		failures sync.Map
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, 8)

	// restore checkpoint through SetStackState then read it back
	for idx := 0; idx < n; idx++ {
		name := fmt.Sprintf("pvm-%03d", idx)
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			i := NewGceMachineInfra(name, provider.WithStateDirRoot(root))

			blob := datatypes.JSON(checkpointPayload(name))
			if err := i.SetStackState(&blob); err != nil {
				failures.Store(name, fmt.Sprintf("SetStackState: %v", err))
				return
			}

			readBack, err := i.ReadStateFile()
			if err != nil {
				failures.Store(name, fmt.Sprintf("ReadStateFile: %v", err))
				return
			}
			if readBack == nil {
				failures.Store(name, "ReadStateFile returned nil")
				return
			}
			if string(*readBack) != string(blob) {
				failures.Store(name, fmt.Sprintf("cross-talk: read %s, want %s", *readBack, blob))
				return
			}
		}(name)
	}
	wg.Wait()

	// report every instance that failed the round trip
	failed := false
	failures.Range(func(key, value any) bool {
		failed = true
		t.Errorf("instance %v: %v", key, value)
		return true
	})
	if failed {
		t.FailNow()
	}
}

// TestStateDirIsolation_ConcurrentReadWrite_Race covers a writer on one
// instance and a reader on another sharing a root; the reader never sees
// the writer's payload.
func TestStateDirIsolation_ConcurrentReadWrite_Race(t *testing.T) {
	root := t.TempDir()

	a := NewGceMachineInfra("racer-a", provider.WithStateDirRoot(root))
	b := NewGceMachineInfra("racer-b", provider.WithStateDirRoot(root))

	pathA, err := a.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath(a): %v", err)
	}
	payloadA := checkpointPayload("racer-a")

	pathB, err := b.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath(b): %v", err)
	}
	// seed B so concurrent reads have a stable own payload
	if err := writeStateAtomically(pathB, checkpointPayload("racer-b")); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	const iterations = 500
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// write A's checkpoint while B reads
	go func() {
		defer wg.Done()
		for n := 0; n < iterations; n++ {
			if err := writeStateAtomically(pathA, payloadA); err != nil {
				t.Errorf("write A: %v", err)
				return
			}
		}
		close(done)
	}()

	// assert B never observes A's payload
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			readBack, err := b.ReadStateFile()
			if err != nil {
				t.Errorf("read B: %v", err)
				return
			}
			if readBack != nil && string(*readBack) != string(checkpointPayload("racer-b")) {
				t.Errorf("B observed foreign payload: %s", *readBack)
				return
			}
		}
	}()

	wg.Wait()
}

// TestStateFilePathCollision_DistinctNames rejects a shared state path
// across distinct names, including pairs that differ only in letter case.
func TestStateFilePathCollision_DistinctNames(t *testing.T) {
	root := t.TempDir()

	names := make([]string, 0, 100)
	for idx := 0; idx < 50; idx++ {
		names = append(names, fmt.Sprintf("node-%d", idx))
		names = append(names, fmt.Sprintf("Node-%d", idx))
	}

	seen := make(map[string]string, len(names))
	// assert GetStateFilePath returns a unique path per name
	for _, name := range names {
		i := NewGceMachineInfra(name, provider.WithStateDirRoot(root))
		path, err := i.GetStateFilePath()
		if err != nil {
			t.Fatalf("GetStateFilePath(%s): %v", name, err)
		}
		if path == "" {
			t.Fatalf("GetStateFilePath(%s) returned empty path", name)
		}
		if prev, ok := seen[path]; ok {
			t.Fatalf("path collision: %q shared by %q and %q", path, prev, name)
		}
		seen[path] = name
	}
	if len(seen) != len(names) {
		t.Fatalf("expected %d distinct paths, got %d", len(names), len(seen))
	}
}

// assertGoroutinesDrain waits until NumGoroutine is at most baseline plus slack.
// After retries it fails only when the count exceeds four times baseline plus slack.
func assertGoroutinesDrain(t *testing.T, baseline int) {
	t.Helper()
	const slack = 50
	for attempt := 0; attempt < 20; attempt++ {
		if runtime.NumGoroutine() <= baseline+slack {
			return
		}
		time.Sleep(10 * time.Millisecond)
		runtime.GC()
	}
	current := runtime.NumGoroutine()
	if current > baseline*4+slack {
		t.Errorf("goroutines did not drain: %d still running (baseline ~%d)", current, baseline)
	}
}
