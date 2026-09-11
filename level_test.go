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

// Tests derived from go.uber.org/zap/level_test.go.
package sllogger

import (
	"sync"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

func TestAtomicLevel(t *testing.T) {
	lvl := NewAtomicLevel()
	if got := lvl.Level(); got != InfoLevel {
		t.Fatalf("default level = %v, want info", got)
	}

	lvl.SetLevel(WarnLevel)
	if got := lvl.Level(); got != WarnLevel {
		t.Fatalf("level = %v", got)
	}
	if lvl.Enabled(InfoLevel) {
		t.Fatal("info must be disabled at warn level")
	}
	if !lvl.Enabled(ErrorLevel) {
		t.Fatal("error must be enabled at warn level")
	}
	if got := lvl.String(); got != "warn" {
		t.Fatalf("String() = %q", got)
	}
}

func TestNewAtomicLevelAt(t *testing.T) {
	lvl := NewAtomicLevelAt(ErrorLevel)
	if got := lvl.Level(); got != ErrorLevel {
		t.Fatalf("level = %v", got)
	}
}

func TestParseAtomicLevel(t *testing.T) {
	tests := []struct {
		text string
		want Level
		ok   bool
	}{
		{"debug", DebugLevel, true},
		{"INFO", InfoLevel, true},
		{"warn", WarnLevel, true},
		{"error", ErrorLevel, true},
		{"bogus", InfoLevel, false},
	}
	for _, tt := range tests {
		got, err := ParseAtomicLevel(tt.text)
		if tt.ok && err != nil {
			t.Errorf("ParseAtomicLevel(%q): %v", tt.text, err)
			continue
		}
		if !tt.ok {
			if err == nil {
				t.Errorf("ParseAtomicLevel(%q) expected an error", tt.text)
			}
			continue
		}
		if got.Level() != tt.want {
			t.Errorf("ParseAtomicLevel(%q) = %v", tt.text, got.Level())
		}
	}
}

func TestAtomicLevelMarshalText(t *testing.T) {
	lvl := NewAtomicLevelAt(WarnLevel)
	text, err := lvl.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "warn" {
		t.Fatalf("MarshalText = %q", text)
	}
}

func TestAtomicLevelUnmarshalText(t *testing.T) {
	var lvl AtomicLevel
	if err := lvl.UnmarshalText([]byte("error")); err != nil {
		t.Fatal(err)
	}
	if lvl.Level() != ErrorLevel {
		t.Fatalf("level = %v", lvl.Level())
	}
	// The zero value must be usable.
	var zero AtomicLevel
	if err := zero.UnmarshalText([]byte("debug")); err != nil {
		t.Fatal(err)
	}
	if zero.Level() != DebugLevel {
		t.Fatalf("level = %v", zero.Level())
	}
	if err := zero.UnmarshalText([]byte("nope")); err == nil {
		t.Fatal("expected an error for an unknown level")
	}
}

func TestAtomicLevelConcurrent(t *testing.T) {
	lvl := NewAtomicLevel()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lvl.SetLevel(Level(i%7 - 1))
			_ = lvl.Enabled(InfoLevel)
			_ = lvl.Level()
		}(i)
	}
	wg.Wait()
}

func TestAtomicLevelImplementsLevelEnabler(t *testing.T) {
	var enab slcore.LevelEnabler = NewAtomicLevelAt(ErrorLevel)
	if enab.Enabled(DebugLevel) {
		t.Fatal("debug must be disabled")
	}
}

func TestLevelEnablerFunc(t *testing.T) {
	enab := LevelEnablerFunc(func(l Level) bool { return l >= WarnLevel })
	if enab.Enabled(InfoLevel) {
		t.Fatal("info must be disabled")
	}
	if !enab.Enabled(WarnLevel) {
		t.Fatal("warn must be enabled")
	}
}

func TestLevelConstantsMatchSlcore(t *testing.T) {
	if DebugLevel != slcore.DebugLevel || FatalLevel != slcore.FatalLevel {
		t.Fatal("level constants must alias slcore")
	}
}
