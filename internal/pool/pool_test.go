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

package pool

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestPoolNewAndGet(t *testing.T) {
	var created atomic.Int32
	p := New(func() *int {
		created.Add(1)
		v := new(int)
		return v
	})

	v := p.Get()
	if v == nil {
		t.Fatal("Get returned nil")
	}
	if created.Load() != 1 {
		t.Fatalf("factory called %d times, want 1", created.Load())
	}

	p.Put(v)

	// sync.Pool is a best-effort cache: it may reuse the pooled object, but it
	// is also allowed to drop it (e.g. on GC) and call the factory again. So
	// only assert that Get keeps returning usable objects.
	again := p.Get()
	if again == nil {
		t.Fatal("Get returned nil after Put")
	}
	if created.Load() < 1 {
		t.Fatal("factory was never called")
	}
}

func TestPoolValueRoundTrip(t *testing.T) {
	p := New(func() *string { s := ""; return &s })

	s := p.Get()
	*s = "payload"
	// The value must be readable while we hold the reference.
	if *s != "payload" {
		t.Fatalf("value = %q, want payload", *s)
	}
	p.Put(s)

	// After Put, the pooled value is either the same object (value kept) or a
	// freshly built one (empty). Either is valid sync.Pool behaviour.
	got := p.Get()
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if *got != "" && *got != "payload" {
		t.Fatalf("value = %q, want either empty or payload", *got)
	}
}

func TestPoolWithStructType(t *testing.T) {
	type item struct {
		n int
		s string
	}
	p := New(func() *item { return &item{} })

	it := p.Get()
	it.n, it.s = 7, "seven"
	if it.n != 7 || it.s != "seven" {
		t.Fatalf("got %+v, want {7 seven}", *it)
	}
	p.Put(it)

	// Like the round-trip test: the object may or may not be reused.
	got := p.Get()
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if (got.n != 0 && got.n != 7) || (got.s != "" && got.s != "seven") {
		t.Fatalf("got %+v, want either the zero value or {7 seven}", *got)
	}
}

func TestPoolConcurrent(t *testing.T) {
	p := New(func() *int { v := 0; return &v })

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				v := p.Get()
				*v = j
				p.Put(v)
			}
		}()
	}
	wg.Wait()
}
