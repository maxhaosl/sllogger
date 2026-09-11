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

// Tests derived from go.uber.org/zap/field_test.go.
package sllogger

import (
	"errors"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

func TestFieldConstructors(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  slcore.FieldType
	}{
		{"binary", Binary("k", []byte("v")), slcore.BinaryType},
		{"bool", Bool("k", true), slcore.BoolType},
		{"bytestring", ByteString("k", []byte("v")), slcore.ByteStringType},
		{"complex128", Complex128("k", 1i), slcore.Complex128Type},
		{"complex64", Complex64("k", 1i), slcore.Complex64Type},
		{"float64", Float64("k", 1), slcore.Float64Type},
		{"float32", Float32("k", 1), slcore.Float32Type},
		{"int", Int("k", 1), slcore.Int64Type},
		{"int64", Int64("k", 1), slcore.Int64Type},
		{"int32", Int32("k", 1), slcore.Int32Type},
		{"int16", Int16("k", 1), slcore.Int16Type},
		{"int8", Int8("k", 1), slcore.Int8Type},
		{"string", String("k", "v"), slcore.StringType},
		{"uint", Uint("k", 1), slcore.Uint64Type},
		{"uint64", Uint64("k", 1), slcore.Uint64Type},
		{"uint32", Uint32("k", 1), slcore.Uint32Type},
		{"uint16", Uint16("k", 1), slcore.Uint16Type},
		{"uint8", Uint8("k", 1), slcore.Uint8Type},
		{"uintptr", Uintptr("k", 1), slcore.UintptrType},
		{"reflect", Reflect("k", 1), slcore.ReflectType},
		{"namespace", Namespace("k"), slcore.NamespaceType},
		{"time", Time("k", time.Now()), slcore.TimeFullType},
		{"duration", Duration("k", time.Second), slcore.DurationType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field.Type != tt.want {
				t.Fatalf("type = %v, want %v", tt.field.Type, tt.want)
			}
			if tt.field.Key != "k" {
				t.Fatalf("key = %q", tt.field.Key)
			}
		})
	}
}

func TestSkipFieldHasNoKey(t *testing.T) {
	// Skip is a no-op field: it carries neither key nor value.
	f := Skip()
	if f.Type != slcore.SkipType {
		t.Fatalf("type = %v", f.Type)
	}
	if f.Key != "" {
		t.Fatalf("key = %q, want empty", f.Key)
	}
}

func TestBoolFieldValue(t *testing.T) {
	if Bool("k", true).Integer != 1 {
		t.Fatal("true must be encoded as 1")
	}
	if Bool("k", false).Integer != 0 {
		t.Fatal("false must be encoded as 0")
	}
}

func TestErrorField(t *testing.T) {
	err := errors.New("boom")
	f := Error(err)
	if f.Type != slcore.ErrorType {
		t.Fatalf("type = %v", f.Type)
	}
	if f.Key != "error" {
		t.Fatalf("key = %q", f.Key)
	}

	// A nil error is a no-op field.
	if got := Error(nil).Type; got != slcore.SkipType {
		t.Fatalf("nil error type = %v, want Skip", got)
	}
	if got := NamedError("k", nil).Type; got != slcore.SkipType {
		t.Fatalf("nil named error type = %v", got)
	}
	if got := NamedError("k", err).Key; got != "k" {
		t.Fatalf("key = %q", got)
	}
}

// stringerValue implements fmt.Stringer only, so Any must pick
// StringerType for it.
type stringerValue struct{}

func (stringerValue) String() string { return "stringer" }

type testObject struct{ value string }

func (t testObject) MarshalLogObject(enc slcore.ObjectEncoder) error {
	enc.AddString("value", t.value)
	return nil
}

type testArray struct{}

func (testArray) MarshalLogArray(enc slcore.ArrayEncoder) error {
	enc.AppendString("item")
	return nil
}

func TestAny(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  slcore.FieldType
	}{
		{"bool", true, slcore.BoolType},
		{"bytes", []byte("x"), slcore.BinaryType},
		{"complex128", complex128(1), slcore.Complex128Type},
		{"complex64", complex64(1), slcore.Complex64Type},
		{"error", errors.New("x"), slcore.ErrorType},
		{"float32", float32(1), slcore.Float32Type},
		{"float64", float64(1), slcore.Float64Type},
		{"int", int(1), slcore.Int64Type},
		{"int8", int8(1), slcore.Int8Type},
		{"int16", int16(1), slcore.Int16Type},
		{"int32", int32(1), slcore.Int32Type},
		{"int64", int64(1), slcore.Int64Type},
		{"string", "x", slcore.StringType},
		{"uint", uint(1), slcore.Uint64Type},
		{"uint8", uint8(1), slcore.Uint8Type},
		{"uint16", uint16(1), slcore.Uint16Type},
		{"uint32", uint32(1), slcore.Uint32Type},
		{"uint64", uint64(1), slcore.Uint64Type},
		{"uintptr", uintptr(1), slcore.UintptrType},
		{"time", time.Now(), slcore.TimeFullType},
		{"duration", time.Second, slcore.DurationType},
		{"stringer", stringerValue{}, slcore.StringerType},
		{"object", testObject{}, slcore.ObjectMarshalerType},
		{"array", testArray{}, slcore.ArrayMarshalerType},
		{"nil", nil, slcore.ReflectType},
		{"struct", struct{ A int }{1}, slcore.ReflectType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Any("k", tt.value).Type; got != tt.want {
				t.Fatalf("type = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArrayAndObjectConstructors(t *testing.T) {
	if got := Array("k", testArray{}).Type; got != slcore.ArrayMarshalerType {
		t.Fatalf("type = %v", got)
	}
	if got := Object("k", testObject{}).Type; got != slcore.ObjectMarshalerType {
		t.Fatalf("type = %v", got)
	}
}

func TestFieldRoundTrip(t *testing.T) {
	// Fields must survive an encode round trip with their values intact.
	log, ws := testLogger(t)
	log.Info("fields",
		String("s", "v"),
		Int("i", 7),
		Bool("b", true),
		Duration("d", 2*time.Second),
	)
	line := ws.lines()[0]
	for _, want := range []string{`"s":"v"`, `"i":7`, `"b":true`, `"d":2000`} {
		if !contains(line, want) {
			t.Errorf("output %s missing %s", line, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOfSub(s, sub) >= 0)
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
