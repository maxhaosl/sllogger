// Package testutil provides reusable, dependency-free test fixtures shared across
// the sllogger test suites. It intentionally does NOT import the slcore package:
// slcore's white-box tests live in package slcore, and a cycle
// (slcore -> testutil -> slcore) is not allowed. slcore-dependent doubles
// (stub encoders, map encoders) therefore stay in the slcore test package.
package testutil

import (
	"bytes"
	"os"
	"sync"
	"testing"
	"time"
)

// Sink is a WriteSyncer test double that records every Write and Sync. It is the
// shared recording sink for assertions on the bytes handed to a logger.
type Sink struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  int
	syncs   int
	writeEr error
}

// Write implements io.Writer, recording the call (and failing if a write error
// was injected via SetWriteError).
func (w *Sink) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeEr != nil {
		return 0, w.writeEr
	}
	w.writes++
	return w.buf.Write(p)
}

// Sync implements WriteSyncer, recording the call.
func (w *Sink) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncs++
	return nil
}

// String returns everything written so far.
func (w *Sink) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// Counts returns the number of Write and Sync calls observed.
func (w *Sink) Counts() (writes, syncs int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes, w.syncs
}

// SetWriteError makes subsequent Writes fail with err.
func (w *Sink) SetWriteError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeEr = err
}

// TempDir creates a temp directory for the test and registers cleanup.
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sllogger-test-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// MustParseTime parses value with layout and panics on error.
func MustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}
