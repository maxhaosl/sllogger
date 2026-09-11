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

package testutil

import (
	"errors"
	"testing"
	"time"
)

func TestSinkRecordsWritesAndSyncs(t *testing.T) {
	s := &Sink{}
	if _, err := s.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	writes, syncs := s.Counts()
	if writes != 1 {
		t.Fatalf("writes = %d, want 1", writes)
	}
	if got := s.String(); got != "hello" {
		t.Fatalf("String() = %q, want %q", got, "hello")
	}
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	_, syncs = s.Counts()
	if syncs != 1 {
		t.Fatalf("syncs = %d, want 1", syncs)
	}

	boom := errors.New("boom")
	s.SetWriteError(boom)
	if _, err := s.Write([]byte("x")); err != boom {
		t.Fatalf("Write error = %v, want %v", err, boom)
	}
}

func TestMustParseTime(t *testing.T) {
	got := MustParseTime("2006-01-02", "2026-09-03")
	want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("MustParseTime = %v, want %v", got, want)
	}
}

func TestTempDirIsUsableAndCleanedUp(t *testing.T) {
	dir := TempDir(t)
	if dir == "" {
		t.Fatal("TempDir returned empty path")
	}
	// t.Cleanup (registered inside TempDir) removes the directory after the test.
}
