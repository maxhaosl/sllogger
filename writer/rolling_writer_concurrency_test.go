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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentWrites(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const (
		goroutines = 16
		perRoutine = 200
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perRoutine; i++ {
				if _, err := w.Write([]byte("line\n")); err != nil {
					t.Errorf("write error: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// All entries must be present, no interleaved or lost bytes.
	var total int
	files := listFiles(t, cfg.Dir)
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(cfg.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		total += strings.Count(string(b), "line")
	}
	if total != goroutines*perRoutine {
		t.Fatalf("total = %d, want %d", total, goroutines*perRoutine)
	}
	if got := w.Metrics().Written.Load(); got != goroutines*perRoutine {
		t.Fatalf("written = %d", got)
	}
}

func TestConcurrentRotateSyncClose(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 64
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				w.Write([]byte(strings.Repeat("x", 40) + "\n"))
			}
		}()
	}
	// Concurrent rotation, sync and cleanup while writes are in flight.
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			w.Rotate()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			w.Sync()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			w.cleaner.Clean()
		}
	}()
	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Close twice must be a no-op.
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteAfterClose(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if err := w.Rotate(); err != ErrClosed {
		t.Fatalf("Rotate after close = %v, want ErrClosed", err)
	}
	// Sync after close must not panic.
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingDirAndBaseName(t *testing.T) {
	if _, err := NewRollingWriter(&Config{BaseName: "x"}); err == nil {
		t.Fatal("Dir is required")
	}
	if _, err := NewRollingWriter(&Config{Dir: t.TempDir()}); err == nil {
		t.Fatal("BaseName is required")
	}
}

func TestDirIsCreated(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "logs")
	w, err := NewRollingWriter(&Config{Dir: dir, BaseName: "app"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestSingleEntryLargerThanMaxSize(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 10
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Oversized entries must be written as-is instead of looping forever.
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte(strings.Repeat("y", 100))); err != nil {
			t.Fatal(err)
		}
	}
	files := listFiles(t, cfg.Dir)
	if len(files) != 3 {
		t.Fatalf("files = %v, want one per oversized entry", files)
	}
}

func TestConfigExposed(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.Config().BaseName != "LOG_CALL_INFO" {
		t.Fatalf("BaseName = %q", w.Config().BaseName)
	}
	if w.Config().RotationInterval != DefaultRotationInterval {
		t.Fatalf("RotationInterval = %v", w.Config().RotationInterval)
	}
}

func TestMaxSizeDisabled(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = -1 // negative disables size checks (0 becomes the default)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 10; i++ {
		w.Write([]byte(strings.Repeat("z", 5000)))
	}
	if got := len(listFiles(t, cfg.Dir)); got != 1 {
		t.Fatalf("files = %d, want 1 with size rotation disabled", got)
	}
}

func TestCleanupCoalescesConcurrentCalls(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.cleaner.Clean()
		}()
	}
	wg.Wait()
	// The in-flight flag must be released for the next scheduled pass.
	if w.cleaner.inFlight.Load() {
		t.Fatal("cleaner left in-flight")
	}
}

func TestCleanupIgnoresUnrelatedFiles(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	foreign := filepath.Join(cfg.Dir, "app.log")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(cfg.Dir, "README.txt")
	if err := os.WriteFile(readme, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Even an aggressive cleanup must not touch non-managed files.
	w.cleaner.cfg.MaxBackups = 1
	w.cleaner.cfg.MaxAge = time.Nanosecond
	w.cleaner.Clean()

	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign log removed: %v", err)
	}
	if _, err := os.Stat(readme); err != nil {
		t.Fatalf("non-log file removed: %v", err)
	}
}

func TestCleanupCombinedStrategies(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(cfg.Dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 4 files: two stale (by age) and two fresh.
	write("LOG_CALL_INFO.2026-08-01.playurl8080.log", strings.Repeat("a", 50))
	write("LOG_CALL_INFO.2026-08-02.playurl8080.log", strings.Repeat("b", 50))
	write("LOG_CALL_INFO.2026-09-03.playurl8080.log", strings.Repeat("c", 50))
	write("LOG_CALL_INFO.2026-09-03.playurl8080.01.log", strings.Repeat("d", 50))

	stamp := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	os.Chtimes(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-08-01.playurl8080.log"), stamp, stamp)
	stamp2 := time.Date(2026, 8, 2, 0, 0, 0, 0, time.Local)
	os.Chtimes(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-08-02.playurl8080.log"), stamp2, stamp2)

	// MaxAge removes the stale ones, then MaxTotalSize keeps the rest under
	// 120 bytes (i.e. at most two files).
	w.cleaner.cfg.MaxAge = 24 * time.Hour
	w.cleaner.cfg.MaxTotalSize = 120
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	for _, f := range files {
		if strings.Contains(f, "2026-08") {
			t.Fatalf("stale file survived: %v", files)
		}
	}
	if len(files) > 3 {
		t.Fatalf("files = %v, size quota not enforced", files)
	}
	if got := w.Metrics().RemovedFiles.Load(); got < 2 {
		t.Fatalf("removed = %d, want at least 2", got)
	}
}

func TestMetricsSnapshot(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write([]byte("a"))
	w.Write([]byte("b"))
	snap := w.Metrics().Snapshot()
	if snap.Written != 2 {
		t.Fatalf("written = %d", snap.Written)
	}
	if snap.RotateCount == 0 {
		t.Fatal("rotate count should account for the initial open")
	}
}

func TestResumePicksLatestSeq(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 1000
	cfg.RotationInterval = 0

	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-03.playurl8080.log",
		"LOG_CALL_INFO.2026-09-03.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-03.playurl8080.05.log",
	} {
		if err := os.WriteFile(filepath.Join(cfg.Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if got := filepath.Base(w.path); got != "LOG_CALL_INFO.2026-09-03.playurl8080.05.log" {
		t.Fatalf("resumed file = %q", got)
	}
}

func TestCleanupRunsOnClose(t *testing.T) {
	cfg, _ := testConfig(t)
	stale := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2020-01-01.playurl8080.log")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	os.Chtimes(stale, old, old)

	cfg.MaxAge = time.Hour
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale file should be removed on Close")
	}
}
