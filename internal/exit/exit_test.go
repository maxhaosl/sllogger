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

package exit

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// TestWithExitsWithCode spawns a subprocess so that the real os.Exit path is
// exercised without terminating the test binary.
func TestWithExitsWithCode(t *testing.T) {
	if os.Getenv("SLLOGGER_EXIT_SUBPROCESS") == "1" {
		With(3)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestWithExitsWithCode")
	cmd.Env = append(os.Environ(), "SLLOGGER_EXIT_SUBPROCESS=1")
	err := cmd.Run()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if code := exitErr.ExitCode(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	_ = strconv.Itoa
}

// TestWithZeroExitsSuccessfully verifies that exit code 0 is propagated.
func TestWithZeroExitsSuccessfully(t *testing.T) {
	if os.Getenv("SLLOGGER_EXIT_ZERO") == "1" {
		With(0)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestWithZeroExitsSuccessfully")
	cmd.Env = append(os.Environ(), "SLLOGGER_EXIT_ZERO=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected a clean exit, got %v", err)
	}
}
