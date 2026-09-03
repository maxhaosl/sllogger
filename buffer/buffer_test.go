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

// Tests derived from go.uber.org/zap/buffer.
package buffer

import (
	"testing"
	"time"
)

func TestBufferAppend(t *testing.T) {
	tests := []struct {
		name string
		do   func(*Buffer)
		want string
	}{
		{"byte", func(b *Buffer) { b.AppendByte('a') }, "a"},
		{"bytes", func(b *Buffer) { b.AppendBytes([]byte("bc")) }, "bc"},
		{"string", func(b *Buffer) { b.AppendString("de") }, "de"},
		{"int", func(b *Buffer) { b.AppendInt(42) }, "42"},
		{"negative int", func(b *Buffer) { b.AppendInt(-7) }, "-7"},
		{"uint", func(b *Buffer) { b.AppendUint(9) }, "9"},
		{"bool true", func(b *Buffer) { b.AppendBool(true) }, "true"},
		{"bool false", func(b *Buffer) { b.AppendBool(false) }, "false"},
		{"float", func(b *Buffer) { b.AppendFloat(1.5, 64) }, "1.5"},
		{"float32", func(b *Buffer) { b.AppendFloat(float64(float32(2.5)), 32) }, "2.5"},
		{"time", func(b *Buffer) {
			b.AppendTime(time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC), "2006-01-02")
		}, "2026-09-03"},
		{"chained", func(b *Buffer) {
			b.AppendString("a=")
			b.AppendInt(1)
			b.AppendByte('|')
			b.AppendBool(true)
		}, "a=1|true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewPool().Get()
			defer buf.Free()
			tt.do(buf)
			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBufferWriters(t *testing.T) {
	buf := NewPool().Get()
	defer buf.Free()

	n, err := buf.Write([]byte("hello"))
	if n != 5 || err != nil {
		t.Fatalf("Write = (%d, %v)", n, err)
	}
	if err := buf.WriteByte(' '); err != nil {
		t.Fatal(err)
	}
	n, err = buf.WriteString("world")
	if n != 5 || err != nil {
		t.Fatalf("WriteString = (%d, %v)", n, err)
	}
	if got, want := buf.String(), "hello world"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBufferLenCapBytes(t *testing.T) {
	buf := NewPool().Get()
	defer buf.Free()

	if buf.Len() != 0 {
		t.Fatalf("Len of fresh buffer = %d", buf.Len())
	}
	buf.AppendString("abc")
	if buf.Len() != 3 {
		t.Fatalf("Len = %d", buf.Len())
	}
	if buf.Cap() < 3 {
		t.Fatalf("Cap = %d", buf.Cap())
	}
	if got := string(buf.Bytes()); got != "abc" {
		t.Fatalf("Bytes = %q", got)
	}
}

func TestBufferReset(t *testing.T) {
	buf := NewPool().Get()
	defer buf.Free()

	buf.AppendString("content")
	capBefore := buf.Cap()
	buf.Reset()
	if buf.Len() != 0 {
		t.Fatalf("Len after Reset = %d", buf.Len())
	}
	// Reset must retain the backing array to allow reuse without allocation.
	if buf.Cap() != capBefore {
		t.Fatalf("Cap changed: %d -> %d", capBefore, buf.Cap())
	}
	buf.AppendString("x")
	if got := buf.String(); got != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestBufferTrimNewline(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"a", "a"},
		{"a\n", "a"},
		{"a\nb\n", "a\nb"},
		{"\n", ""},
		{"a\nb", "a\nb"},
	}
	for _, tt := range tests {
		buf := NewPool().Get()
		buf.AppendString(tt.in)
		buf.TrimNewline()
		if got := buf.String(); got != tt.want {
			t.Errorf("TrimNewline(%q) = %q, want %q", tt.in, got, tt.want)
		}
		buf.Free()
	}
}

func TestPoolReuse(t *testing.T) {
	pool := NewPool()

	b1 := pool.Get()
	b1.AppendString("discarded")
	cap1 := b1.Cap()
	b1.Free()

	b2 := pool.Get()
	if b2.Len() != 0 {
		t.Fatalf("recycled buffer is not reset: %q", b2.String())
	}
	if b2.Cap() != cap1 {
		// Not a hard requirement of the API, but verifies pooling works.
		t.Logf("capacity changed between Get calls: %d -> %d", cap1, b2.Cap())
	}
	b2.Free()
}

func TestPoolFreshBufferCapacity(t *testing.T) {
	buf := NewPool().Get()
	defer buf.Free()
	if buf.Cap() < _size {
		t.Fatalf("fresh buffer cap = %d, want >= %d", buf.Cap(), _size)
	}
}
