// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to obtain a copy of
// this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and/or sell copies of the Software, and to permit
// persons to whom the Software is distributed under the terms of this license,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package encoder

import (
	"encoding/json"
	"math"
	"time"
	"unicode/utf8"

	"github.com/maxhaosl/sllogger/buffer"
	"github.com/maxhaosl/sllogger/internal/bufferpool"
	"github.com/maxhaosl/sllogger/slcore"
)

// JSONEncoder is a lightweight one-line JSON encoder implementing
// slcore.Encoder. It covers general-purpose logging; the | separated
// CALL_INFO format is handled by TemplateEncoder.
//
// Unlike the previous implementation (which built a map[string]interface{} and
// ran it through encoding/json on every entry), this encoder streams directly
// into a pooled buffer. That removes the per-entry map allocation and the
// reflection pass, and avoids sharing mutable state across concurrent encodes.
type JSONEncoder struct {
	cfg slcore.EncoderConfig
	// ctx holds the With-context fields, populated once at With time. It is
	// serialized (json.Marshal) into ctxBuf whenever it changes, so the per-entry
	// EncodeEntry hot path only appends the precomputed bytes instead of building
	// and reflecting over a map on every log line.
	ctx mapObjectEncoder
	// ctxBuf is the cached JSON object body of ctx, e.g. "service":"playurl","port":8080.
	ctxBuf []byte
}

// NewJSONEncoder returns a JSONEncoder for the given config.
//
// Keys are honoured exactly as configured: a key equal to slcore.OmitKey
// removes that portion of the entry from the output. Callers that want the
// conventional key names should start from DefaultJSONEncoderConfig.
func NewJSONEncoder(cfg slcore.EncoderConfig) *JSONEncoder {
	return &JSONEncoder{cfg: cfg, ctx: mapObjectEncoder{}}
}

// DefaultJSONEncoderConfig returns the conventional JSON key names
// ("level", "ts", "logger", "caller", "msg", "stacktrace") with the default
// line ending.
func DefaultJSONEncoderConfig() slcore.EncoderConfig {
	return slcore.EncoderConfig{
		TimeKey:       "ts",
		LevelKey:      "level",
		NameKey:       "logger",
		CallerKey:     "caller",
		MessageKey:    "msg",
		StacktraceKey: "stacktrace",
		LineEnding:    slcore.DefaultLineEnding,
	}
}

// Clone implements slcore.Encoder.
func (e *JSONEncoder) Clone() slcore.Encoder {
	clone := *e
	clone.ctx = make(mapObjectEncoder, len(e.ctx))
	for k, v := range e.ctx {
		clone.ctx[k] = v
	}
	clone.ctxBuf = append([]byte(nil), e.ctxBuf...)
	return &clone
}

// EncodeEntry implements slcore.Encoder.
func (e *JSONEncoder) EncodeEntry(ent slcore.Entry, fields []slcore.Field) (*buffer.Buffer, error) {
	buf := bufferpool.Get()
	je := &jsonEncoder{buf: buf}

	buf.AppendByte('{')
	if len(e.ctxBuf) > 0 {
		buf.AppendBytes(e.ctxBuf)
		je.wroteAny = true
	}
	e.encodeEntryFields(je, ent)
	for i := range fields {
		fields[i].AddTo(je)
	}
	buf.AppendByte('}')
	buf.AppendString(lineEndingOr(e.cfg.LineEnding))
	return buf, nil
}

