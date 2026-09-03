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

package bufferpool

import (
	"sync"
	"testing"
)

func TestGetReturnsUsableBuffer(t *testing.T) {
	buf := Get()
	if buf == nil {
		t.Fatal("Get returned nil")
	}
	// A recycled buffer is reset, so a fresh Get must be empty.
	if buf.Len() != 0 {
		t.Fatalf("Len = %d, want 0", buf.Len())
	}

	buf.AppendString("payload")
	if got := buf.String(); got != "payload" {
		t.Fatalf("String() = %q, want payload", got)
	}
	buf.Free()
}

func TestGetResetsReusedBuffer(t *testing.T) {
	b1 := Get()
	b1.AppendString("leftover")
	b1.Free()

	// Whether or not the pool hands back the same object, the buffer must
	// come back empty (buffer.Pool.Get resets it).
	b2 := Get()
	if b2.Len() != 0 {
		t.Fatalf("Len = %d, want 0 after recycling", b2.Len())
	}
	b2.Free()
}

func TestFreeReturnsBufferToSharedPool(t *testing.T) {
	// Free must not panic, and subsequent Get calls must keep working.
	for i := 0; i < 100; i++ {
		buf := Get()
		buf.AppendString("x")
		buf.Free()
	}
	if buf := Get(); buf.Len() != 0 {
		t.Fatalf("Len = %d, want 0", buf.Len())
	}
}

func TestSharedPoolConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				buf := Get()
				buf.AppendString("concurrent")
				if buf.Len() != len("concurrent") {
					t.Errorf("Len = %d", buf.Len())
				}
				buf.Free()
			}
		}()
	}
	wg.Wait()
}
