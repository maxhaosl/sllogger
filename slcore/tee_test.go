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

package slcore

import (
	"errors"
	"testing"
	"time"
)

func TestNewTeeNoCores(t *testing.T) {
	core := NewTee()
	if core.Enabled(DebugLevel) {
		t.Fatal("empty tee should be disabled")
	}
	if ce := core.Check(Entry{Level: ErrorLevel}, nil); ce != nil {
		t.Fatal("empty tee should not check")
	}
	if err := core.Sync(); err != nil {
		t.Fatalf("empty tee Sync should be nil, got %v", err)
	}
}

func TestNewTeeSingleCore(t *testing.T) {
	c := newSpyCore(DebugLevel)
	core := NewTee(c)
	if core != Core(c) {
		t.Fatal("NewTee with a single core should return it unchanged")
	}
}

func TestNewTeeFanOut(t *testing.T) {
	a := newSpyCore(DebugLevel)
	b := newSpyCore(DebugLevel)
	core := NewTee(a, b)

	e := Entry{Level: InfoLevel, Message: "hi", Time: time.Now()}
	var ce *CheckedEntry
	ce = core.Check(e, ce)
	if ce == nil {
		t.Fatal("expected Check to pass")
	}
	ce.Write()

	if len(a.rec.writes) != 1 || len(b.rec.writes) != 1 {
		t.Fatalf("expected each core written once; a=%d b=%d", len(a.rec.writes), len(b.rec.writes))
	}
	if err := core.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if a.rec.syncs != 1 || b.rec.syncs != 1 {
		t.Fatalf("expected each core synced once; a=%d b=%d", a.rec.syncs, b.rec.syncs)
	}
}

func TestNewTeeEnabled(t *testing.T) {
	low := newSpyCore(DebugLevel)
	high := newSpyCore(WarnLevel)
	core := NewTee(low, high)
	// Both children enabled at Warn+, so the tee is enabled at Warn.
	if !core.Enabled(DebugLevel) {
		t.Fatal("tee should be enabled at DebugLevel via the low core")
	}
	if !core.Enabled(WarnLevel) {
		t.Fatal("tee should be enabled at WarnLevel")
	}
}

func TestNewTeeLevel(t *testing.T) {
	// Two equally-enabled cores report a concrete level (not InvalidLevel).
	a := newSpyCore(InfoLevel)
	b := newSpyCore(InfoLevel)
	core := NewTee(a, b)
	if got := LevelOf(core); got == InvalidLevel {
		t.Fatal("tee with equal enablers should report a concrete level")
	}
}

func TestNewTeeWith(t *testing.T) {
	a := newSpyCore(DebugLevel)
	b := newSpyCore(DebugLevel)
	core := NewTee(a, b)
	child := core.With([]Field{String("k", "v")})
	if _, ok := child.(multiCore); !ok {
		t.Fatalf("With should return multiCore, got %T", child)
	}
	if len(a.rec.fields) != 1 || len(b.rec.fields) != 1 {
		t.Fatalf("With should propagate to each child; a=%d b=%d", len(a.rec.fields), len(b.rec.fields))
	}
	if len(a.rec.fields[0]) != 1 || len(b.rec.fields[0]) != 1 {
		t.Fatal("With should pass the field to each child")
	}
}

func TestNewTeeWriteError(t *testing.T) {
	good := newSpyCore(DebugLevel)
	bad := newErrCore(DebugLevel, errors.New("boom"), nil)
	core := NewTee(good, bad)

	e := Entry{Level: InfoLevel, Message: "x", Time: time.Now()}
	if err := core.Write(e, nil); err == nil {
		t.Fatal("expected aggregated error from failing child")
	}
	if len(good.rec.writes) != 1 {
		t.Fatalf("good core should still be written despite sibling error; got %d", len(good.rec.writes))
	}
}
