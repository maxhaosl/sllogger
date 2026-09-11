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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHourlyFileNameRotation verifies that DateLayout "2006-01-02.15"
// (yyyy-MM-dd.hh) yields exactly one file per hour, named
// LOG_FEIGN.{date}.{service}.log with the hour embedded in the date segment
// and NO rotation sequence suffix.
func TestHourlyFileNameRotation(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 9, 12, 0, 0, 0, 0, time.Local)
	mc := newMockClock(start)
	cfg := &Config{
		Dir:         dir,
		BaseName:    "LOG_FEIGN",
		DateLayout:  "2006-01-02.15", // yyyy-MM-dd.hh
		ServiceName: "feignsvc",
		ServicePort: 9090,
		Clock:       mc,
	}
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for hour := 0; hour < 24; hour++ {
		mc.Set(start.Add(time.Duration(hour) * time.Hour))
		if _, err := w.Write([]byte("feign\n")); err != nil {
			t.Fatal(err)
		}
	}

	// The currently open file must be the 23:00 file of the same day.
	if got := filepath.Base(w.path); got != "LOG_FEIGN.2026-09-12.23.feignsvc9090.log" {
		t.Fatalf("current file after 24h: got %q", got)
	}

	files := listFiles(t, dir)
	if len(files) != 24 {
		t.Fatalf("expected 24 hourly files (00..23), got %d: %v", len(files), files)
	}
	for hour := 0; hour < 24; hour++ {
		want := "LOG_FEIGN.2026-09-12." + fmt.Sprintf("%02d", hour) + ".feignsvc9090.log"
		if !containsFile(files, want) {
			t.Fatalf("missing hourly file %q; have %v", want, files)
		}
	}
}

// TestHourlyMaxAgeCleanup verifies MaxAge=72h retains the last ~3 days of hourly
// LOG_FEIGN files, deletes the oldest ones, and leaves a foreign log type in the
// same directory untouched (per-type cleanup isolation).
func TestHourlyMaxAgeCleanup(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	mc := newMockClock(start)
	cfg := &Config{
		Dir:         dir,
		BaseName:    "LOG_FEIGN",
		DateLayout:  "2006-01-02.15", // yyyy-MM-dd.hh
		ServiceName: "feignsvc",
		ServicePort: 9090,
		MaxAge:      72 * time.Hour, // keep 3 days
		Clock:       mc,
	}
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write 80 hourly files through the writer (2026-09-10 00:00 .. 2026-09-13 07:00).
	const total = 80
	for h := 0; h < total; h++ {
		sim := start.Add(time.Duration(h) * time.Hour)
		mc.Set(sim)
		if _, err := w.Write([]byte("feign\n")); err != nil {
			t.Fatal(err)
		}
		// Align the on-disk mtime with the simulated hour so MaxAge can be
		// exercised without waiting 3 real days.
		if err := os.Chtimes(w.path, sim, sim); err != nil {
			t.Fatal(err)
		}
	}

	// A foreign-type file in the same directory must survive LOG_FEIGN cleanup.
	foreign := filepath.Join(dir, "LOG_MGMONITOR.2026-09-10-00.mgmonitor.log")
	if err := os.WriteFile(foreign, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(foreign, start, start)

	// The clock is already at the last simulated hour; run a cleanup pass.
	w.cleaner.Clean()

	// deadline = now - 72h = 2026-09-10 07:00. Hours 00..06 (7 files) are older
	// and must be removed; hours 07..79 (73 files) are kept.
	for h := 0; h <= 6; h++ {
		name := "LOG_FEIGN." + start.Add(time.Duration(h)*time.Hour).Format("2006-01-02.15") + ".feignsvc9090.log"
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("old file %s should have been cleaned", name)
		}
	}
	for h := 7; h < total; h++ {
		name := "LOG_FEIGN." + start.Add(time.Duration(h)*time.Hour).Format("2006-01-02.15") + ".feignsvc9090.log"
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("recent file %s should be kept: %v", name, err)
		}
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign log file must not be removed: %v", err)
	}
	if got := w.Metrics().RemovedFiles.Load(); got != 7 {
		t.Fatalf("expected 7 removed hourly files, got %d", got)
	}
}

func containsFile(files []string, name string) bool {
	for _, f := range files {
		if f == name {
			return true
		}
	}
	return false
}
