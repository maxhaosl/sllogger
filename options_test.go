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
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

func TestIncreaseLevelOption(t *testing.T) {
	log, ws := testLogger(t)
	// testLogger is at DebugLevel; raise to WarnLevel.
	log = log.WithOptions(IncreaseLevel(slcore.WarnLevel))

	log.Debug("dropped")
	log.Info("dropped")
	log.Warn("kept")
	log.Error("kept")

	lines := ws.lines()
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines after raising level, got %d: %v", len(lines), lines)
	}
}

func TestIncreaseLevelDoesNotLower(t *testing.T) {
	log, ws := testLogger(t) // DebugLevel
	// Attempting to "lower" to DebugLevel is a no-op (already at DebugLevel).
	log = log.WithOptions(IncreaseLevel(slcore.DebugLevel))
	log.Info("kept")
	if len(ws.lines()) != 1 {
		t.Fatalf("expected 1 line, got %d", len(ws.lines()))
	}
}
