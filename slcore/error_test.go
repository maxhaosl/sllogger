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

// Tests derived from go.uber.org/zap/zapcore/error_test.go.
package slcore

import (
	"errors"
	"fmt"
	"testing"
)

type verboseError struct{ msg string }

func (e verboseError) Error() string { return e.msg }
func (e verboseError) Format(s fmt.State, verb rune) {
	if verb == 'v' && s.Flag('+') {
		fmt.Fprintf(s, "%s: verbose detail", e.msg)
		return
	}
	fmt.Fprint(s, e.msg)
}

type panicError struct{}

func (panicError) Error() string { panic("error panic") }

type nilPanicError struct{}

func (*nilPanicError) Error() string { panic("nil receiver panic") }

func TestEncodeError(t *testing.T) {
	enc := newMapObjectEncoder()
	if err := encodeError("k", errors.New("boom"), enc); err != nil {
		t.Fatal(err)
	}
	if enc["k"] != "boom" {
		t.Fatalf("k = %v", enc["k"])
	}
}

func TestEncodeErrorVerbose(t *testing.T) {
	enc := newMapObjectEncoder()
	if err := encodeError("k", verboseError{"boom"}, enc); err != nil {
		t.Fatal(err)
	}
	if enc["k"] != "boom" {
		t.Fatalf("k = %v", enc["k"])
	}
	if enc["kVerbose"] != "boom: verbose detail" {
		t.Fatalf("kVerbose = %v", enc["kVerbose"])
	}
}

func TestEncodeErrorNonVerboseFormatter(t *testing.T) {
	// A fmt.Formatter whose verbose output equals the plain output must not
	// produce a redundant Verbose field.
	enc := newMapObjectEncoder()
	if err := encodeError("k", plainFormatter{}, enc); err != nil {
		t.Fatal(err)
	}
	if _, ok := enc["kVerbose"]; ok {
		t.Fatal("redundant Verbose field")
	}
}

type plainFormatter struct{}

func (plainFormatter) Error() string { return "plain" }
func (plainFormatter) Format(s fmt.State, verb rune) {
	fmt.Fprint(s, "plain")
}

func TestEncodeErrorPanicRecovered(t *testing.T) {
	enc := newMapObjectEncoder()
	err := encodeError("k", panicError{}, enc)
	if err == nil {
		t.Fatal("expected the panic to be converted into an error")
	}
}

func TestEncodeErrorNilReceiverPanic(t *testing.T) {
	enc := newMapObjectEncoder()
	if err := encodeError("k", (*nilPanicError)(nil), enc); err != nil {
		t.Fatalf("nil receiver must be rendered as <nil>, got err %v", err)
	}
	if enc["k"] != "<nil>" {
		t.Fatalf("k = %v, want <nil>", enc["k"])
	}
}

func TestAppendErrors(t *testing.T) {
	if got := appendErrors(nil); got != nil {
		t.Fatalf("appendErrors(nil) = %v", got)
	}
	e1 := errors.New("e1")
	e2 := errors.New("e2")
	got := appendErrors(e1, e2)
	if !errors.Is(got, e1) || !errors.Is(got, e2) {
		t.Fatalf("joined error lost components: %v", got)
	}
	// errors.Join always wraps, even for a single non-nil error, but the
	// components stay reachable with errors.Is.
	if got := appendErrors(nil, e1); !errors.Is(got, e1) {
		t.Fatalf("appendErrors(nil, e1) = %v, want e1", got)
	}
}
