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
	"strings"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/buffer"
	"github.com/maxhaosl/sllogger/slcore"
)

// bufferPoolForBench is shared by the encoder benchmarks.
func bufferPoolForBench() buffer.Pool { return buffer.NewPool() }

func encodeToString(t *testing.T, enc *TemplateEncoder, ent slcore.Entry, fields []slcore.Field) string {
	t.Helper()
	buf, err := enc.EncodeEntry(ent, fields)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	return buf.String()
}

var edgeTime = time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

func TestTemplateUnicodeAndLongFields(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "date|log_msg", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("数", 5000)
	fields := []slcore.Field{{Key: FieldLogMsg, Type: slcore.StringType, String: "中文" + long}}
	got := encodeToString(t, enc, slcore.Entry{Time: edgeTime}, fields)
	if !strings.Contains(got, "中文") {
		t.Fatal("unicode content lost")
	}
	if !strings.Contains(got, long) {
		t.Fatal("long field truncated")
	}
}

func TestTemplateUnknownFieldRendersNull(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "date|does_not_exist", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeToString(t, enc, slcore.Entry{Time: edgeTime}, nil)
	want := "2026-09-03 10:00:00.000|null\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateCustomSeparator(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "date\tlog_level\tlog_msg",
		Separator: "\t",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeToString(t, enc, slcore.Entry{Level: slcore.InfoLevel, Time: edgeTime, Message: "m"}, nil)
	want := "2026-09-03 10:00:00.000\tINFO\tm\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateWhitespaceAroundFields(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  " date | log_level ",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeToString(t, enc, slcore.Entry{Level: slcore.InfoLevel, Time: edgeTime}, nil)
	want := "2026-09-03 10:00:00.000|INFO\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateCustomTimeLayout(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:   "date",
		TimeLayout: "2006/01/02 15:04:05",
		ServiceID:  "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeToString(t, enc, slcore.Entry{Time: edgeTime}, nil)
	if got != "2026/09/03 10:00:00\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateCustomNullValue(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "log_msg",
		NullValue: "-",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeToString(t, enc, slcore.Entry{Time: edgeTime}, nil); got != "-\n" {
		t.Fatalf("got %q, want %q", got, "-")
	}
}

func TestTemplateCustomLineEnding(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:   "log_msg",
		LineEnding: "",
		ServiceID:  "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeToString(t, enc, slcore.Entry{Message: "m"}, nil); got != "m\n" {
		t.Fatalf("empty LineEnding must default to \\n, got %q", got)
	}
}

func TestTemplateNoEscape(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "log_msg",
		NoEscape:  true,
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := []slcore.Field{{Key: FieldLogMsg, Type: slcore.StringType, String: "a|b"}}
	if got := encodeToString(t, enc, slcore.Entry{}, fields); got != "a|b\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTemplateFieldsAndCloneIsolation(t *testing.T) {
	enc, err := NewCallInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(enc.Fields()); got != 14 {
		t.Fatalf("CALL_INFO field count = %d, want 14", got)
	}
	req, err := NewRequestInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(req.Fields()); got != 10 {
		t.Fatalf("RequestInfo field count = %d, want 10", got)
	}

	// Clone must deep-copy the With context in both directions.
	child := enc.Clone().(*TemplateEncoder)
	child.AddString(FieldTraceID, "child")
	child2 := child.Clone().(*TemplateEncoder)
	child2.AddString(FieldSpanID, "grandchild")

	ent := slcore.Entry{Time: edgeTime}
	if got := encodeToString(t, enc, ent, nil); strings.Contains(got, "child") {
		t.Fatal("parent saw the child's context")
	}
	if got := encodeToString(t, child, ent, nil); strings.Contains(got, "grandchild") {
		t.Fatal("child saw the grandchild's context")
	}
	if got := encodeToString(t, child2, ent, nil); !strings.Contains(got, "child") {
		t.Fatal("grandchild lost the inherited context")
	}
}

func TestTemplateObjectEncoderTypes(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "i|f|b|d|u|bin",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	child := enc.Clone().(*TemplateEncoder)
	child.AddInt("i", 3)
	child.AddFloat64("f", 1.5)
	child.AddBool("b", true)
	child.AddDuration("d", 2*time.Second)
	child.AddUint64("u", 8)
	child.AddBinary("bin", []byte("zz"))

	got := encodeToString(t, child, slcore.Entry{Time: edgeTime}, nil)
	want := "3|1.5|true|2000|8|zz\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateErrors(t *testing.T) {
	if _, err := NewTemplateEncoder(TemplateConfig{Separator: "|"}); err == nil {
		t.Fatal("empty template must fail")
	}
	if _, err := NewTemplateEncoder(TemplateConfig{Template: "   "}); err == nil {
		t.Fatal("blank template must fail")
	}
	if _, err := NewTemplateEncoder(TemplateConfig{Template: "a||b"}); err == nil {
		t.Fatal("empty segment must fail")
	}
}

func TestTemplateRenderFieldHelper(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "date|log_msg", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	rc := &RenderContext{
		Entry:  slcore.Entry{Time: edgeTime, Message: "msg"},
		Config: enc.cfg,
	}
	if got := enc.renderField(FieldDate, rc); got != "2026-09-03 10:00:00.000" {
		t.Fatalf("renderField(date) = %q", got)
	}
	if got := enc.renderField(FieldLogMsg, rc); got != "msg" {
		t.Fatalf("renderField(log_msg) = %q", got)
	}
	if got := enc.renderField("unknown", rc); got != "" {
		t.Fatalf("renderField(unknown) = %q, want empty", got)
	}
}
