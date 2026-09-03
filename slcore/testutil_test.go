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
	"encoding/json"
	"errors"
	"sync"
	"time"

	"sllogger/buffer"
	"sllogger/internal/bufferpool"
)

// Field constructors for tests. The ergonomic constructors live in the root
// sllogger package (which depends on slcore), so tests here build fields
// directly.
func String(key, val string) Field {
	return Field{Key: key, Type: StringType, String: val}
}

func Int64(key string, val int64) Field {
	return Field{Key: key, Type: Int64Type, Integer: val}
}

func Bool(key string, val bool) Field {
	var i int64
	if val {
		i = 1
	}
	return Field{Key: key, Type: BoolType, Integer: i}
}

// mapObjectEncoder is a minimal ObjectEncoder used to assert what fields
// actually reach an encoder.
type mapObjectEncoder map[string]interface{}

func newMapObjectEncoder() mapObjectEncoder { return mapObjectEncoder{} }

func (m mapObjectEncoder) AddArray(key string, arr ArrayMarshaler) error {
	return arr.MarshalLogArray(mapArrayEncoder{key: key, dst: m})
}

func (m mapObjectEncoder) AddObject(key string, obj ObjectMarshaler) error {
	sub := map[string]interface{}{}
	if err := obj.MarshalLogObject(mapObjectEncoder(sub)); err != nil {
		return err
	}
	m[key] = sub
	return nil
}

func (m mapObjectEncoder) AddBinary(key string, val []byte)     { m[key] = string(val) }
func (m mapObjectEncoder) AddByteString(key string, val []byte) { m[key] = string(val) }
func (m mapObjectEncoder) AddBool(key string, val bool)         { m[key] = val }
func (m mapObjectEncoder) AddComplex128(key string, val complex128) {
	m[key] = real(val)
}
func (m mapObjectEncoder) AddComplex64(key string, val complex64) {
	m[key] = real(val)
}
func (m mapObjectEncoder) AddDuration(key string, val time.Duration) {
	m[key] = val.Milliseconds()
}
func (m mapObjectEncoder) AddFloat64(key string, val float64) { m[key] = val }
func (m mapObjectEncoder) AddFloat32(key string, val float32) { m[key] = val }
func (m mapObjectEncoder) AddInt(key string, val int)         { m[key] = val }
func (m mapObjectEncoder) AddInt64(key string, val int64)     { m[key] = val }
func (m mapObjectEncoder) AddInt32(key string, val int32)     { m[key] = val }
func (m mapObjectEncoder) AddInt16(key string, val int16)     { m[key] = val }
func (m mapObjectEncoder) AddInt8(key string, val int8)       { m[key] = val }
func (m mapObjectEncoder) AddString(key string, val string)   { m[key] = val }
func (m mapObjectEncoder) AddTime(key string, val time.Time)  { m[key] = val.UnixNano() }
func (m mapObjectEncoder) AddUint(key string, val uint)       { m[key] = val }
func (m mapObjectEncoder) AddUint64(key string, val uint64)   { m[key] = val }
func (m mapObjectEncoder) AddUint32(key string, val uint32)   { m[key] = val }
func (m mapObjectEncoder) AddUint16(key string, val uint16)   { m[key] = val }
func (m mapObjectEncoder) AddUint8(key string, val uint8)     { m[key] = val }
func (m mapObjectEncoder) AddUintptr(key string, val uintptr) { m[key] = uint64(val) }
func (m mapObjectEncoder) AddReflected(key string, value interface{}) error {
	m[key] = value
	return nil
}
func (m mapObjectEncoder) OpenNamespace(key string) { m[key+".ns"] = true }

type mapArrayEncoder struct {
	key string
	dst mapObjectEncoder
}

