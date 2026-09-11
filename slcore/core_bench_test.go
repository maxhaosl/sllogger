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

package slcore

import (
	"bytes"
	"testing"
	"time"
)

func coreBenchEntry() Entry {
	return Entry{Level: InfoLevel, Message: "x", Time: time.Now()}
}

func BenchmarkSamplerCore(b *testing.B) {
	core := NewSampler(NewNopCore(), time.Second, 1, 1)
	ent := coreBenchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := core.Check(ent, nil)
		if ce != nil {
			ce.Write()
		}
	}
}

func BenchmarkTeeCore(b *testing.B) {
	core := NewTee(NewNopCore(), NewNopCore())
	ent := coreBenchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := core.Check(ent, nil)
		if ce != nil {
			ce.Write()
		}
	}
}

// benchEnabledCore is a minimal Core that enables every level at or above the
// configured floor, with a no-op Write — enough to benchmark core wrappers
// without pulling in an encoder (which would create an import cycle in slcore).
type benchEnabledCore struct{ floor Level }

func (c benchEnabledCore) Enabled(l Level) bool { return l >= c.floor }
func (c benchEnabledCore) Level() Level         { return c.floor }
func (c benchEnabledCore) With([]Field) Core    { return c }
func (c benchEnabledCore) Check(ent Entry, ce *CheckedEntry) *CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}
func (c benchEnabledCore) Write(Entry, []Field) error { return nil }
func (c benchEnabledCore) Sync() error                { return nil }

func BenchmarkIncreaseLevelCore(b *testing.B) {
	// Base core enables everything; raising the floor to WarnLevel is valid and
	// exercises the fast (filtered) path for lower-severity entries.
	core, err := NewIncreaseLevelCore(benchEnabledCore{floor: DebugLevel}, WarnLevel)
	if err != nil {
		b.Fatal(err)
	}
	ent := Entry{Level: InfoLevel, Message: "x", Time: time.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := core.Check(ent, nil)
		if ce != nil {
			ce.Write()
		}
	}
}

func BenchmarkLazyWithCore(b *testing.B) {
	core := NewLazyWith(NewNopCore(), []Field{})
	ent := coreBenchEntry()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := core.Check(ent, nil)
		if ce != nil {
			ce.Write()
		}
	}
}

func BenchmarkMapObjectEncoder(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMapObjectEncoder()
		m.AddString("name", "value")
		m.AddInt("count", i)
		m.AddBool("ok", true)
	}
}

func BenchmarkReflectedEncoder(b *testing.B) {
	var buf bytes.Buffer
	enc := defaultReflectedEncoder(&buf)
	data := map[string]int{"a": 1, "b": 2}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = enc.Encode(data)
	}
}
