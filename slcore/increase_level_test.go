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
	"testing"
	"time"
)

func TestNewIncreaseLevelCore(t *testing.T) {
	base := newSpyCore(DebugLevel)
	core, err := NewIncreaseLevelCore(base, WarnLevel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !core.Enabled(WarnLevel) {
		t.Fatal("core should be enabled at WarnLevel")
	}
	if core.Enabled(InfoLevel) {
		t.Fatal("core must NOT be enabled at InfoLevel after raising the level")
	}
	if ce := core.Check(Entry{Level: InfoLevel, Message: "x", Time: time.Now()}, nil); ce != nil {
		t.Fatal("InfoLevel check should be filtered out")
	}

	ce := core.Check(Entry{Level: WarnLevel, Message: "y", Time: time.Now()}, nil)
	if ce == nil {
		t.Fatal("WarnLevel check should pass")
	}
	ce.Write()
	if len(base.rec.writes) != 1 {
		t.Fatalf("expected 1 write to base core, got %d", len(base.rec.writes))
	}
}

func TestNewIncreaseLevelCoreInvalid(t *testing.T) {
	// Can't raise the level to DebugLevel if the wrapped core only emits Error+.
	base := newSpyCore(ErrorLevel)
	if _, err := NewIncreaseLevelCore(base, DebugLevel); err == nil {
		t.Fatal("expected error when raising base below its enabled level")
	}
}

func TestNewIncreaseLevelCoreValidHigher(t *testing.T) {
	// Raising DebugLevel -> ErrorLevel is valid: every level the new filter
	// enables is also enabled by the underlying core.
	base := newSpyCore(DebugLevel)
	if _, err := NewIncreaseLevelCore(base, Level(ErrorLevel)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewIncreaseLevelCoreWith(t *testing.T) {
	base := newSpyCore(DebugLevel)
	core, err := NewIncreaseLevelCore(base, WarnLevel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	child := core.With([]Field{String("k", "v")})
	if _, ok := child.(*levelFilterCore); !ok {
		t.Fatalf("With should return *levelFilterCore, got %T", child)
	}
	if len(base.rec.fields) != 1 {
		t.Fatalf("With should propagate to the base core; got %d", len(base.rec.fields))
	}
}

func TestNewIncreaseLevelCoreLevel(t *testing.T) {
	base := newSpyCore(DebugLevel)
	core, err := NewIncreaseLevelCore(base, WarnLevel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := LevelOf(core); got < WarnLevel {
		t.Fatalf("LevelOf should be >= WarnLevel, got %v", got)
	}
}
