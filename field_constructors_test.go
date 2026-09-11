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
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

// logFieldLine logs a single field on an isolated logger and returns the one
// produced line.
func logFieldLine(t *testing.T, f Field) string {
	t.Helper()
	log, ws := testLogger(t)
	log.Info("msg", f)
	lines := ws.lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	return lines[0]
}

// logFieldJSON logs a single field and decodes the produced line into a generic
// map for inspection.
func logFieldJSON(t *testing.T, f Field) map[string]json.RawMessage {
	t.Helper()
	line := logFieldLine(t, f)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("line %q is not valid JSON: %v", line, err)
	}
	return m
}

// fieldRaw returns the raw JSON value under f.Key.
func fieldRaw(t *testing.T, f Field) json.RawMessage {
	t.Helper()
	m := logFieldJSON(t, f)
	raw, ok := m[f.Key]
	if !ok {
		t.Fatalf("field key %q missing from line %v", f.Key, m)
	}
	return raw
}

// decode unmarshals a raw JSON value into interface{}.
func decode(t *testing.T, raw json.RawMessage) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

type arrObj struct{ name string }

func (o arrObj) MarshalLogObject(e slcore.ObjectEncoder) error {
	e.AddString("name", o.name)
	return nil
}

type arrPtrObj struct{ name string }

func (o *arrPtrObj) MarshalLogObject(e slcore.ObjectEncoder) error {
	e.AddString("name", o.name)
	return nil
}

type myStr string

func (s myStr) String() string { return string(s) }

func TestPointerFields(t *testing.T) {
	b := true
	bi := 42
	bs := "hello"
	bd := 1.5
	bdt := time.Minute
	bt := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	bc128 := complex128(1 + 2i)
	bc64 := complex64(1 + 2i)
	bf32 := float32(1.5)
	bi64 := int64(7)
	bi32 := int32(7)
	bi16 := int16(7)
	bi8 := int8(7)
	bu := uint(7)
	bu64 := uint64(7)
	bu32 := uint32(7)
	bu16 := uint16(7)
	bu8 := uint8(7)
	bup := uintptr(7)

	cases := []struct {
		name  string
		field Field
		want  interface{}
	}{
		{"Boolp", Boolp("k", &b), true},
		{"Complex128p", Complex128p("k", &bc128), 1.0},
		{"Complex64p", Complex64p("k", &bc64), 1.0},
		{"Durationp", Durationp("k", &bdt), float64(60000)},
		{"Float64p", Float64p("k", &bd), 1.5},
		{"Float32p", Float32p("k", &bf32), 1.5},
		{"Intp", Intp("k", &bi), float64(42)},
		{"Int64p", Int64p("k", &bi64), float64(7)},
		{"Int32p", Int32p("k", &bi32), float64(7)},
		{"Int16p", Int16p("k", &bi16), float64(7)},
		{"Int8p", Int8p("k", &bi8), float64(7)},
		{"Stringp", Stringp("k", &bs), "hello"},
		{"Uintp", Uintp("k", &bu), float64(7)},
		{"Uint64p", Uint64p("k", &bu64), float64(7)},
		{"Uint32p", Uint32p("k", &bu32), float64(7)},
		{"Uint16p", Uint16p("k", &bu16), float64(7)},
		{"Uint8p", Uint8p("k", &bu8), float64(7)},
		{"Uintptrp", Uintptrp("k", &bup), float64(7)},
		{"Timep", Timep("k", &bt), bt.Format(time.RFC3339Nano)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := fieldRaw(t, c.field)
			if got := decode(t, raw); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v, want %#v (raw %s)", got, c.want, raw)
			}
		})
	}
}

func TestByteStringField(t *testing.T) {
	raw := fieldRaw(t, ByteString("k", []byte("x")))
	if string(raw) != `"x"` {
		t.Fatalf("ByteString should encode as string, got %s", raw)
	}
}

func TestNilPointerField(t *testing.T) {
	for _, f := range []Field{
		Boolp("k", nil),
		Intp("k", nil),
		Stringp("k", nil),
		Float64p("k", nil),
		Timep("k", nil),
	} {
		raw := fieldRaw(t, f)
		if string(raw) != "null" {
			t.Fatalf("nil pointer %s should encode as null, got %s", f.Key, raw)
		}
	}
}

func TestStackField(t *testing.T) {
	line := logFieldLine(t, Stack("stack"))
	if !strings.Contains(line, `"stack":"`) {
		t.Fatalf("stack field missing or empty: %s", line)
	}
	if !strings.Contains(line, "sllogger") && !strings.Contains(line, ".go") {
		t.Fatalf("stack trace does not look like a real trace: %s", line)
	}
}

func TestInlineField(t *testing.T) {
	// Inline merges the marshaler's fields into the parent object.
	line := logFieldLine(t, Inline(arrObj{"a"}))
	if !strings.Contains(line, `"name":"a"`) {
		t.Fatalf("inline field not merged: %s", line)
	}
}

func TestDictField(t *testing.T) {
	f := Dict("dict", String("a", "b"), Int("c", 3))
	raw := fieldRaw(t, f)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("dict not an object: %v", err)
	}
	if m["a"] != "b" || m["c"] != float64(3) {
		t.Fatalf("dict mismatch: %v", m)
	}
}

func TestDictObjectField(t *testing.T) {
	f := Object("o", DictObject(String("a", "b"), Int("c", 3)))
	raw := fieldRaw(t, f)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("dict object not an object: %v", err)
	}
	if m["a"] != "b" || m["c"] != float64(3) {
		t.Fatalf("dict object mismatch: %v", m)
	}
}

func TestAnyField(t *testing.T) {
	t1 := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"int", 42, float64(42)},
		{"string", "hello", "hello"},
		{"bool", true, true},
		{"float", 3.5, 3.5},
		{"slice", []int{1, 2, 3}, []interface{}{float64(1), float64(2), float64(3)}},
		{"map", map[string]int{"x": 1}, map[string]interface{}{"x": float64(1)}},
		{"error", errors.New("boom"), "boom"},
		{"time", t1, t1.Format(time.RFC3339Nano)},
		{"complex", complex128(1 + 2i), float64(1)},
		{"object", arrObj{"a"}, map[string]interface{}{"name": "a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := fieldRaw(t, Any("a", c.value))
			if got := decode(t, raw); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Any(%v) got %#v want %#v (raw %s)", c.value, got, c.want, raw)
			}
		})
	}
}

func TestAnyFieldNil(t *testing.T) {
	raw := fieldRaw(t, Any("a", nil))
	if string(raw) != "null" {
		t.Fatalf("Any(nil) should be null, got %s", raw)
	}
}
