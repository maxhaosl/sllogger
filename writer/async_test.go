// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package writer

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAsyncWriterFlushOnSync(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.FlushInterval = time.Hour // only Sync/Close should flush
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	for i := 0; i < 10; i++ {
		aw.Write([]byte("entry\n"))
	}
	if err := aw.Sync(); err != nil {
		t.Fatal(err)
	}

	// The worker has processed the sync request, so the file must be complete.
	data, err := os.ReadFile(rw.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "entry"); got != 10 {
		t.Fatalf("entries after Sync = %d, want 10", got)
	}
	if got := aw.Metrics().Enqueued.Load(); got != 10 {
		t.Fatalf("enqueued = %d", got)
	}
}

func TestAsyncWriterBatching(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.BatchSize = 5
	cfg.FlushInterval = 10 * time.Millisecond
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	for i := 0; i < 20; i++ {
		aw.Write([]byte("e\n"))
	}
	aw.Close()

	// 20 entries with BatchSize 5 => at most 4 downstream writes.
	if got := rw.Metrics().Written.Load(); got > 4 {
		t.Fatalf("downstream writes = %d, batching ineffective", got)
	}
	if got := strings.Count(readAll(t, rw.path), "e"); got != 20 {
		t.Fatalf("entries = %d, want 20", got)
	}
}

func TestAsyncWriterBlockingMode(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.QueueSize = 2
	cfg.BlockOnFull = true
	cfg.FlushInterval = time.Millisecond
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	const n = 2000
	for i := 0; i < n; i++ {
		aw.Write([]byte("b\n"))
	}
	aw.Close()

	if got := strings.Count(readAll(t, rw.path), "b"); got != n {
		t.Fatalf("entries = %d, want %d", got, n)
	}
	if got := aw.Metrics().Dropped.Load(); got != 0 {
		t.Fatalf("dropped = %d, want 0 in blocking mode", got)
	}
}

func TestAsyncWriterDropAccounting(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.QueueSize = 1
	cfg.FlushInterval = time.Hour
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	// Fill the queue so the worker never drains it during the test.
	for i := 0; i < 1000; i++ {
		aw.Write([]byte("d\n"))
	}
	m := aw.Metrics().Snapshot()
	if m.Enqueued+m.Dropped != 1000 {
		t.Fatalf("enqueued+dropped = %d, want 1000", m.Enqueued+m.Dropped)
	}
	if m.Dropped == 0 {
		t.Fatal("expected drops with a tiny queue and no draining")
	}
	aw.Close()
}

func TestAsyncWriterConcurrentProducers(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.QueueSize = 1000
	cfg.BlockOnFull = true
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	const (
		goroutines = 12
		perRoutine = 500
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perRoutine; i++ {
				aw.Write([]byte("c\n"))
			}
		}()
	}
	wg.Wait()
	aw.Close()

	if got := strings.Count(readAll(t, rw.path), "c"); got != goroutines*perRoutine {
		t.Fatalf("entries = %d, want %d", got, goroutines*perRoutine)
	}
}

func TestAsyncWriterCloseIsIdempotent(t *testing.T) {
	cfg, _ := testConfig(t)
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	aw.Write([]byte("x\n"))

	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := aw.Write([]byte("y\n")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v, want ErrClosed", err)
	}
	if err := aw.Sync(); err != nil {
		t.Fatalf("Sync after Close = %v, want nil", err)
	}
}

func TestAsyncWriterBufferReuse(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.BlockOnFull = true
	cfg.FlushInterval = time.Millisecond
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	// Writing payloads larger than the initial pooled capacity verifies that
	// recycled buffers grow safely and are never reused across entries.
	for i := 1; i <= 200; i++ {
		aw.Write([]byte(strings.Repeat("q", i) + "\n"))
	}
	aw.Close()

	data := readAll(t, rw.path)
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) != 200 {
		t.Fatalf("lines = %d, want 200", len(lines))
	}
	for i, line := range lines {
		if len(line) != i+1 {
			t.Fatalf("line %d has length %d, want %d", i, len(line), i+1)
		}
	}
}

func TestAsyncWriterSyncDuringClose(t *testing.T) {
	cfg, _ := testConfig(t)
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	aw.Write([]byte("z\n"))

	// A Sync issued while closing must not deadlock.
	done := make(chan struct{})
	go func() {
		aw.Sync()
		close(done)
	}()
	aw.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Sync deadlocked during Close")
	}
}
