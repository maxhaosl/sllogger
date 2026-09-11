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
	"sync"
	"testing"
	"time"
)

func TestNewLazyWithDefersUntilEnabled(t *testing.T) {
	base := newSpyCore(DebugLevel)
	lazy := NewLazyWith(base, []Field{String("k", "v")})
	if len(base.rec.fields) != 0 {
		t.Fatalf("With must not be called before first successful Check; got %d", len(base.rec.fields))
	}

	// An enabled Check triggers the deferred With exactly once.
	ce := lazy.Check(Entry{Level: InfoLevel, Message: "m", Time: time.Now()}, nil)
	if ce == nil {
		t.Fatal("InfoLevel should be enabled")
	}
	if len(base.rec.fields) != 1 {
		t.Fatalf("With should be called after an enabled Check; got %d", len(base.rec.fields))
	}
	if len(base.rec.fields[0]) != 1 {
		t.Fatal("With should receive the lazy fields")
	}
}

func TestNewLazyWithDefersOnDisabled(t *testing.T) {
	base := newSpyCore(WarnLevel)
	lazy := NewLazyWith(base, []Field{String("k", "v")})
	ce := lazy.Check(Entry{Level: InfoLevel, Message: "m", Time: time.Now()}, nil)
	if ce != nil {
		t.Fatal("InfoLevel should be disabled, Check must return nil")
	}
	if len(base.rec.fields) != 0 {
		t.Fatalf("With must not be called for a disabled level; got %d", len(base.rec.fields))
	}
}

func TestNewLazyWithWrite(t *testing.T) {
	base := newSpyCore(DebugLevel)
	lazy := NewLazyWith(base, []Field{String("k", "v")})
	lazy.Write(Entry{Level: InfoLevel, Message: "m", Time: time.Now()}, nil)
	if len(base.rec.fields) != 1 {
		t.Fatalf("Write should trigger the deferred With; got %d", len(base.rec.fields))
	}
	if len(base.rec.writes) != 1 {
		t.Fatalf("expected 1 write to base; got %d", len(base.rec.writes))
	}
}

func TestNewLazyWithConcurrent(t *testing.T) {
	base := newSpyCore(DebugLevel)
	lazy := NewLazyWith(base, []Field{String("k", "v")})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ce := lazy.Check(Entry{Level: InfoLevel, Message: "m", Time: time.Now()}, nil)
			if ce != nil {
				ce.Write()
			}
		}()
	}
	wg.Wait()
	if len(base.rec.fields) != 1 {
		t.Fatalf("With should be called exactly once across goroutines; got %d", len(base.rec.fields))
	}
}
