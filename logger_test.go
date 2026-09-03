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

// Tests derived from go.uber.org/zap/logger_test.go.
package sllogger

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sllogger/encoder"
	"sllogger/slcore"
)

func testLogger(t *testing.T) (*Logger, *slcoreWriteRecorder) {
	t.Helper()
	ws := newWriteRecorder()
	log := New(slcore.NewCore(newTestEncoder(t), ws, DebugLevel))
	return log, ws
}

// newTestEncoder returns a JSON encoder with the conventional key names.
func newTestEncoder(t *testing.T) slcore.Encoder {
	t.Helper()
	return encoder.NewJSONEncoder(encoder.DefaultJSONEncoderConfig())
}

// slcoreWriteRecorder captures encoded output.
type slcoreWriteRecorder struct {
	mu   sync.Mutex
	buf  strings.Builder
	sync int
}

func newWriteRecorder() *slcoreWriteRecorder { return &slcoreWriteRecorder{} }

func (w *slcoreWriteRecorder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *slcoreWriteRecorder) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sync++
	return nil
}

func (w *slcoreWriteRecorder) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *slcoreWriteRecorder) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	trimmed := strings.TrimSpace(w.buf.String())
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestNewNilCore(t *testing.T) {
	log := New(nil)
	if _, ok := log.core.(interface{ Enabled(slcore.Level) bool }); !ok {
		t.Fatal("New(nil) must build a usable core")
	}
	log.Info("no-op")
}

func TestNewNop(t *testing.T) {
	log := NewNop()
	log.Info("ignored")
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestMust(t *testing.T) {
	log := Must(New(slcore.NewCore(
		mustTestEncoder(t),
		slcore.AddSync(io.Discard),
		DebugLevel,
	)), nil)
	if log == nil {
		t.Fatal("Must returned nil")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Must must panic on error")
		}
	}()
	Must(nil, errors.New("boom"))
}

func mustTestEncoder(t *testing.T) slcore.Encoder {
	t.Helper()
	enc := newTestEncoder(t)
	return enc
}

func TestLoggerLevels(t *testing.T) {
	log, ws := testLogger(t)

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")

	lines := ws.lines()
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	for i, want := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %s, want level %s", i, lines[i], want)
		}
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	enc := newTestEncoder(t)
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, WarnLevel))

	log.Debug("no")
	log.Info("no")
	log.Warn("yes")
	if got := len(ws.lines()); got != 1 {
		t.Fatalf("lines = %d, want 1", got)
	}
	if got := log.Level(); got != WarnLevel {
		t.Fatalf("Level() = %v, want warn", got)
	}
}

func TestLoggerWith(t *testing.T) {
	log, ws := testLogger(t)

	child := log.With(String("component", "auth"))
	child.Info("hello")
	log.Info("parent")

	lines := ws.lines()
	if !strings.Contains(lines[0], `"component":"auth"`) {
		t.Fatalf("child lost its context: %s", lines[0])
	}
	if strings.Contains(lines[1], "component") {
		t.Fatalf("parent inherited the child's context: %s", lines[1])
	}
	// With on an empty field list returns the same logger.
	if got := log.With(); got != log {
		t.Fatal("With() with no fields must return the same logger")
	}
}

func TestLoggerNamed(t *testing.T) {
	log, ws := testLogger(t)

	if got := log.Name(); got != "" {
		t.Fatalf("root name = %q, want empty", got)
	}
	child := log.Named("api").Named("v1")
	if got := child.Name(); got != "api.v1" {
		t.Fatalf("name = %q, want api.v1", got)
	}
	// Named("") is a no-op.
	if got := log.Named(""); got.Name() != "" {
		t.Fatal("Named(\"\") must not change the name")
	}

	child.Info("named")
	if got := ws.lines()[0]; !strings.Contains(got, `"logger":"api.v1"`) {
		t.Fatalf("output = %s", got)
	}
}

func TestLoggerCheck(t *testing.T) {
	log, ws := testLogger(t)

	ce := log.Check(InfoLevel, "checked")
	if ce == nil {
		t.Fatal("Check returned nil")
	}
	ce.Write(String("k", "v"))

	if got := ws.lines()[0]; !strings.Contains(got, "checked") {
		t.Fatalf("output = %s", got)
	}
}

func TestLoggerLog(t *testing.T) {
	log, ws := testLogger(t)
	log.Log(WarnLevel, "custom level")
	if got := ws.lines()[0]; !strings.Contains(got, "WARN") {
		t.Fatalf("output = %s", got)
	}
}

