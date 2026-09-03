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

// TestJSONEncoderWithAllTypes 覆盖 JSONEncoder 自身的 ObjectEncoder 方法，
// 即 ioCore.With(fields) 注入的上下文路径。
func TestJSONEncoderWithAllTypes(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())

	enc.AddBool("b", true)
	enc.AddByteString("bs", []byte("bytes"))
	enc.AddBinary("bin", []byte("binary"))
	enc.AddComplex128("c128", 1+2i)
	enc.AddComplex64("c64", complex64(3+4i))
	enc.AddDuration("d", 2*time.Second)
	enc.AddFloat64("f64", 1.5)
	enc.AddFloat32("f32", 2.5)
	enc.AddInt("i", 1)
	enc.AddInt64("i64", 2)
	enc.AddInt32("i32", 3)
	enc.AddInt16("i16", 4)
	enc.AddInt8("i8", 5)
	enc.AddString("s", "v")
	enc.AddTime("t", time.Unix(0, 0).UTC())
	enc.AddUint("u", 6)
	enc.AddUint64("u64", 7)
	enc.AddUint32("u32", 8)
	enc.AddUint16("u16", 9)
	enc.AddUint8("u8", 10)
	enc.AddUintptr("up", 11)
	if err := enc.AddReflected("r", "reflected"); err != nil {
		t.Fatal(err)
	}
	// OpenNamespace is a no-op for this encoder.
	enc.OpenNamespace("ns")

	want := map[string]interface{}{
		"b": true, "bs": "bytes", "bin": "binary",
		"c128": float64(1), "c64": float32(3),
		"d":   int64(2000),
		"f64": 1.5, "f32": float32(2.5),
		"i": 1, "i64": int64(2), "i32": int32(3), "i16": int16(4), "i8": int8(5),
		"s": "v", "t": "1970-01-01T00:00:00Z",
		"u": uint(6), "u64": uint64(7), "u32": uint32(8), "u16": uint16(9), "u8": uint8(10),
		"up": uint64(11), "r": "reflected",
	}
	for k, expect := range want {
		got, ok := enc.ctx[k]
		if !ok {
			t.Errorf("key %q missing", k)
			continue
		}
		if got != expect {
			t.Errorf("ctx[%q] = %#v, want %#v", k, got, expect)
		}
	}
	if _, ok := enc.ctx["ns"]; ok {
		t.Error("OpenNamespace should not create a key")
	}

	// The context must reach the encoded output.
	out := encodeJSON(t, enc, slcore.Entry{Level: slcore.InfoLevel}, nil)
	for k := range want {
		if _, ok := out[k]; !ok {
			t.Errorf("encoded output missing key %q: %v", k, out)
		}
	}
}

func TestJSONEncoderWithObjectAndArray(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())

	if err := enc.AddObject("obj", objMarshaler{}); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddArray("arr", multiArray{}); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddObject("bad", failingObject{}); err == nil {
		t.Fatal("expected an error from a failing object marshaler")
	}
	if err := enc.AddArray("badarr", failingArray{}); err == nil {
		t.Fatal("expected an error from a failing array marshaler")
	}

	out := encodeJSON(t, enc, slcore.Entry{Level: slcore.InfoLevel}, nil)
	obj, ok := out["obj"].(map[string]interface{})
	if !ok || obj["inner"] != "value" {
		t.Fatalf("obj = %#v", out["obj"])
	}
	arr, ok := out["arr"].([]interface{})
	if !ok || len(arr) != 3 {
		t.Fatalf("arr = %#v", out["arr"])
	}
}

// TestJSONEncoderCloneDeepCopiesContext 确保 With 上下文在 Clone 后双向隔离。
func TestJSONEncoderCloneDeepCopiesContext(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	enc.AddString("a", "1")

	child := enc.Clone().(*JSONEncoder)
	child.AddString("b", "2")
	grandchild := child.Clone().(*JSONEncoder)
	grandchild.AddString("c", "3")

	if _, ok := enc.ctx["b"]; ok {
		t.Error("parent saw the child's context")
	}
	if _, ok := child.ctx["c"]; ok {
		t.Error("child saw the grandchild's context")
	}
	if grandchild.ctx["a"] != "1" || grandchild.ctx["b"] != "2" {
		t.Error("grandchild lost inherited context")
	}
}

// TestJSONEncoderCloneCopiesConfig 确保 Clone 保留编码器配置。
func TestJSONEncoderCloneCopiesConfig(t *testing.T) {
	cfg := DefaultJSONEncoderConfig()
	cfg.LineEnding = "\r\n"
	cfg.MessageKey = "message"
	enc := NewJSONEncoder(cfg)

	clone := enc.Clone().(*JSONEncoder)
	if clone.cfg.MessageKey != "message" || clone.cfg.LineEnding != "\r\n" {
		t.Fatalf("clone lost config: %+v", clone.cfg)
	}
}
