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
	"context"
	"io"
	"testing"
	"time"

	"sllogger/encoder"
	"sllogger/slcore"
)

var benchTime = time.Date(2021, 11, 14, 20, 37, 34, 376000000, time.Local)

type benchClock struct{}

func (benchClock) Now() time.Time                         { return benchTime }
func (benchClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func benchCallFields() []Field {
	return []Field{
		String(encoder.FieldTraceID, "3c06e3121c18f6114a2e9f2e38e5b8fe"),
		String(encoder.FieldSpanID, "b256f6eac12c9818"),
		String(encoder.FieldMobile, "18237438309"),
		String(encoder.FieldUserID, "1071748417"),
		String(encoder.FieldClientID, "c6559e74a24df8d97a2296ff1e23387a"),
		String(encoder.FieldURL, "http://play.example.com:443/playurl/v1/play/playurl"),
		String(encoder.FieldMethod, "GET"),
		Int64(encoder.FieldUseTime, 123),
		String(encoder.FieldServerIP, "127.0.0.1"),
		String(encoder.FieldBussID, "null"),
		String(encoder.FieldLogMsg, "^MG.getContent:[690894368]"),
	}
}

func benchEntry() slcore.Entry {
	return slcore.Entry{
		Level:      InfoLevel,
		Time:       benchTime,
		LoggerName: "playurl",
		Message:    "^MG.getContent:[690894368]",
	}
}

func mustEncoder(b *testing.B) slcore.Encoder {
	b.Helper()
	enc, err := encoder.NewCallInfoEncoder(encoder.TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		b.Fatal(err)
	}
	return enc
}

// BenchmarkEncoderCallInfo 测量 CALL_INFO 模板编码耗时（设计文档目标 <5μs）。
func BenchmarkEncoderCallInfo(b *testing.B) {
	enc := mustEncoder(b)
	ent := benchEntry()
	fields := benchCallFields()

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

// BenchmarkLoggerDisabled 测量日志被禁用时的开销。
func BenchmarkLoggerDisabled(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), ErrorLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Info("disabled message")
	}
}

// BenchmarkLoggerToDiscard 测量完整编码链路（不含真实文件 IO）。
func BenchmarkLoggerToDiscard(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Info("enabled message", benchCallFields()...)
	}
}

func BenchmarkLoggerWith(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = log.With(String("trace_id", "tid"))
	}
}

func BenchmarkLoggerNamed(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = log.Named("api")
	}
}

func BenchmarkSugarInfow(b *testing.B) {
	sugar := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel)).Sugar()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sugar.Infow("message", "url", "http://svc/api", "useTime", 123)
	}
}

func BenchmarkSugarInfof(b *testing.B) {
	sugar := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel)).Sugar()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sugar.Infof("cost=%dms", 123)
	}
}

func BenchmarkLoggerWithCaller(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel), AddCaller())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Info("with caller", benchCallFields()...)
	}
}

func BenchmarkLoggerWithStacktrace(b *testing.B) {
	log := New(slcore.NewCore(mustEncoder(b), slcore.AddSync(io.Discard), InfoLevel),
		AddStacktrace(ErrorLevel))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Info("with stack", benchCallFields()...)
	}
}

func BenchmarkAny(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Any("k", "v")
	}
}

func BenchmarkAtomicLevelSet(b *testing.B) {
	lvl := NewAtomicLevel()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lvl.SetLevel(WarnLevel)
	}
}

// BenchmarkCallInfoAsync 测量异步入队链路（设计文档目标 <5μs）。
func BenchmarkCallInfoAsync(b *testing.B) {
	cfg := NewCallInfoConfig(b.TempDir(), "bench", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.QueueSize = 1000000
	cfg.Rolling.Clock = benchClock{}
	log, err := cfg.Build()
	if err != nil {
		b.Fatal(err)
	}
	ctx := WithTrace(context.Background(), "tid", "sid")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.CallInfo(ctx, CallInfo{
			Mobile: "18237438309", UserID: "1071748417",
			ClientID: "c6559e74a24df8d97a2296ff1e23387a",
			URL:      "http://play.example.com:443/playurl/v1/play/playurl",
			Method:   "GET", UseTime: 123,
			ServerIP: "127.0.0.1", BussID: "null",
			LogMsg: "^MG.getContent:[690894368]",
		})
	}
	b.StopTimer()
	log.Close()
}
