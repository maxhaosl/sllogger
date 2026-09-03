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

// Tests derived from go.uber.org/zap/zapcore/write_syncer_test.go.
package slcore

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

type nopSyncer struct{ *bytes.Buffer }

func (nopSyncer) Sync() error { return nil }

type failingSyncer struct {
	WriteSyncer
	err error
}

func (f failingSyncer) Sync() error { return f.err }

func TestAddSync(t *testing.T) {
	// A WriteSyncer is passed through unchanged.
	buf := nopSyncer{&bytes.Buffer{}}
	if got := AddSync(buf); got != WriteSyncer(buf) {
		t.Fatal("AddSync must not wrap an existing WriteSyncer")
	}

	// A plain io.Writer gets a no-op Sync.
	sb := &strings.Builder{}
	ws := AddSync(sb)
	if _, err := ws.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "hi" {
		t.Fatalf("write = %q", sb.String())
	}
	if err := ws.Sync(); err != nil {
		t.Fatalf("Sync on a wrapped writer must be a no-op: %v", err)
	}
}

func TestLockReentrant(t *testing.T) {
	ws := Lock(&countingWriteSyncer{})
	if _, ok := ws.(*lockedWriteSyncer); !ok {
		t.Fatal("Lock did not wrap the syncer")
	}
	// Locking twice must not stack another layer.
	if got := Lock(ws); got != ws {
		t.Fatal("Lock must be idempotent")
	}
}

func TestLockConcurrentWrites(t *testing.T) {
	ws := Lock(nopSyncer{&bytes.Buffer{}})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ws.Write([]byte(strings.Repeat("x", 8)))
			ws.Sync()
		}(i)
	}
	wg.Wait()
	// The race detector (go test -race) proves mutual exclusion; assert the
	// total length only.
	if got := ws.(*lockedWriteSyncer).ws.(nopSyncer).Buffer.Len(); got != 50*8 {
		t.Fatalf("bytes written = %d, want %d", got, 50*8)
	}
}

func TestNewMultiWriteSyncerSingle(t *testing.T) {
	ws := &countingWriteSyncer{}
	if got := NewMultiWriteSyncer(ws); got != WriteSyncer(ws) {
		t.Fatal("a single syncer must be returned as-is")
	}
}

func TestNewMultiWriteSyncerWrites(t *testing.T) {
	a := &countingWriteSyncer{}
	b := &countingWriteSyncer{}
	ws := NewMultiWriteSyncer(a, b)

	n, err := ws.Write([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("payload") {
		t.Fatalf("n = %d", n)
	}
	if a.String() != "payload" || b.String() != "payload" {
		t.Fatalf("a=%q b=%q", a.String(), b.String())
	}
	if err := ws.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, syncs := a.counts(); syncs != 1 {
		t.Fatal("Sync not propagated to the first syncer")
	}
	if _, syncs := b.counts(); syncs != 1 {
		t.Fatal("Sync not propagated to the second syncer")
	}
}

func TestNewMultiWriteSyncerErrors(t *testing.T) {
	bad := &countingWriteSyncer{writeEr: errStub}
	good := &countingWriteSyncer{}
	ws := NewMultiWriteSyncer(bad, good)

	if _, err := ws.Write([]byte("x")); err == nil {
		t.Fatal("expected the write error to be reported")
	}
	// The remaining syncer must still receive the write.
	if good.String() != "x" {
		t.Fatalf("good = %q", good.String())
	}
}

func TestNewMultiWriteSyncerSmallestWriteWins(t *testing.T) {
	short := shortWriteSyncer{n: 2}
	long := &countingWriteSyncer{}
	ws := NewMultiWriteSyncer(short, long)
	n, err := ws.Write([]byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want the smallest write (2)", n)
	}
}

type shortWriteSyncer struct{ n int }

func (s shortWriteSyncer) Write(p []byte) (int, error) { return s.n, nil }
func (s shortWriteSyncer) Sync() error                 { return nil }

func TestNewMultiWriteSyncerSyncErrors(t *testing.T) {
	ws := NewMultiWriteSyncer(&countingWriteSyncer{}, failingSyncer{err: errStub})
	if err := ws.Sync(); err == nil {
		t.Fatal("expected sync error to be reported")
	}
}

func TestWriterWrapperSync(t *testing.T) {
	var w writerWrapper
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync = %v, want nil", err)
	}
}

// Ensure errors is referenced by the multi-syncer tests above.
var _ = errors.Is
