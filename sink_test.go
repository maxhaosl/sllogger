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
	"io"
	"net/url"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

func TestRegisterSink(t *testing.T) {
	var opened []string
	err := RegisterSink("mem", func(u *url.URL) (Sink, error) {
		opened = append(opened, u.String())
		return nopCloserSink{slcore.AddSync(io.Discard)}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate registration is rejected.
	if err := RegisterSink("mem", func(*url.URL) (Sink, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("duplicate sink registration should fail")
	}

	// Empty scheme is rejected.
	if err := RegisterSink("", func(*url.URL) (Sink, error) { return nil, nil }); err == nil {
		t.Fatal("empty scheme should be rejected")
	}

	// Schemes must start with a letter and use RFC 3986 characters only.
	if err := RegisterSink("9bad", func(*url.URL) (Sink, error) { return nil, nil }); err == nil {
		t.Fatal("scheme starting with a digit should be rejected")
	}

	// Open falls back to the sink registry for unrecognised paths.
	ws, closeFn, err := Open("mem://buffer")
	if err != nil {
		t.Fatalf("Open with custom scheme failed: %v", err)
	}
	defer closeFn()
	if ws == nil {
		t.Fatal("Open returned nil WriteSyncer")
	}
	if len(opened) != 1 {
		t.Fatalf("sink factory should be called once, got %d", len(opened))
	}

	// An unknown scheme that is neither a file nor registered fails.
	if _, _, err := Open("ghost://nope"); err == nil {
		t.Fatal("opening an unknown scheme should fail")
	}
}

func TestNewSinkBuiltinFile(t *testing.T) {
	// Absolute paths are opened through the built-in file sink.
	ws, closeFn, err := Open("stdout")
	if err != nil {
		t.Fatalf("Open(stdout) failed: %v", err)
	}
	defer closeFn()
	if ws == nil {
		t.Fatal("Open(stdout) returned nil")
	}
}
