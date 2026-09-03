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

package buffer

import (
	"strconv"
	"testing"
	"time"
)

var sharedPool = NewPool()

func BenchmarkPoolGetPut(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := sharedPool.Get()
		buf.AppendString("x")
		buf.Free()
	}
}

func BenchmarkBufferAppendString(b *testing.B) {
	buf := sharedPool.Get()
	defer buf.Free()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		buf.AppendString("sllogger")
	}
}

func BenchmarkBufferAppendInt(b *testing.B) {
	buf := sharedPool.Get()
	defer buf.Free()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		buf.AppendInt(int64(i))
	}
}

func BenchmarkBufferAppendByte(b *testing.B) {
	buf := sharedPool.Get()
	defer buf.Free()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		buf.AppendByte('|')
	}
}

func BenchmarkBufferAppendTime(b *testing.B) {
	buf := sharedPool.Get()
	defer buf.Free()
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		buf.AppendTime(now, "2006-01-02 15:04:05.000")
	}
}

// BenchmarkBufferVsStrconv compares the pooled buffer against plain
// strconv/string concatenation for reference.
func BenchmarkStrconvAppendInt(b *testing.B) {
	bs := make([]byte, 0, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bs = strconv.AppendInt(bs[:0], int64(i), 10)
	}
	_ = bs
}
