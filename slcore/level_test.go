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

// Tests derived from go.uber.org/zap/zapcore/level_test.go.
package slcore

import (
	"testing"
)

func TestLevelString(t *testing.T) {
	tests := []struct {
		lvl  Level
		want string
	}{
		{DebugLevel, "debug"},
		{InfoLevel, "info"},
		{WarnLevel, "warn"},
		{ErrorLevel, "error"},
		{DPanicLevel, "dpanic"},
		{PanicLevel, "panic"},
		{FatalLevel, "fatal"},
		{Level(42), "Level(42)"},
	}
	for _, tt := range tests {
		if got := tt.lvl.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", int8(tt.lvl), got, tt.want)
		}
	}
}

func TestLevelCapitalString(t *testing.T) {
	tests := []struct {
		lvl  Level
		want string
	}{
		{DebugLevel, "DEBUG"},
		{InfoLevel, "INFO"},
		{WarnLevel, "WARN"},
		{ErrorLevel, "ERROR"},
		{DPanicLevel, "DPANIC"},
		{PanicLevel, "PANIC"},
		{FatalLevel, "FATAL"},
		{Level(-2), "LEVEL(-2)"},
		{Level(42), "LEVEL(42)"},
	}
	for _, tt := range tests {
		if got := tt.lvl.CapitalString(); got != tt.want {
			t.Errorf("Level(%d).CapitalString() = %q, want %q", int8(tt.lvl), got, tt.want)
		}
	}
}

func TestLevelUnmarshalText(t *testing.T) {
	tests := []struct {
		text string
		want Level
		ok   bool
	}{
		{"debug", DebugLevel, true},
		{"DEBUG", DebugLevel, true},
		{"info", InfoLevel, true},
		{"", InfoLevel, true}, // zero value is useful
		{"warn", WarnLevel, true},
		{"warning", WarnLevel, true},
		{"WARNING", WarnLevel, true},
		{"error", ErrorLevel, true},
		{"dpanic", DPanicLevel, true},
		{"panic", PanicLevel, true},
		{"fatal", FatalLevel, true},
		{"nope", InfoLevel, false},
	}
	for _, tt := range tests {
		var lvl Level
		err := lvl.UnmarshalText([]byte(tt.text))
		if tt.ok && err != nil {
			t.Errorf("UnmarshalText(%q) = %v", tt.text, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("UnmarshalText(%q) expected error", tt.text)
		}
		if tt.ok && lvl != tt.want {
			t.Errorf("UnmarshalText(%q) = %v, want %v", tt.text, lvl, tt.want)
		}
	}
}

func TestLevelUnmarshalNilPointer(t *testing.T) {
	var lvl *Level
	if err := lvl.UnmarshalText([]byte("info")); err != errUnmarshalNilLevel {
		t.Fatalf("expected errUnmarshalNilLevel, got %v", err)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		text string
		want Level
		ok   bool
	}{
		{"info", InfoLevel, true},
		{"ERROR", ErrorLevel, true},
		{"bogus", InfoLevel, false},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.text)
		if tt.ok && err != nil {
			t.Errorf("ParseLevel(%q) error: %v", tt.text, err)
		}
		if tt.ok && got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.text, got, tt.want)
		}
		if !tt.ok && err == nil {
			t.Errorf("ParseLevel(%q) expected error", tt.text)
		}
	}
}

func TestLevelMarshalText(t *testing.T) {
	got, err := WarnLevel.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "warn" {
		t.Fatalf("MarshalText = %q", got)
	}
}

func TestLevelEnabled(t *testing.T) {
	tests := []struct {
		lvl, check Level
		want       bool
	}{
		{DebugLevel, DebugLevel, true},
		{DebugLevel, InfoLevel, true},
		{InfoLevel, DebugLevel, false},
		{InfoLevel, InfoLevel, true},
		{ErrorLevel, PanicLevel, true},
		{PanicLevel, ErrorLevel, false},
	}
	for _, tt := range tests {
		if got := tt.lvl.Enabled(tt.check); got != tt.want {
			t.Errorf("%v.Enabled(%v) = %v", tt.lvl, tt.check, got)
		}
	}
}

func TestLevelOf(t *testing.T) {
	if got := LevelOf(WarnLevel); got != WarnLevel {
		t.Fatalf("LevelOf(WarnLevel) = %v", got)
	}
	// A core that reports its own level wins.
	type selfReporting struct {
		LevelEnabler
	}
	if got := LevelOf(selfReporting{LevelEnabler: DebugLevel}); got != DebugLevel {
		t.Fatalf("LevelOf(selfReporting) = %v, want debug", got)
	}
	// Nothing enabled yields InvalidLevel.
	if got := LevelOf(LevelEnablerFunc(func(Level) bool { return false })); got != InvalidLevel {
		t.Fatalf("LevelOf(none) = %v, want InvalidLevel", got)
	}
}

type LevelEnablerFunc func(Level) bool

func (f LevelEnablerFunc) Enabled(l Level) bool { return f(l) }

func TestLevelFlagInterface(t *testing.T) {
	var lvl Level
	if err := lvl.Set("error"); err != nil {
		t.Fatal(err)
	}
	if lvl != ErrorLevel {
		t.Fatalf("Set(error) = %v", lvl)
	}
	if got := lvl.Get(); got != ErrorLevel {
		t.Fatalf("Get() = %v", got)
	}
	if err := lvl.Set("bogus"); err == nil {
		t.Fatal("Set(bogus) expected error")
	}
}

func TestInvalidLevelIsAboveMax(t *testing.T) {
	if InvalidLevel <= FatalLevel {
		t.Fatal("InvalidLevel must sort above FatalLevel")
	}
	if got := InvalidLevel.String(); got == "" {
		t.Fatal("InvalidLevel.String() must not be empty")
	}
}
