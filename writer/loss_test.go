// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to anyone obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and/or sell copies of the Software, and to permit persons to
// whom the Software is distributed under the terms of this license, subject to
// the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package writer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/buffer"
	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
)

// countLinesInDir counts non-empty lines across all files whose name starts
// with base, and verifies each line is valid JSON. It returns the line count.
func countLinesInDir(t *testing.T, dir, base string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !bytes.HasPrefix([]byte(e.Name()), []byte(base)) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		var scratch map[string]interface{}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				continue
			}
			total++
			if err := json.Unmarshal([]byte(line), &scratch); err != nil {
				t.Fatalf("log file %s contains invalid JSON: %v\nline=%s", e.Name(), err, line)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return total
}

// encodeLine renders one JSON log entry to a buffer (caller must Free it).
func encodeLine(t *testing.T, enc *encoder.JSONEncoder, i int) *buffer.Buffer {
	t.Helper()
	ent := slcore.Entry{Level: slcore.InfoLevel, Message: "load-test", Time: time.Now()}
	// Build a single field as a literal (slcore has no exported Int constructor).
	f := slcore.Field{Key: "i", Type: slcore.Int64Type, Integer: int64(i)}
	buf, err := enc.EncodeEntry(ent, []slcore.Field{f})
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

// TestRollingWriterNoLossSync writes a large number of entries through a
// synchronous RollingWriter (forcing many rotations via a tiny MaxSize) and
// asserts every entry lands in the log files exactly once, with zero errors.
func TestRollingWriterNoLossSync(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, BaseName: "loss", MaxSize: 1 << 16, RotationInterval: 0}
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enc := encoder.NewJSONEncoder(encoder.DefaultJSONEncoderConfig())

	const N = 100_000
	for i := 0; i < N; i++ {
		buf := encodeLine(t, enc, i)
		if _, err := rw.Write(buf.Bytes()); err != nil {
			buf.Free()
			t.Fatalf("write %d: %v", i, err)
		}
		buf.Free()
	}
	if err := rw.Close(); err != nil {
		t.Fatal(err)
	}

	got := countLinesInDir(t, dir, "loss")
	if got != N {
		t.Fatalf("wrote %d entries but log files contain %d lines (lost %d)", N, got, N-got)
	}
	if we := rw.Metrics().WriteErrors.Load(); we != 0 {
		t.Fatalf("unexpected write errors: %d", we)
	}
}

// TestRollingWriterNoLossAsync writes a large number of entries through an
// AsyncWriter (with a generous queue) from multiple goroutines, then asserts
// that every entry is accounted for: lines on disk + dropped == N, and no drops
// occurred because the worker kept up.
func TestRollingWriterNoLossAsync(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Dir:              dir,
		BaseName:         "lossa",
		MaxSize:          1 << 20,
		RotationInterval: 0,
		Async:            true,
		QueueSize:        500_000,
		BlockOnFull:      false,
	}
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	enc := encoder.NewJSONEncoder(encoder.DefaultJSONEncoderConfig())

	const N = 100_000
	const procs = 8
	perProc := N / procs
	var wg sync.WaitGroup
	for p := 0; p < procs; p++ {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			for i := 0; i < perProc; i++ {
				buf := encodeLine(t, enc, off*perProc+i)
				_, _ = aw.Write(buf.Bytes())
				buf.Free()
			}
		}(p)
	}
	wg.Wait()
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	dropped := aw.Metrics().Dropped.Load()
	got := countLinesInDir(t, dir, "lossa")
	if got+int(dropped) != N {
		t.Fatalf("entries not accounted for: lines=%d dropped=%d total=%d want %d", got, dropped, got+int(dropped), N)
	}
	if dropped != 0 {
		t.Fatalf("async dropped %d entries under non-overload conditions", dropped)
	}
}

// BenchmarkRollingWriterNoLoss is both a pressure test and a loss check: it
// writes one million entries through the async writer and verifies the on-disk
// line count equals the number written (no silent loss, no corruption).
func BenchmarkRollingWriterNoLoss(b *testing.B) {
	dir := b.TempDir()
	cfg := &Config{Dir: dir, BaseName: "benchloss", MaxSize: 1 << 20, Async: true, QueueSize: 2_000_000}
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		b.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	enc := encoder.NewJSONEncoder(encoder.DefaultJSONEncoderConfig())

	const N = 1_000_000
	b.ResetTimer()
	var written int64
	for i := 0; i < N; i++ {
		ent := slcore.Entry{Level: slcore.InfoLevel, Message: "bench", Time: time.Now()}
		f := slcore.Field{Key: "i", Type: slcore.Int64Type, Integer: int64(i)}
		buf, err := enc.EncodeEntry(ent, []slcore.Field{f})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := aw.Write(buf.Bytes()); err != nil {
			buf.Free()
			b.Fatal(err)
		}
		buf.Free()
		atomic.AddInt64(&written, 1)
	}
	if err := aw.Close(); err != nil {
		b.Fatal(err)
	}
	b.StopTimer()

	dropped := aw.Metrics().Dropped.Load()
	lines := countLinesInDirBenchmark(b, dir, "benchloss")
	if lines+int(dropped) != N {
		b.Fatalf("LOSS: lines=%d dropped=%d total=%d want %d", lines, dropped, lines+int(dropped), N)
	}
	if dropped != 0 {
		b.Fatalf("LOSS: async dropped %d entries", dropped)
	}
	b.Logf("wrote %d entries in %v (%.0f entries/sec); on-disk lines=%d dropped=%d",
		N, b.Elapsed(), float64(N)/b.Elapsed().Seconds(), lines, dropped)
}

func countLinesInDirBenchmark(b *testing.B, dir, base string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || !bytes.HasPrefix([]byte(e.Name()), []byte(base)) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			b.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			if sc.Text() != "" {
				total++
			}
		}
		f.Close()
	}
	return total
}
