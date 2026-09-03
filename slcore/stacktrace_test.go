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
	"strings"
	"testing"
)

func TestCaptureCaller(t *testing.T) {
	caller := CaptureCaller(0)
	if !caller.Defined {
		t.Fatal("caller not defined")
	}
	if !strings.HasSuffix(caller.File, "stacktrace_test.go") {
		t.Fatalf("caller file = %q, want stacktrace_test.go", caller.File)
	}
	if caller.Line == 0 {
		t.Fatal("caller line not captured")
	}
}

func TestCaptureCallerSkip(t *testing.T) {
	outer := func() EntryCaller { return CaptureCaller(1) }
	caller := outer()
	// Skipping one extra frame lands in this test function.
	if !strings.HasSuffix(caller.File, "stacktrace_test.go") {
		t.Fatalf("caller file = %q", caller.File)
	}
}

func TestCaptureStack(t *testing.T) {
	stack := CaptureStack(0)
	if stack == "" {
		t.Fatal("empty stack")
	}
	if !strings.Contains(stack, "TestCaptureStack") {
		t.Fatalf("stack does not mention the test function: %s", stack)
	}
	// Each frame is rendered as "file:line function".
	firstLine := strings.SplitN(stack, "\n", 2)[0]
	if !strings.Contains(firstLine, ":") {
		t.Fatalf("frame %q is missing file:line", firstLine)
	}
}

func TestCaptureStackSkip(t *testing.T) {
	direct := CaptureStack(0)
	skipped := CaptureStack(1)
	if skipped == "" {
		t.Fatal("empty stack after skipping")
	}
	// Skipping frames must produce a strictly shorter stack.
	if len(skipped) >= len(direct) {
		t.Fatalf("skip did not drop frames: %d >= %d", len(skipped), len(direct))
	}
}
