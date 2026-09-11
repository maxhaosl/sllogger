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

	"github.com/maxhaosl/sllogger/slcore"
)

var (
	benchDate = time.Date(2021, 11, 14, 20, 37, 34, 376000000, time.Local)
	benchURL  = "http://play.example.com:443/playurl/v1/play/playurl"
)

func benchEntry() slcore.Entry {
	return slcore.Entry{Level: slcore.InfoLevel, Time: benchDate, LoggerName: "playurl"}
}

func benchFields() []slcore.Field {
	return []slcore.Field{
		{Key: FieldTraceID, Type: slcore.StringType, String: "3c06e3121c18f6114a2e9f2e38e5b8fe"},
		{Key: FieldSpanID, Type: slcore.StringType, String: "b256f6eac12c9818"},
		{Key: FieldMobile, Type: slcore.StringType, String: "18237438309"},
		{Key: FieldUserID, Type: slcore.StringType, String: "1071748417"},
		{Key: FieldClientID, Type: slcore.StringType, String: "c6559e74a24df8d97a2296ff1e23387a"},
		{Key: FieldURL, Type: slcore.StringType, String: benchURL},
		{Key: FieldMethod, Type: slcore.StringType, String: "GET"},
		{Key: FieldUseTime, Type: slcore.Int64Type, Integer: 123},
		{Key: FieldServerIP, Type: slcore.StringType, String: "127.0.0.1"},
		{Key: FieldLogMsg, Type: slcore.StringType, String: "^MG.getContent:[690894368]"},
	}
}

func BenchmarkCallInfoEncode(b *testing.B) {
	enc, err := NewCallInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		b.Fatal(err)
	}
	ent, fields := benchEntry(), benchFields()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, fields)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkRequestInfoEncode(b *testing.B) {
	enc, err := NewRequestInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		b.Fatal(err)
	}
	fields := []slcore.Field{
		{Key: FieldTraceID, Type: slcore.StringType, String: "tid"},
		{Key: FieldSpanID, Type: slcore.StringType, String: "sid"},
		{Key: FieldHeader, Type: slcore.StringType, String: "h1=v1"},
		{Key: FieldReqHeader, Type: slcore.StringType, String: "h2=v2"},
		{Key: FieldMethod, Type: slcore.StringType, String: "POST"},
		{Key: FieldRateLimiter, Type: slcore.StringType, String: "false"},
	}
	ent := benchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, fields)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkShortTemplateEncode(b *testing.B) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "date|log_level|log_msg",
		ServiceID: "playurl",
	})
	if err != nil {
		b.Fatal(err)
	}
	ent := benchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, nil)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkEscapeRequired(b *testing.B) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "log_msg",
		ServiceID: "svc",
	})
	if err != nil {
		b.Fatal(err)
	}
	fields := []slcore.Field{
		{Key: FieldLogMsg, Type: slcore.StringType, String: "a|b\nc\\d\re"},
	}
	ent := benchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, fields)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkCustomRenderer(b *testing.B) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "date|log_level",
		ServiceID: "playurl",
		Renderers: map[string]Renderer{
			FieldLogLevel: func(rc RenderContext) string {
				return rc.Entry.Level.CapitalString() + " [playurl,tid,sid,true]"
			},
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	ent := benchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, nil)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkWithContextClone(b *testing.B) {
	enc, err := NewCallInfoEncoder(TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		b.Fatal(err)
	}
	child := enc.Clone().(*TemplateEncoder)
	child.AddString(FieldTraceID, "ctx-trace")
	ent := benchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := child.EncodeEntry(ent, nil)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkJSONEncode(b *testing.B) {
	enc := NewJSONEncoder(slcore.EncoderConfig{})
	ent, fields := benchEntry(), benchFields()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := enc.EncodeEntry(ent, fields)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}

func BenchmarkEscapeField(b *testing.B) {
	value := strings.Repeat("payload ", 32) + "a|b\nc"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EscapeField(value)
	}
}

func BenchmarkAppendEscaped(b *testing.B) {
	pool := bufferPoolForBench()
	value := strings.Repeat("payload ", 32) + "a|b\nc"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		appendEscaped(buf, value)
		buf.Free()
	}
}
