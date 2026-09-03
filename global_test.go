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

// Tests derived from go.uber.org/zap/global_test.go.
package sllogger

import (
	stdlog "log"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGlobalLoggers(t *testing.T) {
	if L() == nil || S() == nil {
		t.Fatal("global loggers must not be nil")
	}
	// Writing through the nop globals must be safe.
	S().Info("noop")
	L().Info("noop")
}

func TestReplaceGlobals(t *testing.T) {
	initialL, initialS := L(), S()

	log, ws := testLogger(t)
	restore := ReplaceGlobals(log)
	if L() != log {
		t.Fatal("L() was not replaced")
	}

	L().Info("through global")
	S().Infof("through sugar %d", 1)

	restore()
	// ReplaceGlobals re-wraps the restored logger, so compare by behaviour.
	if L() != initialL {
		t.Fatal("L() was not restored")
	}
	if S().Desugar().Core() != initialS.Desugar().Core() {
		t.Fatal("S() was not restored")
	}
	// Both global entry points must have reached the replaced logger.
	lines := ws.lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for i, want := range []string{"through global", "through sugar 1"} {
		if !contains(lines[i], want) {
			t.Errorf("line %d = %s, want %q", i, lines[i], want)
		}
	}
}

func TestGlobalConcurrentAccess(t *testing.T) {
	log, _ := testLogger(t)
	restore := ReplaceGlobals(log)
	defer restore()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			L().Info("a")
			S().Info("b")
		}()
	}
	wg.Wait()
}

func TestNewStdLog(t *testing.T) {
	log, ws := testLogger(t)
	std := NewStdLog(log)
	std.Print("from std log")

	if got := ws.lines()[0]; !contains(got, "from std log") {
		t.Fatalf("output = %s", got)
	}
}

func TestNewStdLogAt(t *testing.T) {
	log, ws := testLogger(t)
	std, err := NewStdLogAt(log, WarnLevel)
	if err != nil {
		t.Fatal(err)
	}
	std.Print("warning from std log")

	if got := ws.lines()[0]; !contains(got, "WARN") {
		t.Fatalf("output = %s", got)
	}
	if _, err := NewStdLogAt(log, Level(99)); err == nil {
		t.Fatal("expected an error for an invalid level")
	}
}

func TestRedirectStdLog(t *testing.T) {
	log, ws := testLogger(t)
	restore := RedirectStdLog(log)
	defer restore()

	stdlog.Print("redirected")
	if got := ws.lines()[0]; !contains(got, "redirected") {
		t.Fatalf("output = %s", got)
	}
}

func TestRedirectStdLogAt(t *testing.T) {
	log, ws := testLogger(t)
	restore, err := RedirectStdLogAt(log, ErrorLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()

	stdlog.Print("redirected as error")
	if got := ws.lines()[0]; !contains(got, "ERROR") {
		t.Fatalf("output = %s", got)
	}
}

// TestRedirectStdLogAtInvalidLevel 覆盖 redirectStdLogAt 中
// levelToFunc 返回错误的分支（只有通过 RedirectStdLogAt 传入非法级别可达）。
func TestRedirectStdLogAtInvalidLevel(t *testing.T) {
	log, _ := testLogger(t)

	restore, err := RedirectStdLogAt(log, Level(99))
	if err == nil {
		restore()
		t.Fatal("expected an error for an invalid level")
	}
	if restore != nil {
		t.Fatal("restore func must be nil on error")
	}
	// 失败时不应改变标准库 logger 的输出目标。
	stdlog.Print("still goes to the original destination")
}

func TestLevelToFunc(t *testing.T) {
	log, ws := testLogger(t)
	for _, lvl := range []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel} {
		fn, err := levelToFunc(log, lvl)
		if err != nil {
			t.Fatalf("levelToFunc(%v): %v", lvl, err)
		}
		fn("message at " + lvl.String())
	}
	if got := len(ws.lines()); got != 4 {
		t.Fatalf("lines = %d, want 4", got)
	}
	if _, err := levelToFunc(log, Level(99)); err == nil {
		t.Fatal("expected an error for an invalid level")
	}
}

func TestLoggerWriterTrimsWhitespace(t *testing.T) {
	log, ws := testLogger(t)
	w := &loggerWriter{log.Info}
	n, err := w.Write([]byte("  spaced  \n"))
	if err != nil {
		t.Fatal(err)
	}
	// Like zap, the writer reports the length of the trimmed payload.
	if n != len("spaced") {
		t.Fatalf("n = %d, want %d", n, len("spaced"))
	}
	if got := ws.lines()[0]; strings.HasPrefix(got, "  ") {
		t.Fatalf("output was not trimmed: %s", got)
	}
}

func TestStdLogConcurrent(t *testing.T) {
	log, _ := testLogger(t)
	std := NewStdLog(log)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			std.Printf("entry %d", i)
		}(i)
	}
	wg.Wait()
	time.Sleep(10 * time.Millisecond)
}
