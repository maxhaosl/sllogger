// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to anyone obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package writer

import (
	"bytes"
	"os"
	"testing"
)

// TestWriteNilFileReturnsUnavailable guards against the crash where a
// rotation failure once left w.file nil and the next Write dereferenced a nil
// *os.File. The writer must instead report ErrWriteUnavailable.
func TestWriteNilFileReturnsUnavailable(t *testing.T) {
	cfg := &Config{Dir: t.TempDir(), BaseName: "nil", MaxSize: 1 << 20}
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a broken state without panicking.
	w.file = nil
	if _, err := w.Write([]byte("hello\n")); err != ErrWriteUnavailable {
		t.Fatalf("expected ErrWriteUnavailable, got %v", err)
	}
	_ = w.Close()
}

// TestRollingWriterRotationFailureRecovers verifies that when a rotation fails
// (here, because the directory is made read-only mid-run) the writer keeps its
// previous file open instead of becoming nil, returns an error, and resumes
// writing normally once the condition is cleared.
func TestRollingWriterRotationFailureRecovers(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dir: dir, BaseName: "rot", MaxSize: 64, RotationInterval: 0}
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Fill the first file (40 bytes, under the 64-byte limit).
	if _, err := w.Write(bytes.Repeat([]byte("a"), 40)); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so creating a rotated file fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// This write exceeds the size limit and triggers a rotation that must fail.
	if _, werr := w.Write(bytes.Repeat([]byte("b"), 40)); werr == nil {
		t.Fatal("expected an error from the failed rotation")
	}
	if w.file == nil {
		t.Fatal("w.file must remain non-nil after a failed rotation")
	}

	// Restore permissions and confirm the writer recovers.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("c"), 10)); err != nil {
		t.Fatalf("write after recovery failed: %v", err)
	}
	_ = w.Close()
}