func TestLoggerSync(t *testing.T) {
	log, ws := testLogger(t)
	log.Info("x")
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.sync != 1 {
		t.Fatalf("sync = %d, want 1", ws.sync)
	}
}

func TestLoggerCallerAnnotation(t *testing.T) {
	enc := newTestEncoder(t)
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, DebugLevel), AddCaller())

	log.Info("with caller")
	if got := ws.lines()[0]; !strings.Contains(got, "logger_test.go") {
		t.Fatalf("caller missing: %s", got)
	}
}

func TestLoggerCallerSkip(t *testing.T) {
	enc := newTestEncoder(t)
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, DebugLevel), AddCaller(), AddCallerSkip(1))

	// The skip makes the reported caller this test file instead of the
	// logging wrapper.
	log.Info("skipped")
	if got := ws.lines()[0]; !strings.Contains(got, "caller") {
		t.Fatalf("output = %s", got)
	}
}

func TestLoggerStacktrace(t *testing.T) {
	enc := newTestEncoder(t)
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, DebugLevel), AddStacktrace(WarnLevel))

	log.Info("no stack")
	log.Warn("with stack")

	lines := ws.lines()
	if strings.Contains(lines[0], "stacktrace") {
		t.Fatalf("info should not carry a stack: %s", lines[0])
	}
	if !strings.Contains(lines[1], "stacktrace") {
		t.Fatalf("warn should carry a stack: %s", lines[1])
	}
}

func TestLoggerDPanic(t *testing.T) {
	enc := newTestEncoder(t)
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, DebugLevel))

	// Production mode: DPanic logs without panicking.
	log.DPanic("no panic")

	// Development mode: DPanic panics.
	dev := log.WithOptions(Development())
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("DPanic must panic in development mode")
			}
		}()
		dev.DPanic("panic now")
	}()
}

func TestLoggerPanicHook(t *testing.T) {
	enc := newTestEncoder(t)
	log := New(slcore.NewCore(enc, newWriteRecorder(), DebugLevel),
		WithPanicHook(slcore.WriteThenGoexit))

	done := make(chan struct{})
	go func() {
		defer close(done)
		log.Panic("goexit instead of panic")
		t.Error("execution continued after Panic")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("panic hook was not applied")
	}
}

func TestLoggerFatalHook(t *testing.T) {
	enc := newTestEncoder(t)
	log := New(slcore.NewCore(enc, newWriteRecorder(), DebugLevel),
		WithFatalHook(slcore.WriteThenGoexit))

	done := make(chan struct{})
	go func() {
		defer close(done)
		log.Fatal("goexit instead of os.Exit")
		t.Error("execution continued after Fatal")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fatal hook was not applied")
	}
}

func TestTerminalHookOverrideFallsBackToDefault(t *testing.T) {
	// A no-op hook must not swallow Panic/Fatal semantics.
	if got := terminalHookOverride(slcore.WriteThenPanic, nil); got != slcore.WriteThenPanic {
		t.Fatal("nil hook must fall back to the default")
	}
	if got := terminalHookOverride(slcore.WriteThenPanic, slcore.WriteThenNoop); got != slcore.WriteThenPanic {
		t.Fatal("WriteThenNoop must fall back to the default")
	}
	custom := slcore.WriteThenGoexit
	if got := terminalHookOverride(slcore.WriteThenPanic, custom); got != custom {
		t.Fatal("explicit hook must be honoured")
	}
}

func TestLoggerCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	log := Must(Config{
		Level:       NewAtomicLevelAt(DebugLevel),
		OutputPaths: []string{filepath.Join(dir, "app.log")},
	}.Build())

	log.Info("hello")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("content = %s", data)
	}
}

func TestLoggerCore(t *testing.T) {
	log, _ := testLogger(t)
	if log.Core() == nil {
		t.Fatal("Core() is nil")
	}
}

func TestLoggerWithOptionsDoesNotMutate(t *testing.T) {
	log, _ := testLogger(t)
	child := log.WithOptions(ErrorOutput(slcore.AddSync(io.Discard)))
	if child == log {
		t.Fatal("WithOptions must clone")
	}
	if log.errorOutput == child.errorOutput {
		t.Fatal("options leaked into the parent")
	}
}

func TestLoggerConcurrentUse(t *testing.T) {
	log, ws := testLogger(t)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				log.With(String("g", "v")).Info("concurrent")
			}
		}(g)
	}
	wg.Wait()
	if got := len(ws.lines()); got != 400 {
		t.Fatalf("lines = %d, want 400", got)
	}
}
