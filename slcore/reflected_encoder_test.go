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
	"bytes"
	"testing"
)

func TestReflectedEncoderMap(t *testing.T) {
	var buf bytes.Buffer
	enc := defaultReflectedEncoder(&buf)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	want := `{"a":1}` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("reflected encoder = %q, want %q", got, want)
	}
}

func TestReflectedEncoderStruct(t *testing.T) {
	var buf bytes.Buffer
	enc := defaultReflectedEncoder(&buf)
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := enc.Encode(point{X: 1, Y: 2}); err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	want := `{"x":1,"y":2}` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("reflected encoder = %q, want %q", got, want)
	}
}
