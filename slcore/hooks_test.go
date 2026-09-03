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

// Tests derived from go.uber.org/zap/zapcore/hook_test.go.
package slcore

import (
	"testing"
)

func TestRegisterHooks(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)

	calls := 0
	hooked := RegisterHooks(core, func(Entry) error {
		calls++
		return nil
	})

	ce := hooked.Check(Entry{Level: InfoLevel}, nil)
	if ce == nil {
		t.Fatal("Check returned nil")
	}
	ce.Write()

	if calls != 1 {
		t.Fatalf("hook called %d times, want 1", calls)
	}
	if writes, _ := ws.counts(); writes != 1 {
		t.Fatalf("writes = %d, want 1", writes)
	}
}

func TestRegisterHooksMultiple(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, DebugLevel)

	order := make([]int, 0, 2)
	hooked := RegisterHooks(core,
		func(Entry) error { order = append(order, 1); return nil },
		func(Entry) error { order = append(order, 2); return nil },
	)
	if ce := hooked.Check(Entry{Level: InfoLevel}, nil); ce != nil {
		ce.Write()
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("hooks ran in order %v, want [1 2]", order)
	}
}

func TestRegisterHooksErrorsDoNotBlockWrite(t *testing.T) {
	ws := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("x"), ws, DebugLevel)

	hooked := RegisterHooks(core, func(Entry) error { return errStub })
	if ce := hooked.Check(Entry{Level: InfoLevel}, nil); ce != nil {
		ce.Write()
	}
	if writes, _ := ws.counts(); writes != 1 {
		t.Fatalf("writes = %d, want the entry to be written despite the hook error", writes)
	}
}

func TestRegisterHooksDisabledLevel(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, ErrorLevel)
	calls := 0
	hooked := RegisterHooks(core, func(Entry) error { calls++; return nil })
	if ce := hooked.Check(Entry{Level: DebugLevel}, nil); ce != nil {
		ce.Write()
	}
	if calls != 0 {
		t.Fatalf("hook called %d times for a disabled level", calls)
	}
}

func TestRegisterHooksNilCore(t *testing.T) {
	if got := RegisterHooks(nil, func(Entry) error { return nil }); got != nil {
		t.Fatal("RegisterHooks(nil) must return nil")
	}
}

func TestHookCoreWith(t *testing.T) {
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, DebugLevel)
	calls := 0
	hooked := RegisterHooks(core, func(Entry) error { calls++; return nil })

	child := hooked.With([]Field{String("k", "v")})
	if _, ok := child.(hookCore); !ok {
		t.Fatal("With must preserve hooks")
	}
	if ce := child.Check(Entry{Level: InfoLevel}, nil); ce != nil {
		ce.Write()
	}
	if calls != 1 {
		t.Fatalf("hook called %d times on the child core", calls)
	}
}

func TestHookCoreNoHooks(t *testing.T) {
	// Registering zero hooks must still produce a usable core.
	core := NewCore(newStubEncoder("x"), &countingWriteSyncer{}, DebugLevel)
	hooked := RegisterHooks(core)
	if ce := hooked.Check(Entry{Level: InfoLevel}, nil); ce != nil {
		ce.Write()
	}
}
