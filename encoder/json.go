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
	"encoding/json"
	"time"

	"sllogger/buffer"
	"sllogger/internal/bufferpool"
	"sllogger/slcore"
)

// JSONEncoder is a lightweight one-line JSON encoder implementing
// slcore.Encoder. It covers general-purpose logging; the | separated
// CALL_INFO format is handled by TemplateEncoder.
type JSONEncoder struct {
	cfg slcore.EncoderConfig
	ctx map[string]interface{}
}

// NewJSONEncoder returns a JSONEncoder for the given config.
//
// Keys are honoured exactly as configured: a key equal to slcore.OmitKey
// removes that portion of the entry from the output. Callers that want the
// conventional key names should start from DefaultJSONEncoderConfig.
func NewJSONEncoder(cfg slcore.EncoderConfig) *JSONEncoder {
	return &JSONEncoder{cfg: cfg, ctx: make(map[string]interface{})}
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
	clone.ctx = make(map[string]interface{}, len(e.ctx))
	for k, v := range e.ctx {
		clone.ctx[k] = v
	}
	return &clone
}

// EncodeEntry implements slcore.Encoder.
func (e *JSONEncoder) EncodeEntry(ent slcore.Entry, fields []slcore.Field) (*buffer.Buffer, error) {
	buf := bufferpool.Get()

	out := make(map[string]interface{}, len(e.ctx)+len(fields)+6)

	if k := e.cfg.LevelKey; k != slcore.OmitKey {
		if e.cfg.EncodeLevel != nil {
			var cap primitiveCapture
			e.cfg.EncodeLevel(ent.Level, &cap)
			out[k] = cap.val
		} else {
			out[k] = ent.Level.CapitalString()
		}
	}
	if k := e.cfg.TimeKey; k != slcore.OmitKey && !ent.Time.IsZero() {
		if e.cfg.EncodeTime != nil {
			var cap primitiveCapture
			e.cfg.EncodeTime(ent.Time, &cap)
			out[k] = cap.val
		} else {
			out[k] = ent.Time.Format(time.RFC3339Nano)
		}
	}
	if k := e.cfg.NameKey; k != slcore.OmitKey && ent.LoggerName != "" {
		out[k] = ent.LoggerName
	}
	if k := e.cfg.CallerKey; k != slcore.OmitKey && ent.Caller.Defined {
		out[k] = ent.Caller.TrimmedPath()
	}
	if k := e.cfg.MessageKey; k != slcore.OmitKey && ent.Message != "" {
		out[k] = ent.Message
	}
	if k := e.cfg.StacktraceKey; k != slcore.OmitKey && ent.Stack != "" {
		out[k] = ent.Stack
	}

	for k, v := range e.ctx {
		out[k] = v
	}
	for i := range fields {
		fields[i].AddTo((*mapObjectEncoder)(&out))
	}

	data, err := json.Marshal(out)
	if err != nil {
		// Never fail the log write because of a bad payload; fall back to a
		// minimal record.
		data, _ = json.Marshal(map[string]interface{}{
			"level": ent.Level.CapitalString(),
			"msg":   "sllogger: json encode error",
		})
	}
	buf.AppendBytes(data)
	buf.AppendString(lineEndingOr(e.cfg.LineEnding))
	return buf, nil
}

// primitiveCapture records the single value emitted by a LevelEncoder or
// TimeEncoder so it can be stored under its configured key.
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

func lineEndingOr(le string) string {
	if le == "" {
		return slcore.DefaultLineEnding
	}
	return le
}

// ---------------------------------------------------------------------------
// ObjectEncoder over a map: used both for With-context accumulation and for
// call-site field rendering via slcore.Field.AddTo.
// ---------------------------------------------------------------------------

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

// mapArrayEncoder collects appended values into a slice so that they
// serialize as a JSON array instead of overwriting a single key.
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

// With-context accumulation on the JSONEncoder itself.
func (e *JSONEncoder) AddArray(key string, arr slcore.ArrayMarshaler) error {
	ae := &mapArrayEncoder{}
	if err := arr.MarshalLogArray(ae); err != nil {
		return err
	}
	e.ctx[key] = ae.items
	return nil
}
func (e *JSONEncoder) AddObject(key string, obj slcore.ObjectMarshaler) error {
	sub := map[string]interface{}{}
	if err := obj.MarshalLogObject((*mapObjectEncoder)(&sub)); err != nil {
		return err
	}
	e.ctx[key] = sub
	return nil
}
func (e *JSONEncoder) AddBinary(key string, val []byte)     { e.ctx[key] = string(val) }
func (e *JSONEncoder) AddByteString(key string, val []byte) { e.ctx[key] = string(val) }
func (e *JSONEncoder) AddBool(key string, val bool)         { e.ctx[key] = val }
func (e *JSONEncoder) AddComplex128(key string, val complex128) {
	e.ctx[key] = real(val)
}
func (e *JSONEncoder) AddComplex64(key string, val complex64) {
	e.ctx[key] = real(val)
}
func (e *JSONEncoder) AddDuration(key string, val time.Duration) {
	e.ctx[key] = val.Milliseconds()
}
func (e *JSONEncoder) AddFloat64(key string, val float64) { e.ctx[key] = val }
func (e *JSONEncoder) AddFloat32(key string, val float32) { e.ctx[key] = val }
func (e *JSONEncoder) AddInt(key string, val int)         { e.ctx[key] = val }
func (e *JSONEncoder) AddInt64(key string, val int64)     { e.ctx[key] = val }
func (e *JSONEncoder) AddInt32(key string, val int32)     { e.ctx[key] = val }
func (e *JSONEncoder) AddInt16(key string, val int16)     { e.ctx[key] = val }
func (e *JSONEncoder) AddInt8(key string, val int8)       { e.ctx[key] = val }
func (e *JSONEncoder) AddString(key string, val string)   { e.ctx[key] = val }
func (e *JSONEncoder) AddTime(key string, val time.Time) {
	e.ctx[key] = val.Format(time.RFC3339Nano)
}
func (e *JSONEncoder) AddUint(key string, val uint)       { e.ctx[key] = val }
func (e *JSONEncoder) AddUint64(key string, val uint64)   { e.ctx[key] = val }
func (e *JSONEncoder) AddUint32(key string, val uint32)   { e.ctx[key] = val }
func (e *JSONEncoder) AddUint16(key string, val uint16)   { e.ctx[key] = val }
func (e *JSONEncoder) AddUint8(key string, val uint8)     { e.ctx[key] = val }
func (e *JSONEncoder) AddUintptr(key string, val uintptr) { e.ctx[key] = uint64(val) }
func (e *JSONEncoder) AddReflected(key string, value interface{}) error {
	e.ctx[key] = value
	return nil
}
func (e *JSONEncoder) OpenNamespace(key string) {}

var (
	_ slcore.Encoder       = (*JSONEncoder)(nil)
	_ slcore.ObjectEncoder = (*JSONEncoder)(nil)
)
