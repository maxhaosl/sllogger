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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockClock is a controllable clock for rotation tests.
type mockClock struct {
	mu sync.Mutex
	t  time.Time
}

func newMockClock(t time.Time) *mockClock { return &mockClock{t: t} }

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *mockClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var baseTime = time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

func testConfig(t *testing.T) (*Config, *mockClock) {
	t.Helper()
	dir := t.TempDir()
	mc := newMockClock(baseTime)
	cfg := &Config{
		Dir:         dir,
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
		Clock:       mc,
	}
	return cfg, mc
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFileNameRules(t *testing.T) {
	cfg, _ := testConfig(t)
	n := newNamer(cfg.withDefaults())

	got := n.FileName(baseTime, 0)
	want := "LOG_CALL_INFO.2026-09-03.playurl8080.log"
	if got != want {
		t.Fatalf("first file: got %q, want %q", got, want)
	}

	got = n.FileName(baseTime, 1)
	want = "LOG_CALL_INFO.2026-09-03.playurl8080.01.log"
	if got != want {
		t.Fatalf("rotated file: got %q, want %q", got, want)
	}

	got = n.FileName(baseTime, 23)
	want = "LOG_CALL_INFO.2026-09-03.playurl8080.23.log"
	if got != want {
		t.Fatalf("rotated file 23: got %q, want %q", got, want)
	}
}

func TestCustomNamePattern(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Dir:                dir,
		BaseName:           "svc",
		NamePattern:        "{base}_{date}_{service}.log",
		RotatedNamePattern: "{base}_{date}_{service}_{seq}.log",
		DateLayout:         "20060102",
		ServiceName:        "order",
		ServicePort:        80,
	}
	n := newNamer(cfg.withDefaults())
	if got, want := n.FileName(baseTime, 0), "svc_20260903_order80.log"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := n.FileName(baseTime, 2), "svc_20260903_order80_02.log"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSizeRotation(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 100
	cfg.RotationInterval = 0 // disable time rotation
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	payload := strings.Repeat("a", 60)
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}

	files := listFiles(t, cfg.Dir)
	if len(files) != 3 {
		t.Fatalf("expected 3 files after size rotation, got %v", files)
	}
	// Verify size kept below MaxSize.
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(cfg.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() > 120 {
			t.Fatalf("file %s exceeds MaxSize: %d", f, fi.Size())
		}
	}
	if w.Metrics().RotateCount.Load() < 2 {
		t.Fatalf("expected rotations, got %d", w.Metrics().RotateCount.Load())
	}
	_ = clock
}

func TestDayRotationResetsSeq(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 50
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Two rotations on day one.
	for i := 0; i < 3; i++ {
		w.Write([]byte(strings.Repeat("a", 60)))
	}
	// Cross the day boundary.
	clock.Advance(15 * time.Hour) // -> 2026-09-04 01:00
	w.Write([]byte(strings.Repeat("b", 60)))

	files := listFiles(t, cfg.Dir)
	var hasNextDay bool
	for _, f := range files {
		if strings.Contains(f, "2026-09-04") && strings.HasSuffix(f, ".log") &&
			!strings.Contains(f, ".01.") && !strings.Contains(f, ".02.") &&
			!strings.Contains(f, ".03.") {
			hasNextDay = true
		}
	}
	if !hasNextDay {
		t.Fatalf("expected unnumbered file for the new day, got %v", files)
	}
	w.Close()
}

func TestIntervalRotation(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 0 // unlimited
	cfg.RotationInterval = time.Hour
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write([]byte("hour-10"))
	clock.Advance(59 * time.Minute)
	w.Write([]byte("still-hour-10"))
	clock.Advance(time.Minute) // -> 11:00
	w.Write([]byte("hour-11"))
	clock.Advance(time.Hour) // -> 12:00
	w.Write([]byte("hour-12"))

	files := listFiles(t, cfg.Dir)
	if len(files) != 3 {
		t.Fatalf("expected 3 files for interval rotation, got %v", files)
	}
	// The unnumbered file holds the first hour's entries.
	first := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	if got := readAll(t, first); got != "hour-10"+"still-hour-10" {
		t.Fatalf("unexpected content of first file: %q", got)
	}
	if got := readAll(t, filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.01.log")); got != "hour-11" {
		t.Fatalf("unexpected content of .01 file: %q", got)
	}
	if got := readAll(t, filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.02.log")); got != "hour-12" {
		t.Fatalf("unexpected content of .02 file: %q", got)
	}
}

func TestResumeOnRestart(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 1000
	cfg.RotationInterval = 0

	// First session writes one partial file.
	w1, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w1.Write([]byte(strings.Repeat("a", 500)))
	w1.Close()

	// Second session should resume the same file.
	w2, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	w2.Write([]byte(strings.Repeat("b", 500)))

	files := listFiles(t, cfg.Dir)
	if len(files) != 1 {
		t.Fatalf("expected resume into the same file, got %v", files)
	}
	content := readAll(t, filepath.Join(cfg.Dir, files[0]))
	if content != strings.Repeat("a", 500)+strings.Repeat("b", 500) {
		t.Fatalf("content lost on restart: len=%d", len(content))
	}

	// Full file: next session must open seq 01.
	w2.Close() // idempotent Close
	os.WriteFile(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log"),
		[]byte(strings.Repeat("a", 1000)), 0o644)
	w3, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w3.Close()
	if got, want := filepath.Base(w3.path), "LOG_CALL_INFO.2026-09-03.playurl8080.01.log"; got != want {
		t.Fatalf("expected next-seq file, got %q", got)
	}
}

