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
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

func TestArrayConstructors(t *testing.T) {
	t1 := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 3, 11, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		field Field
		want  interface{}
	}{
		{"Bools", Bools("k", []bool{true, false}), []interface{}{true, false}},
		{"ByteStrings", ByteStrings("k", [][]byte{[]byte("a"), []byte("b")}), []interface{}{"a", "b"}},
		{"Complex128s", Complex128s("k", []complex128{1 + 2i, 3 + 4i}), []interface{}{1.0, 3.0}},
		{"Complex64s", Complex64s("k", []complex64{1 + 2i, 3 + 4i}), []interface{}{1.0, 3.0}},
		{"Durations", Durations("k", []time.Duration{time.Second, 2 * time.Second}), []interface{}{float64(1000), float64(2000)}},
		{"Float64s", Float64s("k", []float64{1.5, 2.5}), []interface{}{1.5, 2.5}},
		{"Float32s", Float32s("k", []float32{1.5, 2.5}), []interface{}{float64(1.5), float64(2.5)}},
		{"Ints", Ints("k", []int{1, 2, 3}), []interface{}{float64(1), float64(2), float64(3)}},
		{"Int64s", Int64s("k", []int64{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Int32s", Int32s("k", []int32{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Int16s", Int16s("k", []int16{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Int8s", Int8s("k", []int8{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Strings", Strings("k", []string{"a", "b"}), []interface{}{"a", "b"}},
		{"Uints", Uints("k", []uint{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Uint64s", Uint64s("k", []uint64{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Uint32s", Uint32s("k", []uint32{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Uint16s", Uint16s("k", []uint16{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Uint8s", Uint8s("k", []uint8{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Uintptrs", Uintptrs("k", []uintptr{1, 2}), []interface{}{float64(1), float64(2)}},
		{"Times", Times("k", []time.Time{t1, t2}), []interface{}{t1.Format(time.RFC3339Nano), t2.Format(time.RFC3339Nano)}},
		{"Objects", Objects("k", []slcore.ObjectMarshaler{arrObj{"a"}, arrObj{"b"}}), []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}},
		{"ObjectValues", ObjectValues("k", []arrPtrObj{{"a"}, {"b"}}), []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}},
		{"Stringers", Stringers("k", []fmt.Stringer{myStr("x"), myStr("y")}), []interface{}{"x", "y"}},
		{"Errors", Errors("k", []error{errors.New("e1"), errors.New("e2")}), []interface{}{map[string]interface{}{"error": "e1"}, map[string]interface{}{"error": "e2"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := fieldRaw(t, c.field)
			got := decode(t, raw)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v, want %#v (raw %s)", got, c.want, raw)
			}
		})
	}
}

func TestErrorsSkipsNil(t *testing.T) {
	raw := fieldRaw(t, Errors("k", []error{nil, errors.New("e")}))
	arr, ok := decode(t, raw).([]interface{})
	if !ok {
		t.Fatalf("expected array, got %T", decode(t, raw))
	}
	if len(arr) != 1 {
		t.Fatalf("nil error should be skipped; got %d elements: %v", len(arr), arr)
	}
}
