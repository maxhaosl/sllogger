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

package sllogger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxhaosl/sllogger/writer"
)

// countLogFileLines counts non-empty lines across log files whose name starts
// with base and asserts each is valid JSON.
func countLogFileLines(t *testing.T, dir, base string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || !bytes.HasPrefix([]byte(e.Name()), []byte(base)) {
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
				t.Fatalf("invalid JSON in %s: %v\nline=%s", e.Name(), err, line)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return total
}

// TestLoggerNoLossToFile drives the full pipeline (Logger -> JSON encoder ->
// async RollingWriter) and asserts every logged entry is present on disk
// exactly once. This is the end-to-end "is anything lost?" check.
func TestLoggerNoLossToFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Level:       NewAtomicLevelAt(DebugLevel),
		Encoding:    "json",
		Development: false,
		Rolling: &writer.Config{
			Dir:       dir,
			BaseName:  "e2e",
			MaxSize:   1 << 20,
			Async:     true,
			QueueSize: 500_000,
		},
	}
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	const N = 100_000
	for i := 0; i < N; i++ {
		log.Info("load-test", Int("i", i))
	}
	// Flush + rotate-close.
	if err := log.Sync(); err != nil {
		t.Logf("sync: %v", err)
	}
	log.With(String("flush", "1")).Info("flush-marker")
	if err := log.Sync(); err != nil {
		t.Logf("sync2: %v", err)
	}

	got := countLogFileLines(t, dir, "e2e")
	// The flush-marker adds one extra entry; account for it.
	if got != N+1 {
		t.Fatalf("wrote %d entries (+1 marker) but log files contain %d lines (lost %d)", N, got, N+1-got)
	}
}

// BenchmarkLoggerToFileNoLoss is a pressure test that also verifies no silent
// loss: it writes one million entries to a rolling file via the logger and
// checks the on-disk line count equals the number written.
func BenchmarkLoggerToFileNoLoss(b *testing.B) {
	dir := b.TempDir()
	cfg := Config{
		Level:    NewAtomicLevelAt(DebugLevel),
		Encoding: "json",
		Rolling: &writer.Config{
			Dir:       dir,
			BaseName:  "benchloss",
			MaxSize:   1 << 20,
			Async:     true,
			QueueSize: 2_000_000,
		},
	}
	log, err := cfg.Build()
	if err != nil {
		b.Fatal(err)
	}

	const N = 1_000_000
	b.ResetTimer()
	for i := 0; i < N; i++ {
		log.Info("bench", Int("i", i))
	}
	if err := log.Sync(); err != nil {
		b.Logf("sync: %v", err)
	}
	b.StopTimer()

	lines := countLogFileLinesBenchmark(b, dir, "benchloss")
	if lines != N {
		b.Fatalf("LOSS: wrote %d entries but on-disk lines=%d (lost %d)", N, lines, N-lines)
	}
	b.Logf("wrote %d entries in %v (%.0f entries/sec); on-disk lines=%d",
		N, b.Elapsed(), float64(N)/b.Elapsed().Seconds(), lines)
}

// countRawLogFileLines counts non-empty lines across log files whose name starts
// with base WITHOUT validating JSON. Unlike countLogFileLines it is encoding
// agnostic, so it works for the CALL_INFO template path (^ separated, not JSON).
// Used for loss checks where we only need to confirm every emitted entry produced
// exactly one on-disk line.
func countRawLogFileLines(t *testing.T, dir, base string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || !bytes.HasPrefix([]byte(e.Name()), []byte(base)) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			if sc.Text() != "" {
				total++
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return total
}

// TestCallInfoNoLossToFile drives the full pipeline (Logger -> CALL_INFO template
// encoder -> async RollingWriter) and asserts every CallInfo entry is present on
// disk exactly once. Mirrors TestLoggerNoLossToFile but exercises the library's
// primary CALL_INFO path, where the on-disk line is NOT JSON and must be counted
// without format validation.
func TestCallInfoNoLossToFile(t *testing.T) {
	dir := t.TempDir()
	cfg := NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 1 << 20
	cfg.Rolling.Async = true
	cfg.Rolling.QueueSize = 500_000
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithTrace(context.Background(), "tid", "sid")

	const N = 100_000
	for i := 0; i < N; i++ {
		log.CallInfo(ctx, CallInfo{
			Mobile:   "18237438309",
			UserID:   "1071748417",
			ClientID: "c6559e74a24df8d97a2296ff1e23387a",
			URL:      "http://play.example.com:443/playurl/v1/play/playurl",
			Method:   "GET",
			UseTime:  int64(i),
			ServerIP: "127.0.0.1",
			BussID:   "null",
			LogMsg:   "^MG.getContent:[690894368]",
		})
	}
	if err := log.Sync(); err != nil {
		t.Logf("sync: %v", err)
	}
	log.With(String("flush", "1")).Info("flush-marker")
	if err := log.Sync(); err != nil {
		t.Logf("sync2: %v", err)
	}

	got := countRawLogFileLines(t, dir, "LOG_CALL_INFO")
	// The flush-marker adds one extra entry.
	if got != N+1 {
		t.Fatalf("wrote %d CallInfo entries (+1 marker) but log files contain %d lines (lost %d)", N, got, N+1-got)
	}
}

// BenchmarkCallInfoToFileNoLoss is a pressure test for the CALL_INFO path that
// also verifies no silent loss: it writes one million CallInfo entries to a
// rolling file and checks the on-disk line count equals the number written.
func BenchmarkCallInfoToFileNoLoss(b *testing.B) {
	dir := b.TempDir()
	cfg := NewCallInfoConfig(dir, "benchci", 8080)
	cfg.Rolling.BaseName = "benchci"
	cfg.Rolling.MaxSize = 1 << 20
	cfg.Rolling.Async = true
	cfg.Rolling.QueueSize = 2_000_000
	log, err := cfg.Build()
	if err != nil {
		b.Fatal(err)
	}
	ctx := WithTrace(context.Background(), "tid", "sid")

	const N = 1_000_000
	b.ResetTimer()
	for i := 0; i < N; i++ {
		log.CallInfo(ctx, CallInfo{
			Mobile:   "18237438309",
			UserID:   "1071748417",
			ClientID: "c6559e74a24df8d97a2296ff1e23387a",
			URL:      "http://play.example.com:443/playurl/v1/play/playurl",
			Method:   "GET",
			UseTime:  int64(i),
			ServerIP: "127.0.0.1",
			BussID:   "null",
			LogMsg:   "^MG.getContent:[690894368]",
		})
	}
	if err := log.Sync(); err != nil {
		b.Logf("sync: %v", err)
	}
	b.StopTimer()

	lines := countLogFileLinesBenchmark(b, dir, "benchci")
	if lines != N {
		b.Fatalf("LOSS: wrote %d CallInfo entries but on-disk lines=%d (lost %d)", N, lines, N-lines)
	}
	b.Logf("wrote %d CallInfo entries in %v (%.0f entries/sec); on-disk lines=%d",
		N, b.Elapsed(), float64(N)/b.Elapsed().Seconds(), lines)
}

func countLogFileLinesBenchmark(b *testing.B, dir, base string) int {
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
