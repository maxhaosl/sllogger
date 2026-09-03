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

	"sllogger/slcore"
)

var sampleTime = time.Date(2021, 11, 14, 20, 37, 34, 376000000, time.Local)

func callInfoFields() []slcore.Field {
	return []slcore.Field{
		slcore.Field{Key: FieldTraceID, Type: slcore.StringType, String: "3c06e3121c18f6114a2e9f2e38e5b8fe"},
		slcore.Field{Key: FieldSpanID, Type: slcore.StringType, String: "b256f6eac12c9818"},
		slcore.Field{Key: FieldMobile, Type: slcore.StringType, String: "18237438309"},
		slcore.Field{Key: FieldUserID, Type: slcore.StringType, String: "1071748417"},
		slcore.Field{Key: FieldClientID, Type: slcore.StringType, String: "c6559e74a24df8d97a2296ff1e23387a"},
		slcore.Field{Key: FieldURL, Type: slcore.StringType, String: "http://play.example.com:443/playurl/v1/play/playurl"},
		slcore.Field{Key: FieldMethod, Type: slcore.StringType, String: "GET"},
		slcore.Field{Key: FieldUseTime, Type: slcore.Int64Type, Integer: 123},
		slcore.Field{Key: FieldServerIP, Type: slcore.StringType, String: "127.0.0.1"},
		slcore.Field{Key: FieldLogMsg, Type: slcore.StringType, String: "^MG.getContent:[690894368]"},
	}
}

func encode(t *testing.T, enc *TemplateEncoder, ent slcore.Entry, fields []slcore.Field) string {
	t.Helper()
	buf, err := enc.EncodeEntry(ent, fields)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	return buf.String()
}

func TestCallInfoTemplate(t *testing.T) {
	enc, err := NewCallInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}

	ent := slcore.Entry{Level: slcore.InfoLevel, Time: sampleTime}
	got := encode(t, enc, ent, callInfoFields())
	want := "2021-11-14 20:37:34.376|INFO|playurl|" +
		"3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|" +
		"18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|" +
		"http://play.example.com:443/playurl/v1/play/playurl|GET|123|" +
		"127.0.0.1|null|^MG.getContent:[690894368]\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRequestInfoTemplate(t *testing.T) {
	enc, err := NewRequestInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}

	fields := []slcore.Field{
		slcore.Field{Key: FieldTraceID, Type: slcore.StringType, String: "tid"},
		slcore.Field{Key: FieldSpanID, Type: slcore.StringType, String: "sid"},
		slcore.Field{Key: FieldHeader, Type: slcore.StringType, String: "resp-header"},
		slcore.Field{Key: FieldReqHeader, Type: slcore.StringType, String: "req-header"},
		slcore.Field{Key: FieldMethod, Type: slcore.StringType, String: "POST"},
		slcore.Field{Key: FieldRateLimiter, Type: slcore.StringType, String: "false"},
	}
	ent := slcore.Entry{Level: slcore.InfoLevel, Time: sampleTime, Message: "request success"}
	got := encode(t, enc, ent, fields)
	want := "2021-11-14 20:37:34.376|INFO|playurl|tid|sid|resp-header|req-header|POST|false|request success\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNullValuesAndEscaping(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "date|url|method|log_msg",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	ent := slcore.Entry{Level: slcore.InfoLevel, Time: sampleTime}
	got := encode(t, enc, ent, []slcore.Field{
		slcore.Field{Key: FieldLogMsg, Type: slcore.StringType, String: "a|b\nc\\d"},
	})
	want := "2021-11-14 20:37:34.376|null|null|a\\|b\\nc\\\\d\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCustomRenderer(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "date|log_level|service_id",
		ServiceID: "fixed",
		Renderers: map[string]Renderer{
			// 老格式：` INFO [playurl,tid,sid,true]`
			FieldLogLevel: func(rc RenderContext) string {
				return rc.Entry.Level.CapitalString() + " [" + rc.Config.ServiceID + ",tid,sid,true]"
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ent := slcore.Entry{Level: slcore.InfoLevel, Time: sampleTime}
	got := encode(t, enc, ent, nil)
	want := "2021-11-14 20:37:34.376|INFO [fixed,tid,sid,true]|fixed\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithFieldsAccumulate(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{Template: "service_id|trace_id", ServiceID: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	// ioCore.With clones the encoder and calls AddString.
	child := enc.Clone().(*TemplateEncoder)
	child.AddString(FieldTraceID, "tid-from-with")

	ent := slcore.Entry{Level: slcore.InfoLevel, Time: sampleTime}
	got := encode(t, enc, ent, nil)
	want := "svc|null\n"
	if got != want {
		t.Fatalf("parent must not see with-fields: got %q", got)
	}
	got = encode(t, child, ent, nil)
	want = "svc|tid-from-with\n"
	if got != want {
		t.Fatalf("child must see with-fields: got %q", got)
	}
}

func TestEmptyTemplate(t *testing.T) {
	if _, err := NewTemplateEncoder(TemplateConfig{}); err == nil {
		t.Fatal("expected error for empty template")
	}
	if _, err := NewTemplateEncoder(TemplateConfig{Template: "date||level"}); err == nil {
		t.Fatal("expected error for empty field segment")
	}
}

// ensure strings stays referenced.
var _ = strings.Contains
