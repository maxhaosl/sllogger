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

package encoder

import (
	"testing"
	"time"

	"sllogger/slcore"
)

// TestMapArrayEncoderPrimitives 覆盖 mapArrayEncoder 的全部 Append* 方法。
func TestMapArrayEncoderPrimitives(t *testing.T) {
	ae := &mapArrayEncoder{}
	ae.AppendBool(true)
	ae.AppendByteString([]byte("bs"))
	ae.AppendComplex128(1 + 2i)
	ae.AppendComplex64(complex64(3 + 4i))
	ae.AppendFloat64(1.5)
	ae.AppendFloat32(2.5)
	ae.AppendInt(1)
	ae.AppendInt64(2)
	ae.AppendInt32(3)
	ae.AppendInt16(4)
	ae.AppendInt8(5)
	ae.AppendString("s")
	ae.AppendUint(6)
	ae.AppendUint64(7)
	ae.AppendUint32(8)
	ae.AppendUint16(9)
	ae.AppendUint8(10)
	ae.AppendUintptr(11)
	ae.AppendDuration(2 * time.Second)
	ae.AppendTime(time.Unix(0, 0).UTC())

	// Complex and float32 values keep their narrow types: only the real part
	// is stored, and float32 is not widened.
	want := []interface{}{
		true, "bs", float64(1), float32(3), 1.5, float32(2.5),
		1, int64(2), int32(3), int16(4), int8(5), "s",
		uint(6), uint64(7), uint32(8), uint16(9), uint8(10), uint64(11),
		int64(2000), "1970-01-01T00:00:00Z",
	}
	if len(ae.items) != len(want) {
		t.Fatalf("items = %d, want %d: %v", len(ae.items), len(want), ae.items)
	}
	for i, w := range want {
		if ae.items[i] != w {
			t.Errorf("items[%d] = %#v, want %#v", i, ae.items[i], w)
		}
	}
}

func TestMapArrayEncoderNested(t *testing.T) {
	ae := &mapArrayEncoder{}
	if err := ae.AppendArray(arrMarshaler{}); err != nil {
		t.Fatal(err)
	}
	if err := ae.AppendObject(objMarshaler{}); err != nil {
		t.Fatal(err)
	}
	if err := ae.AppendReflected("reflected"); err != nil {
		t.Fatal(err)
	}

	// AppendArray nests a sub-slice.
	nested, ok := ae.items[0].([]interface{})
	if !ok {
		t.Fatalf("nested array = %#v", ae.items[0])
	}
	if len(nested) != 1 || nested[0] != "first" {
		t.Fatalf("nested = %v", nested)
	}
	// AppendObject nests a map.
	obj, ok := ae.items[1].(map[string]interface{})
	if !ok {
		t.Fatalf("nested object = %#v", ae.items[1])
	}
	if obj["inner"] != "value" {
		t.Fatalf("object = %v", obj)
	}
	if ae.items[2] != "reflected" {
		t.Fatalf("reflected = %v", ae.items[2])
	}
}

func TestMapArrayEncoderArrayError(t *testing.T) {
	ae := &mapArrayEncoder{}
	if err := ae.AppendArray(failingArray{}); err == nil {
		t.Fatal("expected an error from a failing array marshaler")
	}
	if err := ae.AppendObject(failingObject{}); err == nil {
		t.Fatal("expected an error from a failing object marshaler")
	}
}

type failingArray struct{}

func (failingArray) MarshalLogArray(slcore.ArrayEncoder) error { return errTest }

var errTest = errTestValue()

func errTestValue() error { return &testError{} }

type testError struct{}

func (*testError) Error() string { return "test error" }

func TestMapObjectEncoderAllTypes(t *testing.T) {
	m := mapObjectEncoder{}
	m.AddBool("b", true)
	m.AddByteString("bs", []byte("bytes"))
	m.AddBinary("bin", []byte("binary"))
	m.AddComplex128("c128", 1+2i)
	m.AddComplex64("c64", complex64(3+4i))
	m.AddDuration("d", 2*time.Second)
	m.AddFloat64("f64", 1.5)
	m.AddFloat32("f32", 2.5)
	m.AddInt("i", 1)
	m.AddInt64("i64", 2)
	m.AddInt32("i32", 3)
	m.AddInt16("i16", 4)
	m.AddInt8("i8", 5)
	m.AddString("s", "v")
	m.AddTime("t", time.Unix(0, 0).UTC())
	m.AddUint("u", 6)
	m.AddUint64("u64", 7)
	m.AddUint32("u32", 8)
	m.AddUint16("u16", 9)
	m.AddUint8("u8", 10)
	m.AddUintptr("up", 11)
	if err := m.AddReflected("r", "reflected"); err != nil {
		t.Fatal(err)
	}
	m.OpenNamespace("ns")

	expects := map[string]interface{}{
		"b": true, "bs": "bytes", "bin": "binary",
		// Complex and float32 keep their narrow types.
		"c128": float64(1), "c64": float32(3),
		"d":   int64(2000),
		"f64": 1.5, "f32": float32(2.5),
		"i": 1, "i64": int64(2), "i32": int32(3), "i16": int16(4), "i8": int8(5),
		// AddTime formats to RFC3339Nano.
		"s": "v", "t": "1970-01-01T00:00:00Z",
		"u": uint(6), "u64": uint64(7), "u32": uint32(8), "u16": uint16(9), "u8": uint8(10),
		"up": uint64(11), "r": "reflected",
	}
	for k, want := range expects {
		got, ok := m[k]
		if !ok {
			t.Errorf("key %q missing", k)
			continue
		}
		if got != want {
			t.Errorf("m[%q] = %#v, want %#v", k, got, want)
		}
	}
	// OpenNamespace is a no-op for the JSON encoder: no key is created.
	if _, ok := m["ns"]; ok {
		t.Errorf("OpenNamespace should not create a key, got %v", m["ns"])
	}
}

