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

// Tests derived from go.uber.org/zap/options_test.go.
package sllogger

import (
	"io"
	"os"
	"testing"
	"time"

	"sllogger/slcore"
)

func TestOptionsApply(t *testing.T) {
	log, _ := testLogger(t)

	// WrapCore replaces the core.
	wrapped := log.WithOptions(WrapCore(func(slcore.Core) slcore.Core {
		return slcore.NewNopCore()
	}))
	wrapped.Info("dropped by the nop core")

	// The original logger is unaffected.
	log.Info("kept")
}

func TestOptionsHooks(t *testing.T) {
	log, ws := testLogger(t)

	calls := 0
	hooked := log.WithOptions(Hooks(func(slcore.Entry) error {
		calls++
		return nil
	}))
	hooked.Info("hooked")

	if calls != 1 {
		t.Fatalf("hook calls = %d, want 1", calls)
	}
	if got := len(ws.lines()); got != 1 {
		t.Fatalf("lines = %d", got)
	}
}

func TestOptionsFields(t *testing.T) {
	log, ws := testLogger(t)
	log.WithOptions(Fields(String("opt", "1"))).Info("with option field")
	if got := ws.lines()[0]; !contains(got, `"opt":"1"`) {
		t.Fatalf("output = %s", got)
	}
}

func TestOptionsErrorOutput(t *testing.T) {
	log, _ := testLogger(t)
	sink := newWriteRecorder()
	with := log.WithOptions(ErrorOutput(sink))
	if with.errorOutput != slcore.WriteSyncer(sink) {
		t.Fatal("ErrorOutput was not applied")
	}
}

func TestOptionsDevelopment(t *testing.T) {
	log, _ := testLogger(t)
	if log.WithOptions(Development()).development != true {
		t.Fatal("Development was not applied")
	}
}

func TestOptionsCaller(t *testing.T) {
	log, _ := testLogger(t)
	if !log.WithOptions(AddCaller()).addCaller {
		t.Fatal("AddCaller was not applied")
	}
	if log.WithOptions(WithCaller(false)).addCaller {
		t.Fatal("WithCaller(false) was not applied")
	}
}

func TestOptionsCallerSkip(t *testing.T) {
	log, _ := testLogger(t)
	base := log.callerSkip
	if got := log.WithOptions(AddCallerSkip(2)).callerSkip; got != base+2 {
		t.Fatalf("callerSkip = %d, want %d", got, base+2)
	}
}

func TestOptionsAddStacktrace(t *testing.T) {
	log, _ := testLogger(t)
	with := log.WithOptions(AddStacktrace(ErrorLevel))
	if !with.addStack.Enabled(ErrorLevel) || with.addStack.Enabled(WarnLevel) {
		t.Fatal("AddStacktrace was not applied at the requested level")
	}
}

func TestOptionsOnFatal(t *testing.T) {
	log, _ := testLogger(t)
	with := log.WithOptions(OnFatal(slcore.WriteThenNoop))
	if with.onFatal != slcore.CheckWriteHook(slcore.WriteThenNoop) {
		t.Fatal("OnFatal was not applied")
	}
	with = log.WithOptions(WithFatalHook(slcore.WriteThenGoexit))
	if with.onFatal != slcore.CheckWriteHook(slcore.WriteThenGoexit) {
		t.Fatal("WithFatalHook was not applied")
	}
}

func TestOptionsWithPanicHook(t *testing.T) {
	log, _ := testLogger(t)
	with := log.WithOptions(WithPanicHook(slcore.WriteThenGoexit))
	if with.onPanic != slcore.CheckWriteHook(slcore.WriteThenGoexit) {
		t.Fatal("WithPanicHook was not applied")
	}
}

func TestOptionsWithClock(t *testing.T) {
	log, ws := testLogger(t)
	fixed := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	with := log.WithOptions(WithClock(fixedClock{t: fixed}))

	with.Info("fixed clock")
	if got := ws.lines()[0]; !contains(got, "2026-09-03T12:00:00Z") {
		t.Fatalf("output = %s, want the injected clock time", got)
	}
}

func TestOptionFuncIsAnOption(t *testing.T) {
	var opt Option = optionFunc(func(*Logger) {})
	if opt == nil {
		t.Fatal("optionFunc must satisfy Option")
	}
}

func TestCombinedWriteSyncers(t *testing.T) {
	ws := CombineWriteSyncers()
	if ws == nil {
		t.Fatal("CombineWriteSyncers() must not return nil")
	}
	if _, err := ws.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}

	rec := newWriteRecorder()
	combined := CombineWriteSyncers(rec, rec)
	if _, err := combined.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if got := rec.String(); got != "abab" {
		t.Fatalf("output = %q, want abab", got)
	}
}

func TestOpenPaths(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/out.log"

	ws, closeFn, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := ws.Sync(); err != nil {
		t.Fatal(err)
	}
	closeFn()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
}

func TestOpenSpecialPaths(t *testing.T) {
	ws, closeFn, err := Open("stdout", "stderr")
	if err != nil {
		t.Fatal(err)
	}
	closeFn()
	if ws == nil {
		t.Fatal("Open returned nil")
	}
}

func TestOpenInvalidPath(t *testing.T) {
	// A path inside a non-existent directory must fail cleanly.
	if _, _, err := Open(t.TempDir() + "/missing/deep/out.log"); err == nil {
		t.Fatal("expected an error for an unopenable path")
	}
}

var _ = io.Discard