// encodeEntryFields writes the standard entry-level fields (level, ts, logger,
// caller, msg, stacktrace) honouring the configured key names and encoders.
func (e *JSONEncoder) encodeEntryFields(je *jsonEncoder, ent slcore.Entry) {
	if k := e.cfg.LevelKey; k != slcore.OmitKey {
		je.addKey(k)
		if e.cfg.EncodeLevel != nil {
			je.writeValue(func(v slcore.PrimitiveArrayEncoder) { e.cfg.EncodeLevel(ent.Level, v) })
		} else {
			appendJSONString(je.buf, ent.Level.CapitalString())
		}
	}
	if k := e.cfg.TimeKey; k != slcore.OmitKey && !ent.Time.IsZero() {
		je.addKey(k)
		if e.cfg.EncodeTime != nil {
			je.writeValue(func(v slcore.PrimitiveArrayEncoder) { e.cfg.EncodeTime(ent.Time, v) })
		} else {
			appendJSONString(je.buf, ent.Time.Format(time.RFC3339Nano))
		}
	}
	if k := e.cfg.NameKey; k != slcore.OmitKey && ent.LoggerName != "" {
		je.addKey(k)
		appendJSONString(je.buf, ent.LoggerName)
	}
	if k := e.cfg.CallerKey; k != slcore.OmitKey && ent.Caller.Defined {
		je.addKey(k)
		appendJSONString(je.buf, ent.Caller.TrimmedPath())
	}
	if k := e.cfg.MessageKey; k != slcore.OmitKey && ent.Message != "" {
		je.addKey(k)
		appendJSONString(je.buf, ent.Message)
	}
	if k := e.cfg.StacktraceKey; k != slcore.OmitKey && ent.Stack != "" {
		je.addKey(k)
		appendJSONString(je.buf, ent.Stack)
	}
}

// rebuildCtxBuf re-serializes the With-context map into ctxBuf. It is called
// only when context changes (i.e. at With time), never on the encode hot path.
func (e *JSONEncoder) rebuildCtxBuf() {
	if len(e.ctx) == 0 {
		e.ctxBuf = nil
		return
	}
	b, err := json.Marshal(e.ctx)
	if err != nil {
		e.ctxBuf = nil
		return
	}
	// Drop the surrounding braces so the fragment can be embedded as the body
	// of the entry object.
	if len(b) >= 2 {
		b = b[1 : len(b)-1]
	}
	e.ctxBuf = b
}

// --- With-context accumulation (implements slcore.ObjectEncoder) ----------

