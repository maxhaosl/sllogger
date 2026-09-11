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

package sllogger

import (
	"sync"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

// LevelFlag registers on the global flag set, so the registration is guarded by
// a sync.Once to survive -count>1 without a "flag redefined" panic.
var (
	levelFlagOnce sync.Once
	levelFlagVal  *slcore.Level
)

func TestLevelFlag(t *testing.T) {
	levelFlagOnce.Do(func() {
		levelFlagVal = LevelFlag("sllogger_test_levelflag", slcore.InfoLevel, "test level")
	})
	lvl := levelFlagVal

	if lvl == nil {
		t.Fatal("LevelFlag returned nil")
	}
	if err := lvl.Set("info"); err != nil {
		t.Fatalf("Set(info) failed: %v", err)
	}
	if *lvl != slcore.InfoLevel {
		t.Fatalf("default level = %v, want Info", *lvl)
	}

	// The returned *Level satisfies flag.Value: Set/String/Get.
	if err := lvl.Set("debug"); err != nil {
		t.Fatalf("Set(debug) failed: %v", err)
	}
	if *lvl != slcore.DebugLevel {
		t.Fatalf("after Set(debug) level = %v, want Debug", *lvl)
	}
	if lvl.String() != "debug" {
		t.Fatalf("String() = %q, want debug", lvl.String())
	}

	// Invalid values are rejected.
	if err := lvl.Set("not-a-level"); err == nil {
		t.Fatal("Set(invalid) should fail")
	}

	got := lvl.Get()
	if got != slcore.DebugLevel {
		t.Fatalf("Get() = %v, want Debug", got)
	}
}
