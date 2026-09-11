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

// Tests derived from go.uber.org/zap/sugar_test.go.
package sllogger

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

func sugaredLogger(t *testing.T) (*SugaredLogger, *slcoreWriteRecorder) {
	t.Helper()
	ws := newWriteRecorder()
	return New(slcore.NewCore(newTestEncoder(t), ws, DebugLevel)).Sugar(), ws
}

func TestSugarPrintStyle(t *testing.T) {
	sugar, ws := sugaredLogger(t)

	sugar.Debug("debug")
	sugar.Info("info")
	sugar.Warn("warn")
	sugar.Error("error")

	lines := ws.lines()
	want := []string{"debug", "info", "warn", "error"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %d, want %d", len(lines), len(want))
	}
	for i, w := range want {
		if !strings.Contains(lines[i], `"msg":"`+w+`"`) {
			t.Errorf("line %d = %s, want msg %q", i, lines[i], w)
		}
	}
}

func TestSugarPrintfStyle(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Infof("count=%d", 42)
	sugar.Debugf("rate=%.2f", 0.5)
	sugar.Warnf("user=%s", "alice")
	sugar.Errorf("err=%v", errors.New("boom"))

	lines := ws.lines()
	want := []string{"count=42", "rate=0.50", "user=alice", "err=boom"}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("line %d = %s, want %q", i, lines[i], w)
		}
	}
}

func TestSugarPrintlnStyle(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Infoln("a", "b")
	if got := ws.lines()[0]; !strings.Contains(got, "a b") {
		t.Fatalf("output = %s", got)
	}
}

func TestSugarStructuredStyle(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Infow("structured", "key", "value", "n", 3)
	line := ws.lines()[0]
	if !strings.Contains(line, `"key":"value"`) || !strings.Contains(line, `"n":3`) {
		t.Fatalf("output = %s", line)
	}
}

func TestSugarWith(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	child := sugar.With("service", "playurl")
	child.Info("hello")
	sugar.Info("world")

	lines := ws.lines()
	if !strings.Contains(lines[0], `"service":"playurl"`) {
		t.Fatalf("child = %s", lines[0])
	}
	if strings.Contains(lines[1], "playurl") {
		t.Fatalf("parent = %s", lines[1])
	}
}

func TestSugarWithFields(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.With(Int("i", 1)).Info("mixed")
	if got := ws.lines()[0]; !strings.Contains(got, `"i":1`) {
		t.Fatalf("output = %s", got)
	}
}

func TestSugarNamed(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Named("api").Info("named")
	if got := ws.lines()[0]; !strings.Contains(got, `"logger":"api"`) {
		t.Fatalf("output = %s", got)
	}
}

func TestSugarDesugarRoundTrip(t *testing.T) {
	log, _ := testLogger(t)
	sugar := log.Sugar()
	if sugar.Desugar().Name() != log.Name() {
		t.Fatal("Desugar did not restore the original logger")
	}
}

func TestSugarLevel(t *testing.T) {
	log, _ := testLogger(t)
	if got := log.Sugar().Level(); got != DebugLevel {
		t.Fatalf("Level() = %v", got)
	}
}

func TestSugarSweetenFieldsOddArgs(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	// A dangling key must be reported and ignored, not dropped silently.
	sugar.Infow("odd", "key")
	lines := ws.lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want an entry plus an error entry", len(lines))
	}
	if !strings.Contains(lines[0], _oddNumberErrMsg) {
		t.Fatalf("first line = %s", lines[0])
	}
}

func TestSugarSweetenFieldsNonStringKey(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Infow("bad key", 42, "value")
	lines := ws.lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if !strings.Contains(lines[0], _nonStringKeyErrMsg) {
		t.Fatalf("first line = %s", lines[0])
	}
}

func TestSugarSweetenFieldsErrors(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	err := errors.New("boom")
	sugar.Infow("with error", "err", err)
	if got := ws.lines()[0]; !strings.Contains(got, "boom") {
		t.Fatalf("output = %s", got)
	}

	// A second error is reported separately.
	sugar2, ws2 := sugaredLogger(t)
	sugar2.Infow("two errors", errors.New("first"), errors.New("second"))
	if got := ws2.lines()[0]; !strings.Contains(got, _multipleErrMsg) {
		t.Fatalf("output = %s", got)
	}
}

func TestSugarLogAndLogfAndLogw(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Log(WarnLevel, "log")
	sugar.Logf(ErrorLevel, "logf=%d", 1)
	sugar.Logw(InfoLevel, "logw", "k", "v")
	sugar.Logln(DebugLevel, "logln")

	lines := ws.lines()
	if !strings.Contains(lines[0], "WARN") || !strings.Contains(lines[0], `"msg":"log"`) {
		t.Fatalf("line 0 = %s", lines[0])
	}
	if !strings.Contains(lines[1], "logf=1") {
		t.Fatalf("line 1 = %s", lines[1])
	}
	if !strings.Contains(lines[2], `"k":"v"`) {
		t.Fatalf("line 2 = %s", lines[2])
	}
	if !strings.Contains(lines[3], "logln") {
		t.Fatalf("line 3 = %s", lines[3])
	}
}

func TestSugarSync(t *testing.T) {
	sugar, ws := sugaredLogger(t)
	sugar.Info("x")
	if err := sugar.Sync(); err != nil {
		t.Fatal(err)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.sync != 1 {
		t.Fatalf("sync = %d", ws.sync)
	}
}

func TestSugarDisabledLevelSkipsFormatting(t *testing.T) {
	ws := newWriteRecorder()
	sugar := New(slcore.NewCore(newTestEncoder(t), ws, ErrorLevel)).Sugar()

	// Even a formatting call must not produce output when disabled.
	sugar.Debugf("%d", 1)
	sugar.Debugw("nope", "k", "v")
	sugar.Debugln("nope")
	if got := len(ws.lines()); got != 0 {
		t.Fatalf("lines = %d, want 0", got)
	}
}

func TestSugarWithOptions(t *testing.T) {
	sugar, _ := sugaredLogger(t)
	child := sugar.WithOptions(AddCaller())
	if child.base.callerSkip == sugar.base.callerSkip && child.base.addCaller == sugar.base.addCaller {
		t.Fatal("WithOptions did not apply")
	}
}

func TestSugarClose(t *testing.T) {
	dir := t.TempDir()
	log := Must(Config{
		Level:       NewAtomicLevelAt(DebugLevel),
		OutputPaths: []string{dir + "/app.log"},
	}.Build())
	sugar := log.Sugar()
	sugar.Info("hello")
	if err := sugar.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGetMessage(t *testing.T) {
	tests := []struct {
		template string
		args     []interface{}
		want     string
	}{
		{"", nil, ""},
		{"plain", nil, "plain"},
		{"a=%d", []interface{}{1}, "a=1"},
		{"", []interface{}{"single"}, "single"},
		{"", []interface{}{1, 2}, "1 2"},
		{"", []interface{}{123}, "123"},
	}
	for _, tt := range tests {
		if got := getMessage(tt.template, tt.args); got != tt.want {
			t.Errorf("getMessage(%q, %v) = %q, want %q", tt.template, tt.args, got, tt.want)
		}
	}
}

func TestGetMessageln(t *testing.T) {
	if got := getMessageln([]interface{}{"a", "b"}); got != "a b" {
		t.Fatalf("getMessageln = %q", got)
	}
}