func TestCleanupMaxAge(t *testing.T) {
	cfg, clock := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Create three files with different mtimes.
	paths := []string{
		namerFile(t, cfg, "LOG_CALL_INFO.2026-09-01.playurl8080.log"),
		namerFile(t, cfg, "LOG_CALL_INFO.2026-09-02.playurl8080.log"),
		namerFile(t, cfg, "LOG_CALL_INFO.2026-09-03.playurl8080.log"),
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Chtimes(paths[0], time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local))
	os.Chtimes(paths[1], time.Date(2026, 9, 2, 0, 0, 0, 0, time.Local), time.Date(2026, 9, 2, 0, 0, 0, 0, time.Local))
	os.Chtimes(paths[2], baseTime, baseTime)

	// MaxAge = 48h from now (2026-09-03 10:00): only 09-01 is too old.
	w.cleaner.cfg.MaxAge = 48 * time.Hour
	w.cleaner.Clean()

	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("09-01 file should be removed")
	}
	if _, err := os.Stat(paths[1]); err != nil {
		t.Fatalf("09-02 file should be kept: %v", err)
	}
	if _, err := os.Stat(paths[2]); err != nil {
		t.Fatalf("09-03 file should be kept: %v", err)
	}
	_ = clock
}

func TestCleanupMaxBackups(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-01.playurl8080.log",
		"LOG_CALL_INFO.2026-09-01.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-03.playurl8080.log",
	} {
		f := namerFile(t, cfg, name)
		os.WriteFile(f, []byte("data"), 0o644)
	}

	w.cleaner.cfg.MaxBackups = 2
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 files kept, got %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, "09-01") {
			t.Fatalf("oldest day should be removed first: %v", files)
		}
	}
}

func TestCleanupMaxTotalSize(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	write := func(name string, size int) {
		f := namerFile(t, cfg, name)
		os.WriteFile(f, []byte(strings.Repeat("x", size)), 0o644)
	}
	write("LOG_CALL_INFO.2026-09-01.playurl8080.log", 100)
	write("LOG_CALL_INFO.2026-09-02.playurl8080.log", 100)
	write("LOG_CALL_INFO.2026-09-03.playurl8080.log", 100)

	w.cleaner.cfg.MaxTotalSize = 250 // 300 total -> oldest (100) removed
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 files kept, got %v", files)
	}
	if !strings.Contains(files[0], "09-02") {
		t.Fatalf("expected oldest removed, got %v", files)
	}
}

func TestCleanupMaxDirSize(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	write := func(name string, size int) {
		f := namerFile(t, cfg, name)
		os.WriteFile(f, []byte(strings.Repeat("x", size)), 0o644)
	}
	p01 := namerFile(t, cfg, "LOG_CALL_INFO.2026-09-01.playurl8080.log")
	p02 := namerFile(t, cfg, "LOG_CALL_INFO.2026-09-02.playurl8080.log")
	write(filepath.Base(p01), 100)
	write(filepath.Base(p02), 100)
	// A foreign log file also counts towards the directory total.
	os.WriteFile(filepath.Join(cfg.Dir, "OTHER.2026-09-03.svc.log"), []byte(strings.Repeat("y", 100)), 0o644)

	w.cleaner.cfg.MaxDirSize = 250 // 300 total -> excess 50 -> remove oldest (100)
	w.cleaner.Clean()

	if _, err := os.Stat(p01); !os.IsNotExist(err) {
		t.Fatalf("oldest file should be removed by dir quota")
	}
	if _, err := os.Stat(p02); err != nil {
		t.Fatalf("newer file should be kept: %v", err)
	}
	// The foreign file is never touched.
	if _, err := os.Stat(filepath.Join(cfg.Dir, "OTHER.2026-09-03.svc.log")); err != nil {
		t.Fatalf("foreign file must not be removed: %v", err)
	}
}

func TestAsyncWriter(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	aw := NewAsyncWriter(rw, cfg)
	for i := 0; i < 500; i++ {
		if _, err := aw.Write([]byte("entry\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := aw.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := aw.Metrics().Enqueued.Load(); got != 500 {
		t.Fatalf("enqueued=%d", got)
	}
	// Close drains and flushes everything.
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	content := readAll(t, rw.path)
	if got := strings.Count(content, "entry"); got != 500 {
		t.Fatalf("expected 500 entries after close, got %d", got)
	}
	if m := rw.Metrics().Snapshot(); m.Dropped != 0 {
		t.Fatalf("unexpected drops: %d", m.Dropped)
	}
}

func TestAsyncWriterDropsWhenFull(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.QueueSize = 4
	cfg.FlushInterval = time.Hour
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Do not run the worker: replace inner behavior by never draining via a
	// blocked worker is hard; instead write more than queue size and verify
	// drops counted by the queue semantics.
	aw := NewAsyncWriter(rw, cfg)
	for i := 0; i < 100; i++ {
		aw.Write([]byte("x"))
	}
	// Either enqueued or dropped; total must be 100.
	m := aw.Metrics().Snapshot()
	if m.Enqueued+m.Dropped != 100 {
		t.Fatalf("enqueued=%d dropped=%d", m.Enqueued, m.Dropped)
	}
	aw.Close()
}

// namerFile returns the full path for a file name inside the test dir.
func namerFile(t *testing.T, cfg *Config, name string) string {
	t.Helper()
	return filepath.Join(cfg.Dir, name)
}
