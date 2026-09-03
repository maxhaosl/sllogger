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
	"testing"
	"time"
)

const benchLine = "2026-09-03 10:00:00.000|INFO|playurl|tid|sid|18237438309|1071748417|cid|http://svc/api|GET|123|127.0.0.1|null|^MG.getContent:[1]\n"

func benchWriter(b *testing.B, cfg *Config) *RollingWriter {
	b.Helper()
	w, err := NewRollingWriter(cfg)
	if err != nil {
		b.Fatal(err)
	}
	return w
}

func BenchmarkRollingWriterSync(b *testing.B) {
	cfg := &Config{
		Dir:              b.TempDir(),
		BaseName:         "bench",
		MaxSize:          0,
		RotationInterval: 0,
	}
	w := benchWriter(b, cfg)
	defer w.Close()
	line := []byte(benchLine)

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(line); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if err := w.Sync(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkRollingWriterWithSizeCheck(b *testing.B) {
	cfg := &Config{
		Dir:              b.TempDir(),
		BaseName:         "bench",
		MaxSize:          1 << 20, // 1 MiB: exercises the size-rotation check
		RotationInterval: time.Hour,
	}
	w := benchWriter(b, cfg)
	defer w.Close()
	line := []byte(benchLine)

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(line); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkAsyncWriterEnqueue(b *testing.B) {
	dir := b.TempDir()
	cfg := &Config{
		Dir:              dir,
		BaseName:         "bench",
		MaxSize:          0,
		RotationInterval: 0,
		Async:            true,
		QueueSize:        1000000,
		BlockOnFull:      true,
	}
	rw := benchWriter(b, cfg)
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()
	line := []byte(benchLine)

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := aw.Write(line); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkAsyncWriterConcurrent(b *testing.B) {
	dir := b.TempDir()
	cfg := &Config{
		Dir:              dir,
		BaseName:         "bench",
		MaxSize:          0,
		RotationInterval: 0,
		QueueSize:        1000000,
		BlockOnFull:      true,
	}
	rw := benchWriter(b, cfg)
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()
	line := []byte(benchLine)

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := aw.Write(line); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkNamerFileName(b *testing.B) {
	cfg := (&Config{
		Dir:         "/data/logs",
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
	}).withDefaults()
	n := newNamer(cfg)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = n.FileName(baseTime, 1)
	}
}

func BenchmarkStampOf(b *testing.B) {
	cfg := (&Config{Dir: "/tmp", BaseName: "LOG", ServiceName: "s", ServicePort: 1}).withDefaults()
	n := newNamer(cfg)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = n.stampOf(baseTime)
	}
}

func BenchmarkParseFileName(b *testing.B) {
	cfg := (&Config{
		Dir:         "/data/logs",
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
	}).withDefaults()
	n := newNamer(cfg)
	name := "LOG_CALL_INFO.2026-09-03.playurl8080.01.log"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = n.parseFileName(name, nil)
	}
}

func BenchmarkCleanupScan(b *testing.B) {
	cfg := testConfigForBench(b)
	w := benchWriter(b, cfg)
	defer w.Close()

	for i := 0; i < 100; i++ {
		writeBenchFile(b, cfg, i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.cleaner.inFlight.Store(false)
		w.cleaner.Clean()
	}
}

func testConfigForBench(b *testing.B) *Config {
	b.Helper()
	return &Config{
		Dir:         b.TempDir(),
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
		Clock:       benchClockForTest{},
	}
}

type benchClockForTest struct{}

func (benchClockForTest) Now() time.Time                         { return baseTime }
func (benchClockForTest) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func writeBenchFile(b *testing.B, cfg *Config, i int) {
	b.Helper()
	name := cfg.BaseName + ".2026-09-03.playurl8080." + seqName(i+1) + ".log"
	path := filepath.Join(cfg.Dir, name)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 1024)), 0o644); err != nil {
		b.Fatal(err)
	}
}
