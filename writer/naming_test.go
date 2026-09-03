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
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSeqName(t *testing.T) {
	tests := map[int]string{
		0:   "00",
		1:   "01",
		9:   "09",
		10:  "10",
		99:  "99",
		100: "100",
	}
	for seq, want := range tests {
		if got := seqName(seq); got != want {
			t.Errorf("seqName(%d) = %q, want %q", seq, got, want)
		}
	}
}

func TestServiceRendering(t *testing.T) {
	tests := []struct {
		name string
		port int
		want string
	}{
		{"playurl", 8080, "playurl8080"},
		{"playurl", 0, "playurl0"},
		{"svc", -1, "svc-1"},
	}
	for _, tt := range tests {
		cfg := &Config{ServiceName: tt.name, ServicePort: tt.port}
		if got := cfg.service(); got != tt.want {
			t.Errorf("service(%s,%d) = %q, want %q", tt.name, tt.port, got, tt.want)
		}
	}
}

func TestItoa(t *testing.T) {
	for i := -100; i <= 100; i++ {
		if got, want := itoa(i), strconv.Itoa(i); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestRenderPlaceholders(t *testing.T) {
	cfg := (&Config{
		BaseName:    "LOG",
		ServiceName: "svc",
		ServicePort: 80,
		DateLayout:  "2006-01-02",
	}).withDefaults()
	n := newNamer(cfg)

	got := n.render("{base}.{date}.{service}.{seq}.{pid}.log", baseTime, 3)
	want := "LOG.2026-09-03.svc80.03." + strconv.Itoa(os.Getpid()) + ".log"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderUnknownPlaceholderPreserved(t *testing.T) {
	n := newNamer((&Config{BaseName: "LOG"}).withDefaults())
	got := n.render("{unknown}.log", baseTime, 0)
	if got != "{unknown}.log" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanupName(t *testing.T) {
	tests := map[string]string{
		"a..b":        "a.b",
		"LOG..log":    "LOG.log",
		".LOG.log":    "LOG.log",
		"LOG.log.":    "LOG.log",
		"":            "sllogger.log",
		"...":         "sllogger.log",
		"a...b":       "a.b",
		"normal.name": "normal.name",
	}
	for in, want := range tests {
		if got := cleanupName(in); got != want {
			t.Errorf("cleanupName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmptyServiceName(t *testing.T) {
	// An empty service must not produce ".." in the file name.
	cfg := (&Config{BaseName: "LOG", ServiceName: ""}).withDefaults()
	n := newNamer(cfg)
	got := n.FileName(baseTime, 0)
	if got != "LOG.2026-09-03.log" {
		t.Fatalf("got %q, want LOG.2026-09-03.log", got)
	}
}

func TestParseFileName(t *testing.T) {
	cfg := (&Config{
		Dir:         "/data/logs",
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
	}).withDefaults()
	n := newNamer(cfg)

	tests := []struct {
		name    string
		wantSeq int
		wantOK  bool
	}{
		{"LOG_CALL_INFO.2026-09-03.playurl8080.log", 0, true},
		{"LOG_CALL_INFO.2026-09-03.playurl8080.01.log", 1, true},
		{"LOG_CALL_INFO.2026-09-03.playurl8080.99.log", 99, true},
		{"LOG_CALL_INFO.2026-09-03.playurl8080.log", 0, true},
		// Rejected names.
		{"OTHER.2026-09-03.playurl8080.log", 0, false},
		{"LOG_CALL_INFO.2026-09-03.othersvc.log", 0, false},
		{"LOG_CALL_INFO.not-a-date.playurl8080.log", 0, false},
		{"LOG_CALL_INFO.2026-09-03.playurl8080.txt", 0, false},
		{"LOG_CALL_INFO.playurl8080.log", 0, false},
		{"not-a-log-file.log", 0, false},
	}
	for _, tt := range tests {
		pf, ok := n.parseFileName(tt.name, nil)
		if ok != tt.wantOK {
			t.Errorf("parseFileName(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if pf.seq != tt.wantSeq {
			t.Errorf("parseFileName(%q).seq = %d, want %d", tt.name, pf.seq, tt.wantSeq)
		}
		wantDate := time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local)
		if !pf.date.Equal(wantDate) {
			t.Errorf("parseFileName(%q).date = %v", tt.name, pf.date)
		}
		if pf.path != filepath.Join("/data/logs", tt.name) {
			t.Errorf("parseFileName(%q).path = %q", tt.name, pf.path)
		}
	}
}

func TestLayoutGranularity(t *testing.T) {
	tests := map[string]time.Duration{
		"2006-01-02":                 24 * time.Hour,
		"20060102":                   24 * time.Hour,
		"2006-01-02-15":              time.Hour,
		"2006-01-02T15":              time.Hour,
		"2006-01-02T15-04":           time.Minute,
		"2006-01-02T15:04":           time.Minute,
		"2006-01-02T15:04:05":        time.Second,
		"2006-01-02T15:04:05.000Z07": time.Second,
	}
	for layout, want := range tests {
		if got := layoutGranularity(layout); got != want {
			t.Errorf("layoutGranularity(%q) = %v, want %v", layout, got, want)
		}
	}
}

func TestStampOfCaching(t *testing.T) {
	cfg := (&Config{Dir: "/tmp", BaseName: "LOG", ServiceName: "s", ServicePort: 1}).withDefaults()
	n := newNamer(cfg)

	// Within the same day the stamp must be identical and served from cache.
	first := n.stampOf(baseTime)
	second := n.stampOf(baseTime.Add(3 * time.Hour))
	if !first.Equal(second) {
		t.Fatalf("stamp changed within a day: %v vs %v", first, second)
	}
	if !n.cacheValid {
		t.Fatal("cache not populated")
	}
	if want := first.Add(24 * time.Hour); !n.cacheEnd.Equal(want) {
		t.Fatalf("cacheEnd = %v, want %v", n.cacheEnd, want)
	}

	// Crossing midnight produces a new stamp.
	next := n.stampOf(baseTime.Add(24 * time.Hour))
	if next.Equal(first) {
		t.Fatal("stamp did not advance at midnight")
	}
}

func TestStampOfCustomLayout(t *testing.T) {
	cfg := (&Config{
		Dir:         "/tmp",
		BaseName:    "LOG",
		ServiceName: "s",
		ServicePort: 1,
		DateLayout:  "20060102",
	}).withDefaults()
	n := newNamer(cfg)
	if n.fastDay {
		t.Fatal("custom layout must not use the fast day path")
	}
	got := n.stampOf(baseTime)
	want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("stamp = %v, want %v", got, want)
	}
	if got := n.FileName(baseTime, 0); got != "LOG.20260903.s1.log" {
		t.Fatalf("file name = %q", got)
	}
}

func TestStampOfConcurrent(t *testing.T) {
	cfg := (&Config{Dir: "/tmp", BaseName: "LOG", ServiceName: "s", ServicePort: 1}).withDefaults()
	n := newNamer(cfg)

	want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ { // baseTime is 10:00, so stay inside the same day
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got := n.stampOf(baseTime.Add(time.Duration(i) * time.Hour))
			if !got.Equal(want) {
				t.Errorf("stampOf(+%dh) = %v, want %v", i, got, want)
			}
		}(i)
	}
	wg.Wait()
}

func TestFilePath(t *testing.T) {
	cfg := (&Config{
		Dir:         "/data/logs",
		BaseName:    "LOG",
		ServiceName: "svc",
		ServicePort: 80,
	}).withDefaults()
	n := newNamer(cfg)
	want := filepath.Join("/data/logs", "LOG.2026-09-03.svc80.log")
	if got := n.FilePath(baseTime, 0); got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
}