func TestMapObjectEncoderErrors(t *testing.T) {
	m := mapObjectEncoder{}
	if err := m.AddObject("o", failingObject{}); err == nil {
		t.Fatal("expected an error from a failing object marshaler")
	}
	if err := m.AddArray("a", failingArray{}); err == nil {
		t.Fatal("expected an error from a failing array marshaler")
	}
	// Successful nested cases.
	if err := m.AddObject("ok-obj", objMarshaler{}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddArray("ok-arr", arrMarshaler{}); err != nil {
		t.Fatal(err)
	}
	obj, ok := m["ok-obj"].(map[string]interface{})
	if !ok || obj["inner"] != "value" {
		t.Fatalf("ok-obj = %#v", m["ok-obj"])
	}
	arr, ok := m["ok-arr"].([]interface{})
	if !ok || len(arr) != 1 || arr[0] != "first" {
		t.Fatalf("ok-arr = %#v", m["ok-arr"])
	}
}

type failingObject struct{}

func (failingObject) MarshalLogObject(slcore.ObjectEncoder) error { return errTest }

func TestPrimitiveCapture(t *testing.T) {
	var c primitiveCapture
	c.AppendBool(true)
	if c.val != true {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendByteString([]byte("bs"))
	if c.val != "bs" {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendComplex128(1 + 2i)
	if c.val != float64(1) {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendComplex64(complex64(3 + 4i))
	if c.val != float32(3) {
		t.Fatalf("val = %v (%T)", c.val, c.val)
	}
	c.AppendFloat64(1.5)
	if c.val != 1.5 {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendFloat32(2.5)
	if c.val != float32(2.5) {
		t.Fatalf("val = %v (%T)", c.val, c.val)
	}
	c.AppendInt(1)
	c.AppendInt64(2)
	c.AppendInt32(3)
	c.AppendInt16(4)
	c.AppendInt8(5)
	if c.val != int8(5) {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendString("s")
	if c.val != "s" {
		t.Fatalf("val = %v", c.val)
	}
	c.AppendUint(6)
	c.AppendUint64(7)
	c.AppendUint32(8)
	c.AppendUint16(9)
	c.AppendUint8(10)
	c.AppendUintptr(11)
	if c.val != uint64(11) {
		t.Fatalf("val = %v", c.val)
	}
}

func TestLineEndingOr(t *testing.T) {
	if got := lineEndingOr(""); got != "\n" {
		t.Fatalf("empty = %q", got)
	}
	if got := lineEndingOr("\r\n"); got != "\r\n" {
		t.Fatalf("custom = %q", got)
	}
}

// TestJSONEncoderCustomLevelAndTimeEncoders 验证自定义 EncodeLevel/EncodeTime
// 写入的是配置键，而不是内部占位键。
func TestJSONEncoderCustomLevelAndTimeEncoders(t *testing.T) {
	cfg := DefaultJSONEncoderConfig()
	cfg.EncodeLevel = func(l slcore.Level, enc slcore.PrimitiveArrayEncoder) {
		enc.AppendString("L" + l.CapitalString())
	}
	cfg.EncodeTime = func(t time.Time, enc slcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006"))
	}
	enc := NewJSONEncoder(cfg)

	out := encodeJSON(t, enc, slcore.Entry{
		Level: slcore.ErrorLevel,
		Time:  time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
	}, nil)

	if out["level"] != "LERROR" {
		t.Fatalf("level = %v", out["level"])
	}
	if out["ts"] != "2026" {
		t.Fatalf("ts = %v", out["ts"])
	}
	if _, ok := out["value"]; ok {
		t.Fatalf("internal placeholder key leaked: %v", out)
	}
}

// TestJSONEncoderWithArray 验证 With 注入的数组能正确序列化为 JSON 数组。
func TestJSONEncoderWithArray(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	child := enc.Clone().(*JSONEncoder)
	if err := child.AddArray("items", multiArray{}); err != nil {
		t.Fatal(err)
	}
	out := encodeJSON(t, child, slcore.Entry{Level: slcore.InfoLevel}, nil)

	arr, ok := out["items"].([]interface{})
	if !ok {
		t.Fatalf("items = %#v, want an array", out["items"])
	}
	if len(arr) != 3 || arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Fatalf("items = %v", arr)
	}
}

type multiArray struct{}

func (multiArray) MarshalLogArray(enc slcore.ArrayEncoder) error {
	enc.AppendString("a")
	enc.AppendString("b")
	enc.AppendString("c")
	return nil
}
