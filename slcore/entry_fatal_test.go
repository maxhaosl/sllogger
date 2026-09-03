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
	"os"
	"os/exec"
	"testing"
)

// TestCheckWriteActionFatal 覆盖 WriteThenFatal（调用 os.Exit(1)）的分支。
//
// 由于在进程内执行会终止测试，这里用子进程方式验证退出码，
// 与 internal/exit 的测试手法一致。
func TestCheckWriteActionFatal(t *testing.T) {
	if os.Getenv("SLCORE_FATAL_SUBPROCESS") == "1" {
		WriteThenFatal.OnWrite(nil, nil)
		// 不应执行到这里。
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestCheckWriteActionFatal")
	cmd.Env = append(os.Environ(), "SLCORE_FATAL_SUBPROCESS=1")
	err := cmd.Run()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
