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

// Benchmarks derived from go.uber.org/zap/zapcore.
package slcore

import (
	"errors"
	"io"
	"testing"
	"time"
)

var benchEntry = Entry{
	Level:      InfoLevel,
	Time:       time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local),
	LoggerName: "playurl",
	Message:    "benchmark",
}

func BenchmarkLevelString(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = WarnLevel.String()
	}
}

func BenchmarkLevelCapitalString(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = WarnLevel.CapitalString()
	}
}

func BenchmarkLevelUnmarshalText(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var lvl Level
		_ = lvl.UnmarshalText([]byte("warn"))
	}
}

func BenchmarkFieldAddTo(b *testing.B) {
	field := Field{Key: "trace_id", Type: StringType, String: "3c06e3121c18f6114a2e9f2e38e5b8fe"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		field.AddTo(newMapObjectEncoder())
	}
}

func BenchmarkAddFields(b *testing.B) {
	fields := []Field{
		{Key: "trace_id", Type: StringType, String: "abc"},
		{Key: "span_id", Type: StringType, String: "def"},
		{Key: "useTime", Type: Int64Type, Integer: 123},
	}
	enc := newMapObjectEncoder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addFields(enc, fields)
	}
}

func BenchmarkCheckedEntryWrite(b *testing.B) {
	core := NewCore(newStubEncoder("line"), AddSync(io.Discard), DebugLevel)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ce := core.Check(benchEntry, nil); ce != nil {
			ce.Write()
		}
	}
}

func BenchmarkCheckedEntryWriteWithFields(b *testing.B) {
	core := NewCore(newStubEncoder("line"), AddSync(io.Discard), DebugLevel)
	fields := []Field{
		{Key: "trace_id", Type: StringType, String: "abc"},
		{Key: "useTime", Type: Int64Type, Integer: 123},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ce := core.Check(benchEntry, nil); ce != nil {
			ce.Write(fields...)
		}
	}
}

func BenchmarkCoreWith(b *testing.B) {
	core := NewCore(newStubEncoder("line"), AddSync(io.Discard), DebugLevel)
	fields := []Field{{Key: "k", Type: StringType, String: "v"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = core.With(fields)
	}
}

func BenchmarkMultiWriteSyncer(b *testing.B) {
	ws := NewMultiWriteSyncer(AddSync(io.Discard), AddSync(io.Discard))
	payload := []byte("payload\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws.Write(payload)
	}
}

func BenchmarkEncodeError(b *testing.B) {
	err := errors.New("benchmark error")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = encodeError("k", err, newMapObjectEncoder())
	}
}

func BenchmarkCaptureCaller(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CaptureCaller(0)
	}
}

func BenchmarkCaptureStack(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CaptureStack(0)
	}
}
