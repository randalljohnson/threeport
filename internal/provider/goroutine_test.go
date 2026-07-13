package provider

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// fastRefreshConfig returns a lifecycle config with a millisecond-scale
// refresh interval so refreshAck tests tick quickly. All other tunables
// keep production-scale values that the tests never reach.
func fastRefreshConfig() LifecycleConfig {
	return LifecycleConfig{
		StaleAckThreshold: 240 * time.Second,
		RefreshInterval:   5 * time.Millisecond,
		SemaphoreCapacity: 5,
		PersistRetries:    1,
		PersistRetryDelay: time.Millisecond,
	}
}

// TestRefreshAck_QuitClean pins the quit-channel branch of refreshAck:
// the loop ticks on the configured refresh interval, returns promptly
// once quit is signalled, and stops invoking the refresh function after
// it returns.
func TestRefreshAck_QuitClean(t *testing.T) {
	restore := setLifecycleConfig(fastRefreshConfig())
	t.Cleanup(restore)

	var calls int64
	refresh := func() error {
		atomic.AddInt64(&calls, 1)
		return nil
	}

	quit := make(chan bool, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshAck(refresh, quit, newTestLogger())
	}()

	// prove the loop is ticking before signalling quit
	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&calls) >= 2
	}, 10*time.Second, time.Millisecond, "refreshAck should tick on the refresh interval")

	quit <- true
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refreshAck did not return after quit signal")
	}

	// after the loop has returned, the call count must freeze; the grace
	// window spans many refresh intervals to catch a stray ticker
	settled := atomic.LoadInt64(&calls)
	assert.Never(t, func() bool {
		return atomic.LoadInt64(&calls) != settled
	}, 100*time.Millisecond, 5*time.Millisecond, "refresh calls must stop after refreshAck returns")
}

// TestRefreshAck_RefreshErrorDoesNotBlock pins the error branch of the
// tick case: refresh failures are logged and swallowed, the loop keeps
// ticking through them, and quit still exits the loop promptly.
func TestRefreshAck_RefreshErrorDoesNotBlock(t *testing.T) {
	restore := setLifecycleConfig(fastRefreshConfig())
	t.Cleanup(restore)

	var calls int64
	refresh := func() error {
		if atomic.AddInt64(&calls, 1) <= 3 {
			return errFakeInfra
		}
		return nil
	}

	quit := make(chan bool, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshAck(refresh, quit, newTestLogger())
	}()

	// the loop must tick well past the three failing calls
	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&calls) >= 6
	}, 10*time.Second, time.Millisecond, "refreshAck should keep ticking through refresh errors")

	quit <- true
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refreshAck did not return after quit signal")
	}
}

// streamHarness wires a fake streamable provider, a save recorder, and
// the channels needed to drive a streamState goroutine under test. The
// watched path is built through a real PulumiWorkspace rooted in a temp
// dir, matching production path construction exactly.
type streamHarness struct {
	t     *testing.T
	infra *fakeStreamableInfra
	path  string

	mu    sync.Mutex
	saved []*datatypes.JSON

	quit chan bool
	done chan struct{}
}

// newStreamHarness resolves a state file path via PulumiWorkspace under
// t.TempDir() and returns a harness around a fake streamable provider
// reporting that path.
func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	ws := NewPulumiWorkspace("test-instance", "test-project", WithStateDirRoot(t.TempDir()))
	path, err := ws.GetStateFilePath()
	require.NoError(t, err)
	return &streamHarness{
		t:     t,
		infra: newFakeStreamableInfra(path),
		path:  path,
		quit:  make(chan bool, 1),
		done:  make(chan struct{}),
	}
}

// start launches streamState in a goroutine; done closes when it
// returns. A cleanup sends a non-blocking quit so a failed test never
// leaks the watcher goroutine.
func (h *streamHarness) start() {
	go func() {
		defer close(h.done)
		streamState(h.infra, h.saveState, h.quit, newTestLogger())
	}()
	h.t.Cleanup(func() {
		select {
		case h.quit <- true:
		default:
		}
	})
}

// saveState records every state pushed by streamState.
func (h *streamHarness) saveState(state *datatypes.JSON) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.saved = append(h.saved, state)
	return nil
}

// saveCount returns the number of recorded saves.
func (h *streamHarness) saveCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.saved)
}

// lastSaved returns the most recently recorded state, or nil if no save
// occurred.
func (h *streamHarness) lastSaved() *datatypes.JSON {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.saved) == 0 {
		return nil
	}
	return h.saved[len(h.saved)-1]
}

// writeStateFile writes content to the watched state file to fire a
// real fsnotify event, creating the parent directory if needed. Uses
// assert (not require) so it is safe inside Eventually conditions,
// which run on a non-test goroutine.
func (h *streamHarness) writeStateFile(content string) {
	if !assert.NoError(h.t, os.MkdirAll(filepath.Dir(h.path), 0755)) {
		return
	}
	assert.NoError(h.t, os.WriteFile(h.path, []byte(content), 0644))
}

// writeSiblingFile writes a differently named file in the watched
// directory; its events must be filtered out by base filename.
func (h *streamHarness) writeSiblingFile(content string) {
	sibling := filepath.Join(filepath.Dir(h.path), "sibling.json")
	if !assert.NoError(h.t, os.MkdirAll(filepath.Dir(h.path), 0755)) {
		return
	}
	assert.NoError(h.t, os.WriteFile(sibling, []byte(content), 0644))
}

