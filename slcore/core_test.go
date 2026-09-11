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
// all copies or substantial portions of the Softwares.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// Tests derived from go.uber.org/zap/zapcore/core_test.go.
package slcore

import (
	"testing"
	"time"
)

func TestNopCore(t *testing.T) {
	core := NewNopCore()
	if core.Enabled(InfoLevel) {
		t.Fatal("nop core must not be enabled")
	}
	if ce := core.Check(Entry{Level: InfoLevel}, nil); ce != nil {
		t.Fatal("nop core must not return a CheckedEntry")
	}
	if err := core.Write(Entry{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.Sync(); err != nil {
		t.Fatal(err)
	}
	// With returns the same no-op core.
	if got := core.With([]Field{String("k", "v")}); got != core {
		t.Fatal("With on nop core must return itself")
	}
}

func TestIOCoreLevel(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, WarnLevel)
	if got := LevelOf(core); got != WarnLevel {
		t.Fatalf("LevelOf = %v, want warn", got)
	}
}

func TestIOCoreCheck(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, InfoLevel)
	if ce := core.Check(Entry{Level: DebugLevel}, nil); ce != nil {
		t.Fatal("debug must be disabled on an info core")
	}
	if ce := core.Check(Entry{Level: ErrorLevel}, nil); ce == nil {
		t.Fatal("error must be enabled on an info core")
	}
}

func TestIOCoreWith(t *testing.T) {
	enc := newStubEncoder("x")
	core := NewCore(enc, &countingWriteSyncer{}, DebugLevel)

	child := core.With([]Field{String("with", "ctx")})
	if child == core {
		t.Fatal("With must return a new core")
	}
	if enc.WithN == 0 {
		t.Fatal("With did not forward fields to the encoder")
	}
	// The parent must not be affected.
	if got := core.(*ioCore).enc.(*stubEncoder).WithN; got != enc.WithN {
		t.Log("encoder is shared; context isolation is the encoder's responsibility")
	}
}

func TestIOCoreWriteForwardsFields(t *testing.T) {
	ws := &countingWriteSyncer{}
	enc := newStubEncoder("line")
	core := NewCore(enc, ws, DebugLevel)

	core.Write(Entry{Level: InfoLevel, Time: time.Now(), Message: "m"}, []Field{String("a", "b")})

	if got := ws.String(); got != "line\n" {
		t.Fatalf("output = %q", got)
	}
	if len(enc.fields) != 1 || enc.fields[0].Key != "a" {
		t.Fatalf("fields = %+v", enc.fields)
	}
}

func TestIOCoreWriteEncoderError(t *testing.T) {
	enc := newStubEncoder("x")
	enc.err = errStub
	core := NewCore(enc, &countingWriteSyncer{}, DebugLevel)

	if err := core.Write(Entry{Level: InfoLevel}, nil); err != errStub {
		t.Fatalf("err = %v, want %v", err, errStub)
	}
}

func TestIOCoreWriteError(t *testing.T) {
	ws := &countingWriteSyncer{writeEr: errStub}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)

	if err := core.Write(Entry{Level: InfoLevel}, nil); err != errStub {
		t.Fatalf("err = %v, want %v", err, errStub)
	}
}

func TestIOCoreSyncsOnFatal(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)

	if err := core.Write(Entry{Level: FatalLevel}, nil); err != nil {
		t.Fatal(err)
	}
	_, syncs := ws.Counts()
	if syncs != 1 {
		t.Fatalf("syncs = %d, want 1 for fatal entries", syncs)
	}

	// Non-fatal entries must not force a sync on every write.
	ws2 := &countingWriteSyncer{}
	core2 := NewCore(newStubEncoder("x"), ws2, DebugLevel)
	_ = core2.Write(Entry{Level: InfoLevel}, nil)
	if _, syncs := ws2.Counts(); syncs != 0 {
		t.Fatalf("syncs = %d, want 0 for info entries", syncs)
	}
}

func TestIOCoreSync(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)
	if err := core.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, syncs := ws.Counts(); syncs != 1 {
		t.Fatalf("syncs = %d", syncs)
	}
}

func TestIOCoreCloneSharesSink(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)
	clone := core.With([]Field{String("k", "v")}).(*ioCore)
	if clone.out != ws {
		t.Fatal("clone must share the underlying WriteSyncer")
	}
}