func (e *JSONEncoder) AddArray(key string, arr slcore.ArrayMarshaler) error {
	err := e.ctx.AddArray(key, arr)
	e.rebuildCtxBuf()
	return err
}
func (e *JSONEncoder) AddObject(key string, obj slcore.ObjectMarshaler) error {
	err := e.ctx.AddObject(key, obj)
	e.rebuildCtxBuf()
	return err
}
func (e *JSONEncoder) AddBinary(key string, val []byte) {
	e.ctx.AddBinary(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddByteString(key string, val []byte) {
	e.ctx.AddByteString(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddBool(key string, val bool) {
	e.ctx.AddBool(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddComplex128(key string, val complex128) {
	e.ctx.AddComplex128(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddComplex64(key string, val complex64) {
	e.ctx.AddComplex64(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddDuration(key string, val time.Duration) {
	e.ctx.AddDuration(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddFloat64(key string, val float64) {
	e.ctx.AddFloat64(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddFloat32(key string, val float32) {
	e.ctx.AddFloat32(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddInt(key string, val int) {
	e.ctx.AddInt(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddInt64(key string, val int64) {
	e.ctx.AddInt64(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddInt32(key string, val int32) {
	e.ctx.AddInt32(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddInt16(key string, val int16) {
	e.ctx.AddInt16(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddInt8(key string, val int8) {
	e.ctx.AddInt8(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddString(key string, val string) {
	e.ctx.AddString(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddTime(key string, val time.Time) {
	e.ctx.AddTime(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUint(key string, val uint) {
	e.ctx.AddUint(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUint64(key string, val uint64) {
	e.ctx.AddUint64(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUint32(key string, val uint32) {
	e.ctx.AddUint32(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUint16(key string, val uint16) {
	e.ctx.AddUint16(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUint8(key string, val uint8) {
	e.ctx.AddUint8(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddUintptr(key string, val uintptr) {
	e.ctx.AddUintptr(key, val)
	e.rebuildCtxBuf()
}
func (e *JSONEncoder) AddReflected(key string, value interface{}) error {
	err := e.ctx.AddReflected(key, value)
	e.rebuildCtxBuf()
	return err
}
func (e *JSONEncoder) OpenNamespace(key string) {}

// ---------------------------------------------------------------------------
// jsonEncoder streams JSON straight into a buffer. It implements both
// slcore.ObjectEncoder (keyed fields) and slcore.ArrayEncoder (array elements);
// the same buffer is shared by nested containers so they write in place.
// ---------------------------------------------------------------------------

type jsonEncoder struct {
	buf      *buffer.Buffer
	wroteAny bool
}

func (je *jsonEncoder) sep() {
	if je.wroteAny {
		je.buf.AppendByte(',')
	}
	je.wroteAny = true
}

func (je *jsonEncoder) addKey(key string) {
	je.sep()
	appendJSONString(je.buf, key)
	je.buf.AppendByte(':')
}

// writeValue renders a custom Level/Time encoder's output (which is a bare JSON
// value, not a keyed field) into the buffer without a leading separator.
func (je *jsonEncoder) writeValue(enc func(v slcore.PrimitiveArrayEncoder)) {
	tmp := bufferpool.Get()
	ve := &jsonEncoder{buf: tmp}
	enc(ve)
	je.buf.AppendBytes(tmp.Bytes())
	tmp.Free()
}

func (je *jsonEncoder) appendFloat(f float64, bits int) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		je.buf.AppendString("null")
		return
	}
	je.buf.AppendFloat(f, bits)
}

// --- ObjectEncoder (keyed) -------------------------------------------------

func (je *jsonEncoder) AddString(key, val string) { je.addKey(key); appendJSONString(je.buf, val) }
func (je *jsonEncoder) AddInt(key string, val int) { je.addKey(key); je.buf.AppendInt(int64(val)) }
func (je *jsonEncoder) AddInt64(key string, val int64) { je.addKey(key); je.buf.AppendInt(val) }
func (je *jsonEncoder) AddInt32(key string, val int32) { je.addKey(key); je.buf.AppendInt(int64(val)) }
func (je *jsonEncoder) AddInt16(key string, val int16) { je.addKey(key); je.buf.AppendInt(int64(val)) }
func (je *jsonEncoder) AddInt8(key string, val int8)   { je.addKey(key); je.buf.AppendInt(int64(val)) }
func (je *jsonEncoder) AddUint(key string, val uint)   { je.addKey(key); je.buf.AppendUint(uint64(val)) }
func (je *jsonEncoder) AddUint64(key string, val uint64) { je.addKey(key); je.buf.AppendUint(val) }
func (je *jsonEncoder) AddUint32(key string, val uint32) { je.addKey(key); je.buf.AppendUint(uint64(val)) }
func (je *jsonEncoder) AddUint16(key string, val uint16) { je.addKey(key); je.buf.AppendUint(uint64(val)) }
func (je *jsonEncoder) AddUint8(key string, val uint8)   { je.addKey(key); je.buf.AppendUint(uint64(val)) }
func (je *jsonEncoder) AddUintptr(key string, val uintptr) { je.addKey(key); je.buf.AppendUint(uint64(val)) }
func (je *jsonEncoder) AddBool(key string, val bool)       { je.addKey(key); je.buf.AppendBool(val) }
func (je *jsonEncoder) AddFloat64(key string, val float64) { je.addKey(key); je.appendFloat(val, 64) }
func (je *jsonEncoder) AddFloat32(key string, val float32) { je.addKey(key); je.appendFloat(float64(val), 32) }
func (je *jsonEncoder) AddDuration(key string, val time.Duration) {
	je.addKey(key)
	je.buf.AppendInt(val.Milliseconds())
}
func (je *jsonEncoder) AddTime(key string, val time.Time) {
	je.addKey(key)
	appendJSONString(je.buf, val.Format(time.RFC3339Nano))
}
func (je *jsonEncoder) AddComplex128(key string, val complex128) { je.addKey(key); je.appendFloat(real(val), 64) }
func (je *jsonEncoder) AddComplex64(key string, val complex64)   { je.addKey(key); je.appendFloat(float64(real(val)), 32) }
func (je *jsonEncoder) AddBinary(key string, val []byte) {
	je.addKey(key)
	b, _ := json.Marshal(string(val))
	je.buf.AppendBytes(b)
}
func (je *jsonEncoder) AddByteString(key string, val []byte) { je.addKey(key); appendJSONString(je.buf, string(val)) }
func (je *jsonEncoder) AddObject(key string, obj slcore.ObjectMarshaler) error {
	je.addKey(key)
	je.buf.AppendByte('{')
	sub := &jsonEncoder{buf: je.buf}
	_ = obj.MarshalLogObject(sub)
	je.buf.AppendByte('}')
	return nil
}
func (je *jsonEncoder) AddArray(key string, arr slcore.ArrayMarshaler) error {
	je.addKey(key)
	je.buf.AppendByte('[')
	sub := &jsonEncoder{buf: je.buf}
	_ = arr.MarshalLogArray(sub)
	je.buf.AppendByte(']')
	return nil
}
func (je *jsonEncoder) AddReflected(key string, value interface{}) error {
	je.addKey(key)
	b, err := json.Marshal(value)
	if err != nil {
		je.buf.AppendString(`"<unmarshalable>"`)
		return nil
	}
	je.buf.AppendBytes(b)
	return nil
}
func (je *jsonEncoder) OpenNamespace(key string) {}

// --- ArrayEncoder (elements) ----------------------------------------------

func (je *jsonEncoder) AppendString(v string)    { je.sep(); appendJSONString(je.buf, v) }
func (je *jsonEncoder) AppendInt(v int)           { je.sep(); je.buf.AppendInt(int64(v)) }
func (je *jsonEncoder) AppendInt64(v int64)       { je.sep(); je.buf.AppendInt(v) }
func (je *jsonEncoder) AppendInt32(v int32)       { je.sep(); je.buf.AppendInt(int64(v)) }
func (je *jsonEncoder) AppendInt16(v int16)       { je.sep(); je.buf.AppendInt(int64(v)) }
func (je *jsonEncoder) AppendInt8(v int8)         { je.sep(); je.buf.AppendInt(int64(v)) }
func (je *jsonEncoder) AppendUint(v uint)         { je.sep(); je.buf.AppendUint(uint64(v)) }
func (je *jsonEncoder) AppendUint64(v uint64)     { je.sep(); je.buf.AppendUint(v) }
func (je *jsonEncoder) AppendUint32(v uint32)     { je.sep(); je.buf.AppendUint(uint64(v)) }
func (je *jsonEncoder) AppendUint16(v uint16)     { je.sep(); je.buf.AppendUint(uint64(v)) }
func (je *jsonEncoder) AppendUint8(v uint8)       { je.sep(); je.buf.AppendUint(uint64(v)) }
func (je *jsonEncoder) AppendUintptr(v uintptr)   { je.sep(); je.buf.AppendUint(uint64(v)) }
func (je *jsonEncoder) AppendBool(v bool)         { je.sep(); je.buf.AppendBool(v) }
func (je *jsonEncoder) AppendFloat64(v float64)   { je.sep(); je.appendFloat(v, 64) }
func (je *jsonEncoder) AppendFloat32(v float32)   { je.sep(); je.appendFloat(float64(v), 32) }
func (je *jsonEncoder) AppendDuration(v time.Duration) { je.sep(); je.buf.AppendInt(v.Milliseconds()) }
func (je *jsonEncoder) AppendTime(v time.Time) {
	je.sep()
	appendJSONString(je.buf, v.Format(time.RFC3339Nano))
}
func (je *jsonEncoder) AppendComplex128(v complex128) { je.sep(); je.appendFloat(real(v), 64) }
func (je *jsonEncoder) AppendComplex64(v complex64)   { je.sep(); je.appendFloat(float64(real(v)), 32) }
func (je *jsonEncoder) AppendByteString(v []byte)     { je.sep(); appendJSONString(je.buf, string(v)) }
func (je *jsonEncoder) AppendArray(arr slcore.ArrayMarshaler) error {
	je.sep()
	je.buf.AppendByte('[')
	sub := &jsonEncoder{buf: je.buf}
	_ = arr.MarshalLogArray(sub)
	je.buf.AppendByte(']')
	return nil
}
func (je *jsonEncoder) AppendObject(obj slcore.ObjectMarshaler) error {
	je.sep()
	je.buf.AppendByte('{')
	sub := &jsonEncoder{buf: je.buf}
	_ = obj.MarshalLogObject(sub)
	je.buf.AppendByte('}')
	return nil
}
func (je *jsonEncoder) AppendReflected(value interface{}) error {
	je.sep()
	b, err := json.Marshal(value)
	if err != nil {
		je.buf.AppendString(`"<unmarshalable>"`)
		return nil
	}
	je.buf.AppendBytes(b)
	return nil
}

// appendJSONString writes s as a JSON string literal (including the surrounding
// quotes), with HTML escaping and UTF-8 safety matching encoding/json.
func appendJSONString(buf *buffer.Buffer, s string) {
	buf.AppendByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf.AppendString(`\ufffd`)
			i++
			continue
		}
		if r < 0x20 || r == '"' || r == '\\' || r == '<' || r == '>' || r == '&' {
			switch r {
			case '"':
				buf.AppendString(`\"`)
			case '\\':
				buf.AppendString(`\\`)
			case '\n':
				buf.AppendString(`\n`)
			case '\r':
				buf.AppendString(`\r`)
			case '\t':
				buf.AppendString(`\t`)
			case '<':
				buf.AppendString(`\u003c`)
			case '>':
				buf.AppendString(`\u003e`)
			case '&':
				buf.AppendString(`\u0026`)
			default:
				buf.AppendString(`\u00`)
				buf.AppendByte(hexdigits[r>>4])
				buf.AppendByte(hexdigits[r&0xf])
			}
			i += size
			continue
		}
		buf.AppendString(s[i : i+size])
		i += size
	}
	buf.AppendByte('"')
}

const hexdigits = "0123456789abcdef"

func lineEndingOr(le string) string {
	if le == "" {
		return slcore.DefaultLineEnding
	}
	return le
}

// ---------------------------------------------------------------------------
// The following types are retained for backward compatibility with the encoder
// test suite (json_map_test.go / json_with_test.go). They are not used by the
// streaming JSONEncoder, which writes straight into a buffer.
// ---------------------------------------------------------------------------

type primitiveCapture struct{ val interface{} }

func (c *primitiveCapture) AppendBool(v bool)             { c.val = v }
func (c *primitiveCapture) AppendByteString(v []byte)     { c.val = string(v) }
func (c *primitiveCapture) AppendComplex128(v complex128) { c.val = real(v) }
func (c *primitiveCapture) AppendComplex64(v complex64)   { c.val = real(v) }
func (c *primitiveCapture) AppendFloat64(v float64)       { c.val = v }
func (c *primitiveCapture) AppendFloat32(v float32)       { c.val = v }
func (c *primitiveCapture) AppendInt(v int)               { c.val = v }
func (c *primitiveCapture) AppendInt64(v int64)           { c.val = v }
func (c *primitiveCapture) AppendInt32(v int32)           { c.val = v }
func (c *primitiveCapture) AppendInt16(v int16)           { c.val = v }
func (c *primitiveCapture) AppendInt8(v int8)             { c.val = v }
func (c *primitiveCapture) AppendString(v string)         { c.val = v }
func (c *primitiveCapture) AppendUint(v uint)             { c.val = v }
func (c *primitiveCapture) AppendUint64(v uint64)         { c.val = v }
func (c *primitiveCapture) AppendUint32(v uint32)         { c.val = v }
func (c *primitiveCapture) AppendUint16(v uint16)         { c.val = v }
func (c *primitiveCapture) AppendUint8(v uint8)           { c.val = v }
func (c *primitiveCapture) AppendUintptr(v uintptr)       { c.val = uint64(v) }

type mapObjectEncoder map[string]interface{}

func (m *mapObjectEncoder) AddArray(key string, arr slcore.ArrayMarshaler) error {
	ae := &mapArrayEncoder{}
	if err := arr.MarshalLogArray(ae); err != nil {
		return err
	}
	(*m)[key] = ae.items
	return nil
}
func (m *mapObjectEncoder) AddObject(key string, obj slcore.ObjectMarshaler) error {
	sub := map[string]interface{}{}
	if err := obj.MarshalLogObject((*mapObjectEncoder)(&sub)); err != nil {
		return err
	}
	(*m)[key] = sub
	return nil
}
func (m *mapObjectEncoder) AddBinary(key string, val []byte)     { (*m)[key] = string(val) }
func (m *mapObjectEncoder) AddByteString(key string, val []byte) { (*m)[key] = string(val) }
func (m *mapObjectEncoder) AddBool(key string, val bool)         { (*m)[key] = val }
func (m *mapObjectEncoder) AddComplex128(key string, val complex128) {
	(*m)[key] = real(val)
}
func (m *mapObjectEncoder) AddComplex64(key string, val complex64) {
	(*m)[key] = real(val)
}
func (m *mapObjectEncoder) AddDuration(key string, val time.Duration) {
	(*m)[key] = val.Milliseconds()
}
func (m *mapObjectEncoder) AddFloat64(key string, val float64) { (*m)[key] = val }
func (m *mapObjectEncoder) AddFloat32(key string, val float32) { (*m)[key] = val }
func (m *mapObjectEncoder) AddInt(key string, val int)         { (*m)[key] = val }
func (m *mapObjectEncoder) AddInt64(key string, val int64)     { (*m)[key] = val }
func (m *mapObjectEncoder) AddInt32(key string, val int32)     { (*m)[key] = val }
func (m *mapObjectEncoder) AddInt16(key string, val int16)     { (*m)[key] = val }
func (m *mapObjectEncoder) AddInt8(key string, val int8)       { (*m)[key] = val }
func (m *mapObjectEncoder) AddString(key string, val string)   { (*m)[key] = val }
func (m *mapObjectEncoder) AddTime(key string, val time.Time) {
	(*m)[key] = val.Format(time.RFC3339Nano)
}
func (m *mapObjectEncoder) AddUint(key string, val uint)       { (*m)[key] = val }
func (m *mapObjectEncoder) AddUint64(key string, val uint64)   { (*m)[key] = val }
func (m *mapObjectEncoder) AddUint32(key string, val uint32)   { (*m)[key] = val }
func (m *mapObjectEncoder) AddUint16(key string, val uint16)   { (*m)[key] = val }
func (m *mapObjectEncoder) AddUint8(key string, val uint8)     { (*m)[key] = val }
func (m *mapObjectEncoder) AddUintptr(key string, val uintptr) { (*m)[key] = uint64(val) }
func (m *mapObjectEncoder) AddReflected(key string, value interface{}) error {
	(*m)[key] = value
	return nil
}
func (m *mapObjectEncoder) OpenNamespace(key string) {}

type mapArrayEncoder struct {
	items []interface{}
}

func (m *mapArrayEncoder) AppendBool(v bool)             { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendByteString(v []byte)     { m.items = append(m.items, string(v)) }
func (m *mapArrayEncoder) AppendComplex128(v complex128) { m.items = append(m.items, real(v)) }
func (m *mapArrayEncoder) AppendComplex64(v complex64)   { m.items = append(m.items, real(v)) }
func (m *mapArrayEncoder) AppendFloat64(v float64)       { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendFloat32(v float32)       { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendInt(v int)               { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendInt64(v int64)           { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendInt32(v int32)           { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendInt16(v int16)           { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendInt8(v int8)             { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendString(v string)         { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUint(v uint)             { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUint64(v uint64)         { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUint32(v uint32)         { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUint16(v uint16)         { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUint8(v uint8)           { m.items = append(m.items, v) }
func (m *mapArrayEncoder) AppendUintptr(v uintptr)       { m.items = append(m.items, uint64(v)) }
func (m *mapArrayEncoder) AppendDuration(v time.Duration) {
	m.items = append(m.items, v.Milliseconds())
}
func (m *mapArrayEncoder) AppendTime(v time.Time) {
	m.items = append(m.items, v.Format(time.RFC3339Nano))
}
func (m *mapArrayEncoder) AppendArray(arr slcore.ArrayMarshaler) error {
	sub := &mapArrayEncoder{}
	if err := arr.MarshalLogArray(sub); err != nil {
		return err
	}
	m.items = append(m.items, sub.items)
	return nil
}
func (m *mapArrayEncoder) AppendObject(obj slcore.ObjectMarshaler) error {
	sub := map[string]interface{}{}
	if err := obj.MarshalLogObject((*mapObjectEncoder)(&sub)); err != nil {
		return err
	}
	m.items = append(m.items, sub)
	return nil
}
func (m *mapArrayEncoder) AppendReflected(value interface{}) error {
	m.items = append(m.items, value)
	return nil
}

var (
	_ slcore.Encoder       = (*JSONEncoder)(nil)
	_ slcore.ObjectEncoder = (*JSONEncoder)(nil)
	_ slcore.ObjectEncoder = (*jsonEncoder)(nil)
	_ slcore.ArrayEncoder  = (*jsonEncoder)(nil)
)
