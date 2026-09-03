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

// Tests derived from go.uber.org/zap/zapcore/field_test.go.
package slcore

import (
	"errors"
	"math"
	"testing"
	"time"
)

type stringerFails struct{}

func (stringerFails) String() string { panic("stringer panic") }

type stringerOK struct{}

func (stringerOK) String() string { return "ok" }

type nilStringer struct{}

func (*nilStringer) String() string { panic("nil receiver") }

func TestFieldAddToScalarTypes(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  interface{}
	}{
		{"string", Field{Key: "k", Type: StringType, String: "v"}, "v"},
		{"bool true", Field{Key: "k", Type: BoolType, Integer: 1}, true},
		{"bool false", Field{Key: "k", Type: BoolType, Integer: 0}, false},
		{"int64", Field{Key: "k", Type: Int64Type, Integer: 42}, int64(42)},
		{"int32", Field{Key: "k", Type: Int32Type, Integer: 42}, int32(42)},
		{"int16", Field{Key: "k", Type: Int16Type, Integer: 42}, int16(42)},
		{"int8", Field{Key: "k", Type: Int8Type, Integer: 42}, int8(42)},
		{"uint64", Field{Key: "k", Type: Uint64Type, Integer: 42}, uint64(42)},
		{"uint32", Field{Key: "k", Type: Uint32Type, Integer: 42}, uint32(42)},
		{"uint16", Field{Key: "k", Type: Uint16Type, Integer: 42}, uint16(42)},
		{"uint8", Field{Key: "k", Type: Uint8Type, Integer: 42}, uint8(42)},
		{"duration", Field{Key: "k", Type: DurationType, Integer: int64(time.Second)}, int64(1000)},
		{"float64", Field{Key: "k", Type: Float64Type, Integer: int64(math.Float64bits(1.5))}, 1.5},
		{"float32", Field{Key: "k", Type: Float32Type, Integer: int64(math.Float32bits(1.5))}, float32(1.5)},
		{"binary", Field{Key: "k", Type: BinaryType, Interface: []byte("abc")}, "abc"},
		{"bytestring", Field{Key: "k", Type: ByteStringType, Interface: []byte("def")}, "def"},
		{"stringer", Field{Key: "k", Type: StringerType, Interface: stringerOK{}}, "ok"},
		{"error", Field{Key: "k", Type: ErrorType, Interface: errors.New("boom")}, "boom"},
		{"reflected", Field{Key: "k", Type: ReflectType, Interface: 12}, 12},
		{"time full", Field{Key: "k", Type: TimeFullType, Interface: time.Unix(0, 0)}, time.Unix(0, 0).UnixNano()},
		{"uintptr", Field{Key: "k", Type: UintptrType, Integer: 7}, uint64(7)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := newMapObjectEncoder()
			tt.field.AddTo(enc)
			got, ok := enc["k"]
			if !ok {
				t.Fatalf("field %q not encoded", tt.field.Key)
			}
			if got != tt.want {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestFieldAddToComplexTypes(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "c", Type: Complex128Type, Interface: 1 + 2i}.AddTo(enc)
	if enc["c"] != float64(1) {
		t.Fatalf("complex128 = %v", enc["c"])
	}
	enc2 := newMapObjectEncoder()
	Field{Key: "c", Type: Complex64Type, Interface: complex64(1 + 2i)}.AddTo(enc2)
	if enc2["c"] != float32(1) {
		t.Fatalf("complex64 = %v (%T), want float32(1)", enc2["c"], enc2["c"])
	}
}

func TestFieldAddToTimeType(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "t", Type: TimeType, Integer: time.Unix(5, 0).UnixNano(), Interface: time.UTC}.AddTo(enc)
	if enc["t"] != time.Unix(5, 0).UnixNano() {
		t.Fatalf("time = %v", enc["t"])
	}
	// A nil location must fall back to UTC instead of panicking.
	enc2 := newMapObjectEncoder()
	Field{Key: "t", Type: TimeType, Integer: time.Unix(5, 0).UnixNano()}.AddTo(enc2)
	if enc2["t"] != time.Unix(5, 0).UnixNano() {
		t.Fatalf("time (nil loc) = %v", enc2["t"])
	}
}

func TestFieldAddToNamespace(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "ns", Type: NamespaceType}.AddTo(enc)
	if _, ok := enc["ns.ns"]; !ok {
		t.Fatal("namespace not opened")
	}
}

func TestFieldAddToSkipIsNoop(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "s", Type: SkipType}.AddTo(enc)
	if len(enc) != 0 {
		t.Fatalf("SkipType wrote fields: %v", enc)
	}
}

func TestFieldAddToUnknownTypePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown field type")
		}
	}()
	Field{Key: "k", Type: FieldType(250)}.AddTo(newMapObjectEncoder())
}

func TestFieldAddToStringerPanic(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "k", Type: StringerType, Interface: stringerFails{}}.AddTo(enc)
	// The panic must be captured and surfaced as a kError field.
	if _, ok := enc["kError"]; !ok {
		t.Fatalf("panic not captured: %v", enc)
	}
}

func TestFieldAddToNilStringerPanic(t *testing.T) {
	enc := newMapObjectEncoder()
	Field{Key: "k", Type: StringerType, Interface: (*nilStringer)(nil)}.AddTo(enc)
	if enc["k"] != "<nil>" {
		t.Fatalf("nil stringer = %v, want <nil>", enc["k"])
	}
}

// A nil-pointer error whose Error() method panics must be rendered as <nil>
// instead of taking the process down.
type panicErrorPtr struct{}

func (*panicErrorPtr) Error() string { panic("nil receiver panic") }

func TestFieldAddToErrorNilReceiverPanic(t *testing.T) {
	enc := newMapObjectEncoder()
	var err error = (*panicErrorPtr)(nil)
	Field{Key: "k", Type: ErrorType, Interface: err}.AddTo(enc)
	if enc["k"] != "<nil>" {
		t.Fatalf("k = %v, want <nil>", enc["k"])
	}
}

func TestFieldEquals(t *testing.T) {
	tests := []struct {
		name string
		a, b Field
		want bool
	}{
		{"same string", Field{Key: "k", Type: StringType, String: "v"},
			Field{Key: "k", Type: StringType, String: "v"}, true},
		{"different key", Field{Key: "a", Type: StringType}, Field{Key: "b", Type: StringType}, false},
		{"different type", Field{Key: "k", Type: StringType}, Field{Key: "k", Type: BoolType}, false},
		{"different value", Field{Key: "k", Type: StringType, String: "a"},
			Field{Key: "k", Type: StringType, String: "b"}, false},
		{"binary equal", Field{Key: "k", Type: BinaryType, Interface: []byte("ab")},
			Field{Key: "k", Type: BinaryType, Interface: []byte("ab")}, true},
		{"binary differ", Field{Key: "k", Type: BinaryType, Interface: []byte("ab")},
			Field{Key: "k", Type: BinaryType, Interface: []byte("cd")}, false},
		{"error deep equal", Field{Key: "k", Type: ErrorType, Interface: errors.New("x")},
			Field{Key: "k", Type: ErrorType, Interface: errors.New("x")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equals(tt.b); got != tt.want {
				t.Errorf("Equals = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddFields(t *testing.T) {
	enc := newMapObjectEncoder()
	addFields(enc, []Field{
		{Key: "a", Type: StringType, String: "1"},
		{Key: "b", Type: StringType, String: "2"},
	})
	if len(enc) != 2 {
		t.Fatalf("encoded %d fields, want 2", len(enc))
	}
}
