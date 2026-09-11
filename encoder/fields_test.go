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
	"math"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/buffer"
	"github.com/maxhaosl/sllogger/slcore"
)

func TestEscapeField(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"plain", "plain"},
		{"a|b", `a\|b`},
		{"line\nbreak", `line\nbreak`},
		{"carriage\r", `carriage\r`},
		{`back\slash`, `back\\slash`},
		{"mix|a\nb\r\\c", `mix\|a\nb\r\\c`},
		{"中文", "中文"},
		{"emoji 😀", "emoji 😀"},
	}
	for _, tt := range tests {
		if got := EscapeField(tt.in); got != tt.want {
			t.Errorf("EscapeField(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAppendEscapedMatchesEscapeField(t *testing.T) {
	inputs := []string{
		"",
		"plain",
		"a|b",
		"a|b|c\nd\\e\rf",
		"|",
		"\\",
		"leading|and-trailing\\",
		"long text without specials " + string(make([]byte, 0)),
	}
	pool := newTestPool()
	for _, in := range inputs {
		buf := pool.Get()
		appendEscaped(buf, in)
		got := buf.String()
		buf.Free()
		if want := EscapeField(in); got != want {
			t.Errorf("appendEscaped(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppendSanitized(t *testing.T) {
	cfg := &TemplateConfig{NullValue: "null"}
	pool := newTestPool()

	tests := []struct {
		name      string
		value     string
		want      string
		noEscape  bool
		nullValue string
	}{
		{"empty becomes null", "", "null", false, "null"},
		{"escaped by default", "a|b", `a\|b`, false, "null"},
		{"no escape", "a|b", "a|b", true, "null"},
		{"custom null", "", "-", false, "-"},
		{"plain", "value", "value", false, "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.NoEscape = tt.noEscape
			cfg.NullValue = tt.nullValue
			buf := pool.Get()
			appendSanitized(buf, cfg, tt.value)
			got := buf.String()
			buf.Free()
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderContextLookup(t *testing.T) {
	fields := []slcore.Field{
		{Key: "s", Type: slcore.StringType, String: "str"},
		{Key: "b", Type: slcore.BoolType, Integer: 1},
		{Key: "i64", Type: slcore.Int64Type, Integer: -5},
		{Key: "u64", Type: slcore.Uint64Type, Integer: 9},
		{Key: "dur", Type: slcore.DurationType, Integer: int64(2 * time.Second)},
		{Key: "f64", Type: slcore.Float64Type, Integer: int64(math.Float64bits(2.5))},
		{Key: "f32", Type: slcore.Float32Type, Integer: int64(math.Float32bits(1.5))},
		{Key: "obj", Type: slcore.ObjectMarshalerType, Interface: objMarshaler{}},
		{Key: "dup", Type: slcore.StringType, String: "first"},
		{Key: "dup", Type: slcore.StringType, String: "second"},
	}
	rc := &RenderContext{Fields: fields, Config: &TemplateConfig{}}

	tests := map[string]string{
		"s":    "str",
		"b":    "true",
		"i64":  "-5",
		"u64":  "9",
		"dur":  "2000",
		"f64":  "2.5",
		"f32":  "1.5",
		"obj":  "", // complex types are not renderable
		"dup":  "first",
		"nope": "",
	}
	for name, want := range tests {
		if got := rc.lookup(name); got != want {
			t.Errorf("lookup(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestAppendFieldValue(t *testing.T) {
	cfg := &TemplateConfig{NullValue: "null"}
	fields := []slcore.Field{
		{Key: "s", Type: slcore.StringType, String: "hello"},
		{Key: "empty", Type: slcore.StringType, String: ""},
		{Key: "esc", Type: slcore.StringType, String: "a|b"},
		{Key: "i", Type: slcore.Int64Type, Integer: 42},
		{Key: "u", Type: slcore.Uint32Type, Integer: 7},
		{Key: "b", Type: slcore.BoolType, Integer: 1},
		{Key: "d", Type: slcore.DurationType, Integer: int64(time.Second)},
		{Key: "f", Type: slcore.Float64Type, Integer: int64(math.Float64bits(0.5))},
		{Key: "obj", Type: slcore.ObjectMarshalerType, Interface: objMarshaler{}},
	}
	pool := newTestPool()

	tests := []struct {
		name   string
		key    string
		want   string
		exists bool
	}{
		{"string", "s", "hello", true},
		{"empty string falls back", "empty", "", false},
		{"escaped string", "esc", `a\|b`, true},
		{"int", "i", "42", true},
		{"uint", "u", "7", true},
		{"bool", "b", "true", true},
		{"duration ms", "d", "1000", true},
		{"float", "f", "0.5", true},
		{"object not renderable", "obj", "", false},
		{"missing", "missing", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := pool.Get()
			got := appendFieldValue(buf, fields, tt.key, cfg)
			out := buf.String()
			buf.Free()
			if got != tt.exists {
				t.Fatalf("exists = %v, want %v", got, tt.exists)
			}
			if out != tt.want {
				t.Fatalf("value = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	cfg := &TemplateConfig{NullValue: "null"}
	if got := sanitize(cfg, ""); got != "null" {
		t.Errorf("empty = %q", got)
	}
	if got := sanitize(cfg, "a|b"); got != `a\|b` {
		t.Errorf("escaped = %q", got)
	}
	cfg.NoEscape = true
	if got := sanitize(cfg, "a|b"); got != "a|b" {
		t.Errorf("no-escape = %q", got)
	}
}

func TestBuiltinRenderers(t *testing.T) {
	tm := time.Date(2026, 9, 3, 1, 2, 3, 4000000, time.Local)
	cfg := &TemplateConfig{
		TimeLayout: DefaultTimeLayout,
		ServiceID:  "playurl",
		NullValue:  "null",
	}

	t.Run("date", func(t *testing.T) {
		rc := RenderContext{Entry: slcore.Entry{Time: tm}, Config: cfg}
		if got := builtinRenderers[FieldDate](rc); got != "2026-09-03 01:02:03.004" {
			t.Errorf("date = %q", got)
		}
	})
	t.Run("log_level", func(t *testing.T) {
		rc := RenderContext{Entry: slcore.Entry{Level: slcore.WarnLevel}, Config: cfg}
		if got := builtinRenderers[FieldLogLevel](rc); got != "WARN" {
			t.Errorf("level = %q", got)
		}
		cfg.LowercaseLevel = true
		if got := builtinRenderers[FieldLogLevel](rc); got != "warn" {
			t.Errorf("lowercase level = %q", got)
		}
		cfg.LowercaseLevel = false
	})
	t.Run("service_id configurable and overridable", func(t *testing.T) {
		rc := RenderContext{Config: cfg}
		if got := builtinRenderers[FieldServiceID](rc); got != "playurl" {
			t.Errorf("service_id = %q", got)
		}
		rc.Fields = []slcore.Field{{Key: FieldServiceID, Type: slcore.StringType, String: "override"}}
		if got := builtinRenderers[FieldServiceID](rc); got != "override" {
			t.Errorf("overridden service_id = %q", got)
		}
	})
	t.Run("log_msg falls back to entry message", func(t *testing.T) {
		rc := RenderContext{Entry: slcore.Entry{Message: "from entry"}, Config: cfg}
		if got := builtinRenderers[FieldLogMsg](rc); got != "from entry" {
			t.Errorf("log_msg = %q", got)
		}
		rc.Fields = []slcore.Field{{Key: FieldLogMsg, Type: slcore.StringType, String: "from field"}}
		if got := builtinRenderers[FieldLogMsg](rc); got != "from field" {
			t.Errorf("log_msg field = %q", got)
		}
	})
	t.Run("caller and logger name", func(t *testing.T) {
		rc := RenderContext{Entry: slcore.Entry{
			LoggerName: "svc",
			Caller:     slcore.NewEntryCaller(1, "/home/u/proj/pkg/file.go", 5, true),
		}, Config: cfg}
		if got := builtinRenderers[FieldLoggerName](rc); got != "svc" {
			t.Errorf("logger_name = %q", got)
		}
		if got := builtinRenderers[FieldCaller](rc); got != "pkg/file.go:5" {
			t.Errorf("caller = %q", got)
		}
	})
}

func TestRegisterRenderer(t *testing.T) {
	cfg := &TemplateConfig{}
	RegisterRenderer(cfg, FieldServiceID, func(rc RenderContext) string {
		return "custom"
	})
	if cfg.Renderers[FieldServiceID] == nil {
		t.Fatal("renderer not registered")
	}
	if got := cfg.Renderers[FieldServiceID](RenderContext{}); got != "custom" {
		t.Fatalf("renderer = %q", got)
	}
}

// newTestPool returns a buffer pool used by these tests.
func newTestPool() buffer.Pool { return buffer.NewPool() }
