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

	"github.com/maxhaosl/sllogger/slcore"
)

// TestTemplateEncoderObjectEncoderMethods 覆盖 TemplateEncoder 作为
// ObjectEncoder 的全部方法（即 ioCore.With(...) 注入上下文的路径）。
func TestTemplateEncoderObjectEncoderMethods(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "i64|f32|c128|c64|bs|bin",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}

	enc.AddInt64("i64", 7)
	enc.AddFloat32("f32", 2.5)
	enc.AddComplex128("c128", 1+2i)
	enc.AddComplex64("c64", complex64(3+4i))
	enc.AddByteString("bs", []byte("bytes"))
	enc.AddBinary("bin", []byte("binary"))

	if got := enc.context["i64"]; got != "7" {
		t.Errorf("i64 = %q, want 7", got)
	}
	if got := enc.context["f32"]; got != "2.5" {
		t.Errorf("f32 = %q, want 2.5", got)
	}
	if got := enc.context["c128"]; got != "(1+2i)" {
		t.Errorf("c128 = %q", got)
	}
	if got := enc.context["c64"]; got != "(3+4i)" {
		t.Errorf("c64 = %q", got)
	}
	if got := enc.context["bs"]; got != "bytes" {
		t.Errorf("bs = %q", got)
	}
	if got := enc.context["bin"]; got != "binary" {
		t.Errorf("bin = %q", got)
	}

	// 已注入的上下文必须出现在编码结果中。
	out := encodeToString(t, enc, slcore.Entry{}, nil)
	if want := "7|2.5|(1+2i)|(3+4i)|bytes|binary\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

// TestTemplateEncoderRemainingObjectMethods 覆盖其余标量方法。
func TestTemplateEncoderRemainingObjectMethods(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "x", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}

	enc.AddInt("i", 1)
	enc.AddInt32("i32", 2)
	enc.AddInt16("i16", 3)
	enc.AddInt8("i8", 4)
	enc.AddUint("u", 5)
	enc.AddUint64("u64", 6)
	enc.AddUint32("u32", 7)
	enc.AddUint16("u16", 8)
	enc.AddUint8("u8", 9)
	enc.AddUintptr("up", 10)
	enc.AddFloat64("f64", 1.5)
	enc.AddDuration("d", 2*time.Second)
	enc.AddTime("t", time.Unix(0, 0))
	if err := enc.AddReflected("r", "reflected"); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddArray("a", arrMarshaler{}); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddObject("o", objMarshaler{}); err != nil {
		t.Fatal(err)
	}
	enc.OpenNamespace("ns")

	wants := map[string]string{
		"i": "1", "i32": "2", "i16": "3", "i8": "4",
		"u": "5", "u64": "6", "u32": "7", "u16": "8", "u8": "9", "up": "10",
		"f64": "1.5", "d": "2000", "r": "reflected",
	}
	for k, want := range wants {
		if got := enc.context[k]; got != want {
			t.Errorf("context[%q] = %q, want %q", k, got, want)
		}
	}
	// Arrays, objects and namespaces are not renderable into a positional
	// template, so they must not create context entries.
	for _, k := range []string{"a", "o", "ns"} {
		if _, ok := enc.context[k]; ok {
			t.Errorf("context[%q] should not be set", k)
		}
	}
}

