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

package sllogger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/writer"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time                         { return c.t }
func (c fixedClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func TestCallInfoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logTime := time.Date(2021, 11, 14, 20, 37, 34, 376000000, time.Local)

	cfg := NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Level = NewAtomicLevelAt(DebugLevel)
	cfg.Clock = fixedClock{t: logTime}
	cfg.Rolling.Clock = fixedClock{t: logTime}
	cfg.Rolling.RotationInterval = 0
	cfg.Rolling.MaxSize = 0

	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithTrace(context.Background(),
		"3c06e3121c18f6114a2e9f2e38e5b8fe",
		"b256f6eac12c9818")

	log.CallInfo(ctx, CallInfo{
		Mobile:   "18237438309",
		UserID:   "1071748417",
		ClientID: "c6559e74a24df8d97a2296ff1e23387a",
		URL:      "http://play.example.com:443/playurl/v1/play/playurl",
		Method:   "GET",
		UseTime:  123,
		ServerIP: "127.0.0.1",
		BussID:   "null",
		LogMsg:   "^MG.getContent:[690894368]",
	})

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "LOG_CALL_INFO.2021-11-14.playurl8080.log"))
	if err != nil {
		t.Fatal(err)
	}
	want := "2021-11-14 20:37:34.376|INFO|playurl|" +
		"3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|" +
		"18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|" +
		"http://play.example.com:443/playurl/v1/play/playurl|GET|123|" +
		"127.0.0.1|null|^MG.getContent:[690894368]\n"
	if got := string(data); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAsyncEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logTime := time.Date(2026, 9, 3, 11, 0, 0, 0, time.Local)

	cfg := NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.Clock = fixedClock{t: logTime}
	cfg.Rolling.Async = true
	// Blocking mode: call-info logs must never be dropped in this scenario.
	cfg.Rolling.BlockOnFull = true
	cfg.Rolling.QueueSize = 16
	cfg.Rolling.BatchSize = 4
	cfg.Rolling.FlushInterval = 10 * time.Millisecond
	cfg.Rolling.RotationInterval = time.Hour
	cfg.Rolling.MaxSize = 256

	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	ctx := WithTrace(context.Background(), "t1", "s1")
	for i := 0; i < 100; i++ {
		log.CallInfo(ctx, CallInfo{
			URL:     "http://svc/api",
			Method:  "GET",
			UseTime: int64(i),
			LogMsg:  "msg",
		})
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		total += strings.Count(string(b), "\n")
	}
	if total != 100 {
		t.Fatalf("expected 100 entries after close, got %d", total)
	}
}

func TestRequestInfoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfg := NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Encoding = "requestinfo"
	cfg.Clock = fixedClock{t: time.Date(2026, 9, 3, 11, 0, 0, 0, time.Local)}
	cfg.Rolling.RotationInterval = 0

	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithTrace(context.Background(), "tid", "sid")
	log.RequestInfo(ctx, RequestInfo{
		Header:      "h1=v1",
		ReqHeader:   "h2=v2",
		Method:      "POST",
		RateLimiter: "false",
		LogMsg:      "request success",
	})
	log.Close()

	b, err := os.ReadFile(filepath.Join(dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log"))
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-09-03 11:00:00.000|INFO|playurl|tid|sid|h1=v1|h2=v2|POST|false|request success\n"
	if got := string(b); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSugarAndGlobal(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Level:       NewAtomicLevelAt(DebugLevel),
		Encoding:    "json",
		OutputPaths: []string{filepath.Join(dir, "app.log")},
	}
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	restore := ReplaceGlobals(log)
	defer restore()

	S().Infow("hello", "key", "value")
	S().Infof("count=%d", 42)
	L().Info("plain", Int("n", 1))

	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), b)
	}
	if !strings.Contains(lines[0], `"msg":"hello"`) || !strings.Contains(lines[0], `"key":"value"`) {
		t.Fatalf("unexpected json output: %s", lines[0])
	}
}

func TestRollingConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	w, err := writer.NewRollingWriter(&writer.Config{
		Dir:      dir,
		BaseName: "app",
		Clock:    fixedClock{t: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	cfg := w.Config()
	if cfg.MaxSize != writer.DefaultMaxSize {
		t.Fatalf("MaxSize default = %d", cfg.MaxSize)
	}
	if cfg.RotationInterval != writer.DefaultRotationInterval {
		t.Fatalf("RotationInterval default = %v", cfg.RotationInterval)
	}
	if cfg.QueueSize != writer.DefaultQueueSize {
		t.Fatalf("QueueSize default = %d", cfg.QueueSize)
	}
}