// sendQuit signals streamState to stop.
func (h *streamHarness) sendQuit() {
	h.quit <- true
}

// waitDone blocks until streamState returns or fails the test at the
// deadline. Call only from the test goroutine.
func (h *streamHarness) waitDone(deadline time.Duration) {
	h.t.Helper()
	select {
	case <-h.done:
	case <-time.After(deadline):
		h.t.Fatal("streamState did not return before deadline")
	}
}

// TestStreamState_ValidJSON_SavesImmediately pins the happy streaming
// path: streamState creates the state directory itself, watches it, and
// on a real fsnotify write event reads the state file and pushes the
// exact bytes through the save callback.
func TestStreamState_ValidJSON_SavesImmediately(t *testing.T) {
	h := newStreamHarness(t)
	want := validStackState()
	h.infra.setReadState(want, nil)

	h.start()

	// streamState must create the watch directory before the test writes
	// anything; this pins the production MkdirAll on the parent dir
	stateDir := filepath.Dir(h.path)
	require.Eventually(t, func() bool {
		_, err := os.Stat(stateDir)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "streamState should create the state directory")

	// rewrite the file until a save lands; the watcher registration is
	// not observable, so poll-writing absorbs the startup race
	require.Eventually(t, func() bool {
		h.writeStateFile(string(*want))
		return h.saveCount() >= 1
	}, 10*time.Second, 50*time.Millisecond, "state file write should trigger a save")

	got := h.lastSaved()
	require.NotNil(t, got)
	assert.Equal(t, string(*want), string(*got), "saved state must match the bytes read from the state file")
	assert.GreaterOrEqual(t, h.infra.readStateCallCount(), 1, "state file event should trigger a read")

	h.sendQuit()
	h.waitDone(5 * time.Second)
}

// TestStreamState_PartialJSON_Skipped pins the invalid-JSON guard: a
// state file read returning partial JSON is skipped without saving and
// without ending the loop, and a later valid read still saves.
func TestStreamState_PartialJSON_Skipped(t *testing.T) {
	h := newStreamHarness(t)
	partial := jsonPtr(`{"deployment":{"resources":[`)
	h.infra.setReadState(partial, nil)

	h.start()

	// rewrite the file until an event has been consumed, proven by the
	// read counter advancing
	require.Eventually(t, func() bool {
		h.writeStateFile(string(*partial))
		return h.infra.readStateCallCount() >= 1
	}, 10*time.Second, 50*time.Millisecond, "state file write should trigger a read")

	// invalid JSON must never reach the save callback
	assert.Never(t, func() bool {
		return h.saveCount() > 0
	}, 300*time.Millisecond, 20*time.Millisecond, "partial JSON must not be saved")

	// prove the loop survived the skip: a valid read now saves
	want := validStackState()
	h.infra.setReadState(want, nil)
	require.Eventually(t, func() bool {
		h.writeStateFile(string(*want))
		return h.saveCount() >= 1
	}, 10*time.Second, 50*time.Millisecond, "valid state after a skipped partial write should save")

	h.sendQuit()
	h.waitDone(5 * time.Second)
}

// TestStreamState_QuitOrdering_NoLateWrite pins the quit ordering
// contract: once quit has been honored and streamState has returned, a
// subsequent state file write produces no save.
func TestStreamState_QuitOrdering_NoLateWrite(t *testing.T) {
	h := newStreamHarness(t)
	want := validStackState()
	h.infra.setReadState(want, nil)

	h.start()

	// confirm the watcher is live before quitting so the quit interrupts
	// an active watch rather than a not-yet-started one
	require.Eventually(t, func() bool {
		h.writeStateFile(string(*want))
		return h.saveCount() >= 1
	}, 10*time.Second, 50*time.Millisecond, "state file write should trigger a save before quit")

	h.sendQuit()
	h.waitDone(5 * time.Second)

	// the count is frozen once the goroutine has returned; writes after
	// quit must never produce a late save
	settled := h.saveCount()
	h.writeStateFile(string(*want))
	assert.Never(t, func() bool {
		return h.saveCount() > settled
	}, 500*time.Millisecond, 25*time.Millisecond, "no save may occur after quit was honored")
}

// TestStreamState_NonStateFileEvent_Ignored pins the filename filter:
// events for other files in the watched directory trigger neither a
// state file read nor a save, while the loop stays live for real state
// file events.
func TestStreamState_NonStateFileEvent_Ignored(t *testing.T) {
	h := newStreamHarness(t)
	want := validStackState()
	h.infra.setReadState(want, nil)

	h.start()

	// hammer the watched directory with sibling file writes; none may
	// trigger a read or a save
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.writeSiblingFile(`{"unrelated":true}`)
		require.Zero(t, h.infra.readStateCallCount(), "sibling file events must not trigger state file reads")
		require.Zero(t, h.saveCount(), "sibling file events must not trigger saves")
		time.Sleep(25 * time.Millisecond)
	}

	// prove the watcher was live the whole time: a real state file write
	// still produces a save
	require.Eventually(t, func() bool {
		h.writeStateFile(string(*want))
		return h.saveCount() >= 1
	}, 10*time.Second, 50*time.Millisecond, "state file write should still save after sibling events")

	h.sendQuit()
	h.waitDone(5 * time.Second)
}