func (m mapArrayEncoder) AppendBool(v bool)              { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendByteString(v []byte)      { m.dst[m.key] = string(v) }
func (m mapArrayEncoder) AppendComplex128(v complex128)  { m.dst[m.key] = real(v) }
func (m mapArrayEncoder) AppendComplex64(v complex64)    { m.dst[m.key] = real(v) }
func (m mapArrayEncoder) AppendFloat64(v float64)        { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendFloat32(v float32)        { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendInt(v int)                { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendInt64(v int64)            { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendInt32(v int32)            { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendInt16(v int16)            { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendInt8(v int8)              { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendString(v string)          { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUint(v uint)              { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUint64(v uint64)          { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUint32(v uint32)          { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUint16(v uint16)          { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUint8(v uint8)            { m.dst[m.key] = v }
func (m mapArrayEncoder) AppendUintptr(v uintptr)        { m.dst[m.key] = uint64(v) }
func (m mapArrayEncoder) AppendDuration(v time.Duration) { m.dst[m.key] = v.Milliseconds() }
func (m mapArrayEncoder) AppendTime(v time.Time)         { m.dst[m.key] = v.UnixNano() }
func (m mapArrayEncoder) AppendArray(arr ArrayMarshaler) error {
	return arr.MarshalLogArray(m)
}
func (m mapArrayEncoder) AppendObject(obj ObjectMarshaler) error {
	sub := map[string]interface{}{}
	if err := obj.MarshalLogObject(mapObjectEncoder(sub)); err != nil {
		return err
	}
	m.dst[m.key] = sub
	return nil
}
func (m mapArrayEncoder) AppendReflected(value interface{}) error {
	m.dst[m.key] = value
	return nil
}

// countingWriteSyncer records writes for assertions.
type countingWriteSyncer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  int
	syncs   int
	writeEr error
}

func (w *countingWriteSyncer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeEr != nil {
		return 0, w.writeEr
	}
	w.writes++
	return w.buf.Write(p)
}

func (w *countingWriteSyncer) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncs++
	return nil
}

func (w *countingWriteSyncer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *countingWriteSyncer) counts() (writes, syncs int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes, w.syncs
}

// stubEncoder is an Encoder that emits a fixed line and records calls.
type stubEncoder struct {
	mu       sync.Mutex
	err      error
	out      string
	encodeN  int
	cloneN   int
	fields   []Field
	context  map[string]interface{}
	withN    int
	omitLine bool
}

func newStubEncoder(out string) *stubEncoder {
	return &stubEncoder{out: out, context: map[string]interface{}{}}
}

func (e *stubEncoder) EncodeEntry(ent Entry, fields []Field) (*buffer.Buffer, error) {
	e.mu.Lock()
	e.encodeN++
	e.fields = append(e.fields, fields...)
	e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	buf := bufferpool.Get()
	buf.AppendString(e.out)
	if !e.omitLine {
		buf.AppendString("\n")
	}
	return buf, nil
}

func (e *stubEncoder) Clone() Encoder {
	e.mu.Lock()
	e.cloneN++
	e.mu.Unlock()
	return e
}

func (e *stubEncoder) AddString(key, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.withN++
	e.context[key] = val
}

func (e *stubEncoder) AddArray(key string, arr ArrayMarshaler) error { return nil }
func (e *stubEncoder) AddObject(key string, obj ObjectMarshaler) error {
	return nil
}
func (e *stubEncoder) AddBinary(key string, val []byte)       { e.AddString(key, string(val)) }
func (e *stubEncoder) AddByteString(key string, val []byte)   { e.AddString(key, string(val)) }
func (e *stubEncoder) AddBool(key string, val bool)           { e.AddString(key, "bool") }
func (e *stubEncoder) AddComplex128(key string, v complex128) {}
func (e *stubEncoder) AddComplex64(key string, v complex64)   {}
func (e *stubEncoder) AddDuration(key string, v time.Duration) {
	e.AddString(key, v.String())
}
func (e *stubEncoder) AddFloat64(key string, v float64) {}
func (e *stubEncoder) AddFloat32(key string, v float32) {}
func (e *stubEncoder) AddInt(key string, v int)         {}
func (e *stubEncoder) AddInt64(key string, v int64)     {}
func (e *stubEncoder) AddInt32(key string, v int32)     {}
func (e *stubEncoder) AddInt16(key string, v int16)     {}
func (e *stubEncoder) AddInt8(key string, v int8)       {}
func (e *stubEncoder) AddTime(key string, v time.Time)  {}
func (e *stubEncoder) AddUint(key string, v uint)       {}
func (e *stubEncoder) AddUint64(key string, v uint64)   {}
func (e *stubEncoder) AddUint32(key string, v uint32)   {}
func (e *stubEncoder) AddUint16(key string, v uint16)   {}
func (e *stubEncoder) AddUint8(key string, v uint8)     {}
func (e *stubEncoder) AddUintptr(key string, v uintptr) {}
func (e *stubEncoder) AddReflected(key string, v interface{}) error {
	return nil
}
func (e *stubEncoder) OpenNamespace(key string) {}

// failingJSON is used to exercise error propagation paths.
var errStub = errors.New("stub failure")

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
