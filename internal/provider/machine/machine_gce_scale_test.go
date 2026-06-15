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

// checkpointPayload builds an instance-unique checkpoint-format state blob that
// embeds the instance name. It has no top-level "deployment" key, so it stays on
// the direct atomic-write branch.
func checkpointPayload(name string) []byte {
	return []byte(fmt.Sprintf(
		`{"version":3,"checkpoint":{"stack":"gce/%s","instance":%q,"latest":{}}}`,
		name, name,
	))
}

// writeStateAtomically mimics the Pulumi engine's own state write: it ensures
// the state dir exists and writes the payload atomically via temp + rename at
// the resolved state file path. This is the production read source that
// ReadStateFile and streaming consume.
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

func TestStateDirIsolation_N2000_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping N2000 scale test in -short mode")
	}

	const (
		n = 2000
		k = 64
	)
	// SHARED root: isolation must come from the per-instance name segment, the
	// same way production isolates instances under one ~/.threeport/pulumi-state
	// root.
	root := t.TempDir()

	// capture the goroutine baseline before spawning workers; the post-drain
	// check compares against this. This is a coarse secondary check only.
	baselineGoroutines := runtime.NumGoroutine()

	var (
		inFlight  atomic.Int64
		peak      atomic.Int64
		failures  atomic.Int64
		firstErr  atomic.Value // string
		pathsMu   sync.Mutex
		pathsSeen = make(map[string]string, n) // path -> instance name
	)

	recordErr := func(format string, args ...any) {
		failures.Add(1)
		firstErr.CompareAndSwap(nil, fmt.Sprintf(format, args...))
	}

	// blocking-acquire worker pool of width K: a buffered channel acquired
	// blockingly so bodies actually run concurrently at width K under -race.
	sem := make(chan struct{}, k)
	var wg sync.WaitGroup

	for idx := 0; idx < n; idx++ {
		name := fmt.Sprintf("vm-%05d", idx)
		wg.Add(1)
		sem <- struct{}{} // blocking acquire
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			// atomic in-flight counter is the assertion: increment at the top
			// of the worker body, decrement on return, and assert the cap holds
			cur := inFlight.Add(1)
			defer inFlight.Add(-1)
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

			pathsMu.Lock()
			if prev, ok := pathsSeen[path]; ok {
				recordErr("path collision: %s shared by %s and %s", path, prev, name)
			}
			pathsSeen[path] = name
			pathsMu.Unlock()
		}(name)
	}

	wg.Wait()

	if got := failures.Load(); got > 0 {
		if msg, ok := firstErr.Load().(string); ok {
			t.Fatalf("%d worker failures; first: %s", got, msg)
		}
		t.Fatalf("%d worker failures", got)
	}

	// all 2000 resolved paths are distinct
	if len(pathsSeen) != n {
		t.Fatalf("expected %d distinct state paths, got %d", n, len(pathsSeen))
	}

	// each instance's state dir contains ONLY its own state file
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

	// peak in-flight never exceeded the worker cap; counter drained to 0
	if p := peak.Load(); p > k {
		t.Fatalf("peak in-flight %d exceeded worker cap %d", p, k)
	}
	if c := inFlight.Load(); c != 0 {
		t.Fatalf("in-flight counter did not drain to 0: %d", c)
	}

	// goroutine count back to ~baseline: a COARSE secondary check only, never
	// the primary assertion and never a flat N-goroutine expectation.
	assertGoroutinesDrain(t, baselineGoroutines)
}

func TestStateDirIsolation_PulumiBacked_RoundTrip(t *testing.T) {
	requirePulumi(t)

	const n = 50
	root := t.TempDir()

	var (
		failures sync.Map // name -> error string
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, 8)

	for idx := 0; idx < n; idx++ {
		name := fmt.Sprintf("pvm-%03d", idx)
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			i := NewGceMachineInfra(name, provider.WithStateDirRoot(root))

			// write leg through the REAL SetStackState (checkpoint branch:
			// UpsertStack init + atomic temp+rename)
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

func TestStateDirIsolation_ConcurrentReadWrite_Race(t *testing.T) {
	root := t.TempDir()

	a := NewGceMachineInfra("racer-a", provider.WithStateDirRoot(root))
	b := NewGceMachineInfra("racer-b", provider.WithStateDirRoot(root))

	pathA, err := a.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath(a): %v", err)
	}
	payloadA := checkpointPayload("racer-a")

	// seed B with its own payload so reads have a stable, B-only file
	pathB, err := b.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath(b): %v", err)
	}
	if err := writeStateAtomically(pathB, checkpointPayload("racer-b")); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	const iterations = 500
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// goroutine A loops writes at instance A's resolved path
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

	// goroutine B loops reads on instance B and must never observe A's payload
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

func TestStateFilePathCollision_DistinctNames(t *testing.T) {
	root := t.TempDir()

	// 100 names differing only by suffix/case must resolve to distinct paths
	names := make([]string, 0, 100)
	for idx := 0; idx < 50; idx++ {
		names = append(names, fmt.Sprintf("node-%d", idx))
		names = append(names, fmt.Sprintf("Node-%d", idx)) // case variant
	}

	seen := make(map[string]string, len(names))
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

// assertGoroutinesDrain polls the goroutine count back toward the pre-work
// baseline as a coarse secondary check. It is never the gate: a slack margin
// absorbs runtime and test-harness goroutines, and it only fails if goroutines
// stay far above baseline, signalling a real leak rather than scheduling jitter.
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
	// secondary check only: fail only when egregiously above baseline
	current := runtime.NumGoroutine()
	if current > baseline*4+slack {
		t.Errorf("goroutines did not drain: %d still running (baseline ~%d)", current, baseline)
	}
}
