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

// Tests derived from go.uber.org/zap/zapcore/entry_test.go.
package slcore

import (
	"testing"
	"time"
)

func TestNewEntryCaller(t *testing.T) {
	tests := []struct {
		ok   bool
		want EntryCaller
	}{
		{true, EntryCaller{Defined: true, PC: 42, File: "foo.go", Line: 10}},
		{false, EntryCaller{}},
	}
	for _, tt := range tests {
		got := NewEntryCaller(42, "foo.go", 10, tt.ok)
		if got != tt.want {
			t.Errorf("NewEntryCaller(ok=%v) = %+v, want %+v", tt.ok, got, tt.want)
		}
	}
}

func TestEntryCallerTrimmedPath(t *testing.T) {
	tests := []struct {
		caller EntryCaller
		full   string
		short  string
	}{
		{
			caller: EntryCaller{},
			full:   "undefined",
			short:  "undefined",
		},
		{
			caller: EntryCaller{Defined: true, File: "/home/user/proj/pkg/file.go", Line: 7},
			full:   "/home/user/proj/pkg/file.go:7",
			short:  "pkg/file.go:7",
		},
		{
			caller: EntryCaller{Defined: true, File: "file.go", Line: 7},
			full:   "file.go:7",
			short:  "file.go:7",
		},
		{
			caller: EntryCaller{Defined: true, File: "pkg/file.go", Line: 7},
			full:   "pkg/file.go:7",
			short:  "pkg/file.go:7",
		},
	}
	for _, tt := range tests {
		if got := tt.caller.FullPath(); got != tt.full {
			t.Errorf("FullPath() = %q, want %q", got, tt.full)
		}
		if got := tt.caller.TrimmedPath(); got != tt.short {
			t.Errorf("TrimmedPath() = %q, want %q", got, tt.short)
		}
		if got := tt.caller.String(); got != tt.full {
			t.Errorf("String() = %q, want %q", got, tt.full)
		}
	}
}

func TestCheckedEntryWrite(t *testing.T) {
	ws := &countingWriteSyncer{}
	enc := newStubEncoder("hello")
	core := NewCore(enc, ws, DebugLevel)

	ent := Entry{Level: InfoLevel, Time: time.Now(), Message: "msg"}
	ce := core.Check(ent, nil)
	if ce == nil {
		t.Fatal("Check returned nil for enabled level")
	}
	ce.Write(String("k", "v"))

	writes, _ := ws.counts()
	if writes != 1 {
		t.Fatalf("writes = %d", writes)
	}
	if got := ws.String(); got != "hello\n" {
		t.Fatalf("output = %q", got)
	}
	if len(enc.fields) != 1 || enc.fields[0].Key != "k" {
		t.Fatalf("fields not forwarded: %+v", enc.fields)
	}
}

func TestCheckedEntryDisabledLevel(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, ErrorLevel)
	if ce := core.Check(Entry{Level: DebugLevel}, nil); ce != nil {
		t.Fatal("Check must return nil for disabled level")
	}
}

func TestCheckedEntryNilIsSafe(t *testing.T) {
	var ce *CheckedEntry
	// All of these must be no-ops rather than panics.
	ce.Write(String("k", "v"))
	if got := ce.AddCore(Entry{}, NewNopCore()); got == nil {
		t.Fatal("AddCore on nil must allocate a CheckedEntry")
	}
	if got := ce.After(Entry{}, WriteThenNoop); got == nil {
		t.Fatal("After on nil must allocate a CheckedEntry")
	}
	if got := ce.Before(Entry{}, func(e Entry, f []Field) (Entry, []Field) { return e, f }); got == nil {
		t.Fatal("Before on nil must allocate a CheckedEntry")
	}
	if got := ce.Should(Entry{}, WriteThenGoexit); got == nil {
		t.Fatal("Should on nil must allocate a CheckedEntry")
	}
}

func TestCheckedEntryAddCoreAccumulates(t *testing.T) {
	var ce *CheckedEntry
	ent := Entry{Level: InfoLevel}
	ce = ce.AddCore(ent, NewNopCore())
	ce = ce.AddCore(ent, NewNopCore())
	if len(ce.cores) != 2 {
		t.Fatalf("cores = %d", len(ce.cores))
	}
}

func TestCheckedEntryBeforeHooks(t *testing.T) {
	ws := &countingWriteSyncer{}
	enc := newStubEncoder("out")
	core := NewCore(enc, ws, DebugLevel)

	ce := core.Check(Entry{Level: InfoLevel, Message: "orig"}, nil)
	ce = ce.Before(Entry{Level: InfoLevel, Message: "orig"}, func(ent Entry, fs []Field) (Entry, []Field) {
		ent.Message = "mutated"
		return ent, append(fs, String("added", "1"))
	})
	ce.Write()

	if got := enc.fields[0].Key; got != "added" {
		t.Fatalf("hook field missing: %+v", enc.fields)
	}
}

func TestCheckedEntryAfterHook(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("out"), ws, DebugLevel)

	called := false
	ce := core.Check(Entry{Level: InfoLevel}, nil)
	ce = ce.After(Entry{Level: InfoLevel}, afterFunc(func(*CheckedEntry, []Field) {
		called = true
	}))
	ce.Write()

	if !called {
		t.Fatal("after hook not called")
	}
}

type afterFunc func(*CheckedEntry, []Field)

func (f afterFunc) OnWrite(ce *CheckedEntry, fs []Field) { f(ce, fs) }

func TestCheckedEntryReuseDetection(t *testing.T) {
	errOut := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("out"), &countingWriteSyncer{}, DebugLevel)

	ce := core.Check(Entry{Level: InfoLevel}, nil)
	ce.ErrorOutput = errOut
	ce.Write()

	// Writing the same CheckedEntry again must be detected and reported.
	ce.Write()
	if errOut.writes == 0 {
		t.Fatal("unsafe re-use was not reported to ErrorOutput")
	}
}

func TestCheckedEntryWriteErrorReported(t *testing.T) {
	errOut := &countingWriteSyncer{}
	enc := newStubEncoder("out")
	enc.err = errStub
	core := NewCore(enc, &countingWriteSyncer{}, DebugLevel)

	ce := core.Check(Entry{Level: InfoLevel}, nil)
	ce.ErrorOutput = errOut
	ce.Write()

	if errOut.writes == 0 {
		t.Fatal("write error was not reported to ErrorOutput")
	}
}

func TestPutCheckedEntryNil(t *testing.T) {
	// Must not panic so that Write on a nil entry stays safe.
	putCheckedEntry(nil)
}

func TestCheckedEntryResetClearsCores(t *testing.T) {
	ce := getCheckedEntry()
	ce.AddCore(Entry{}, NewNopCore())
	ce.reset()
	if len(ce.cores) != 0 {
		t.Fatalf("cores not cleared: %d", len(ce.cores))
	}
	putCheckedEntry(ce)
}