// TestMakeAppenderBuiltinBranches 覆盖 makeAppender 的每个分支。
func TestMakeAppenderBuiltinBranches(t *testing.T) {
	tm := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)
	ent := slcore.Entry{
		Level:      slcore.WarnLevel,
		Time:       tm,
		LoggerName: "svc",
		Caller:     slcore.NewEntryCaller(1, "/home/u/proj/pkg/file.go", 9, true),
		Message:    "msg",
	}

	t.Run("lowercase level", func(t *testing.T) {
		enc, err := NewTemplateEncoder(TemplateConfig{
			Template:       "log_level",
			LowercaseLevel: true,
			ServiceID:      "svc",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := encodeToString(t, enc, ent, nil); got != "warn\n" {
			t.Fatalf("got %q, want warn", got)
		}
	})
	t.Run("logger_name and caller", func(t *testing.T) {
		enc, err := NewTemplateEncoder(TemplateConfig{
			Template:  "logger_name|caller",
			ServiceID: "svc",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := encodeToString(t, enc, ent, nil); got != "svc|pkg/file.go:9\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("service_id from With context", func(t *testing.T) {
		enc, err := NewTemplateEncoder(TemplateConfig{Template: "service_id"})
		if err != nil {
			t.Fatal(err)
		}
		child := enc.Clone().(*TemplateEncoder)
		child.AddString(FieldServiceID, "from-with")
		if got := encodeToString(t, child, ent, nil); got != "from-with\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("custom renderer overrides builtin", func(t *testing.T) {
		enc, err := NewTemplateEncoder(TemplateConfig{
			Template:  "date",
			ServiceID: "svc",
			Renderers: map[string]Renderer{
				FieldDate: func(RenderContext) string { return "OVERRIDDEN|a|b" },
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		// 自定义渲染器的输出同样会被转义。
		if got := encodeToString(t, enc, ent, nil); got != `OVERRIDDEN\|a\|b`+"\n" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("log_msg falls back to entry message", func(t *testing.T) {
		enc, err := NewTemplateEncoder(TemplateConfig{Template: "log_msg", ServiceID: "svc"})
		if err != nil {
			t.Fatal(err)
		}
		if got := encodeToString(t, enc, ent, nil); got != "msg\n" {
			t.Fatalf("got %q", got)
		}
	})
}

// TestAppendFieldValueNonScalar 复杂类型字段不可渲染，回退到 null。
func TestAppendFieldValueNonScalar(t *testing.T) {
	cfg := &TemplateConfig{NullValue: "null"}
	pool := newTestPool()

	fields := []slcore.Field{
		{Key: "obj", Type: slcore.ObjectMarshalerType, Interface: objMarshaler{}},
		{Key: "arr", Type: slcore.ArrayMarshalerType, Interface: arrMarshaler{}},
		{Key: "ref", Type: slcore.ReflectType, Interface: map[string]int{"a": 1}},
		{Key: "bin", Type: slcore.BinaryType, Interface: []byte("raw")},
	}

	for _, key := range []string{"obj", "arr", "ref", "bin"} {
		buf := pool.Get()
		ok := appendFieldValue(buf, fields, key, cfg)
		out := buf.String()
		buf.Free()
		if ok {
			t.Errorf("%q should not be renderable", key)
		}
		if out != "" {
			t.Errorf("%q wrote %q, want nothing", key, out)
		}
	}

	// 复杂字段在模板中输出 null。
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "obj|arr|ref",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeToString(t, enc, slcore.Entry{}, fields); got != "null|null|null\n" {
		t.Fatalf("got %q", got)
	}
}

// TestRenderFieldUnknownName 未知字段名返回空串。
func TestRenderFieldUnknownName(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "date", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	rc := &RenderContext{Config: enc.cfg}
	if got := enc.renderField("date", rc); got != "0001-01-01 00:00:00.000" {
		t.Fatalf("date with zero time = %q", got)
	}
	// 未出现在模板中的名字仍然可以通过自定义渲染器解析。
	cfgWithRenderer := TemplateConfig{
		Template:  "date|custom",
		ServiceID: "svc",
		Renderers: map[string]Renderer{
			"custom": func(RenderContext) string { return "C" },
		},
	}
	enc2, err := NewTemplateEncoder(cfgWithRenderer)
	if err != nil {
		t.Fatal(err)
	}
	if got := enc2.renderField("custom", &RenderContext{Config: enc2.cfg}); got != "C" {
		t.Fatalf("custom = %q", got)
	}
	// 既不在模板也没有渲染器的名字返回空串。
	if got := enc2.renderField("missing", &RenderContext{Config: enc2.cfg}); got != "" {
		t.Fatalf("missing = %q, want empty", got)
	}
}
