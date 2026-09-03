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
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"sllogger/buffer"
	"sllogger/internal/bufferpool"
	"sllogger/slcore"
)

// Defaults for the template encoder.
const (
	DefaultTimeLayout = "2006-01-02 15:04:05.000" // yyyy-MM-dd HH:mm:ss.SSS
	DefaultSeparator  = "|"
	DefaultNullValue  = "null"
)

// TemplateConfig configures a TemplateEncoder.
type TemplateConfig struct {
	// Template is the "|" separated field template, e.g.
	//
	//	date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
	//
	// Every segment must be a registered field name.
	Template string

	// Separator separates fields. Defaults to "|".
	Separator string

	// TimeLayout is the Go layout for the date field. Defaults to
	// "2006-01-02 15:04:05.000" (design doc #13).
	TimeLayout string

	// LowercaseLevel renders log_level as "info" instead of "INFO".
	LowercaseLevel bool

	// ServiceID is the fixed value of the service_id field.
	ServiceID string

	// NullValue is emitted for missing/empty fields. Defaults to "null".
	NullValue string

	// Escape enables separator escaping (| -> \|, newline -> \n, ...).
	// Enabled by default; set NoEscape to disable.
	NoEscape bool

	// LineEnding defaults to "\n".
	LineEnding string

	// Renderers registers or overrides field renderers by name.
	Renderers map[string]Renderer
}

func (c *TemplateConfig) withDefaults() *TemplateConfig {
	cp := *c
	if cp.Separator == "" {
		cp.Separator = DefaultSeparator
	}
	if cp.TimeLayout == "" {
		cp.TimeLayout = DefaultTimeLayout
	}
	if cp.NullValue == "" {
		cp.NullValue = DefaultNullValue
	}
	if cp.LineEnding == "" {
		cp.LineEnding = slcore.DefaultLineEnding
	}
	return &cp
}

// fieldAppender renders one template field straight into the output buffer.
// It is resolved once when the encoder is created, so the hot encode path
// performs no map lookups and no intermediate string allocations.
type fieldAppender func(e *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer)

// TemplateEncoder renders entries according to a field template. It
// implements slcore.Encoder.
type TemplateEncoder struct {
	cfg    *TemplateConfig
	fields []string
	// appenders is parallel to fields and is precomputed at construction.
	appenders []fieldAppender

	// context accumulates the values added via With (ioCore.With calls the
	// ObjectEncoder methods below).
	context map[string]string
}

// NewTemplateEncoder parses the template and returns an encoder.
func NewTemplateEncoder(cfg TemplateConfig) (*TemplateEncoder, error) {
	normalized := cfg.withDefaults()
	if strings.TrimSpace(normalized.Template) == "" {
		return nil, errors.New("sllogger/encoder: template is empty")
	}
	cfg = *normalized

	segs := strings.Split(cfg.Template, cfg.Separator)
	fields := make([]string, 0, len(segs))
	appenders := make([]fieldAppender, 0, len(segs))
	for _, seg := range segs {
		name := strings.TrimSpace(seg)
		if name == "" {
			return nil, fmt.Errorf("sllogger/encoder: template contains empty field: %q", cfg.Template)
		}
		fields = append(fields, name)
		appenders = append(appenders, makeAppender(cfg, name))
	}

	return &TemplateEncoder{
		cfg:       &cfg,
		fields:    fields,
		appenders: appenders,
		context:   make(map[string]string),
	}, nil
}

// makeAppender binds one template field to its rendering strategy, in
// priority order: custom renderer, built-in renderer, dynamic field lookup.
//
// Appenders write directly into the entry buffer; only string values (which
// need escaping) may allocate, and only when they actually contain characters
// that must be escaped.
func makeAppender(cfg TemplateConfig, name string) fieldAppender {
	if r, ok := cfg.Renderers[name]; ok && r != nil {
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			appendSanitized(buf, rc.Config, r(*rc))
		}
	}

	switch name {
	case FieldDate:
		// AppendTime formats straight into the buffer: no allocation.
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			buf.AppendTime(rc.Entry.Time, rc.Config.TimeLayout)
		}
	case FieldLogLevel:
		if cfg.LowercaseLevel {
			return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
				buf.AppendString(rc.Entry.Level.String())
			}
		}
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			buf.AppendString(rc.Entry.Level.CapitalString())
		}
	case FieldServiceID:
		// service_id may be overridden by a call-site field.
		return func(e *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			if appendFieldValue(buf, rc.Fields, FieldServiceID, rc.Config) {
				return
			}
			if v := e.context[FieldServiceID]; v != "" {
				appendSanitized(buf, rc.Config, v)
				return
			}
			appendSanitized(buf, rc.Config, rc.Config.ServiceID)
		}
	case FieldLoggerName:
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			appendSanitized(buf, rc.Config, rc.Entry.LoggerName)
		}
	case FieldCaller:
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			trimmed := rc.Entry.Caller.TrimmedPath()
			appendSanitized(buf, rc.Config, trimmed)
		}
	case FieldLogMsg:
		// log_msg: fields["log_msg"] wins over the entry message.
		return func(_ *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
			if appendFieldValue(buf, rc.Fields, FieldLogMsg, rc.Config) {
				return
			}
			appendSanitized(buf, rc.Config, rc.Entry.Message)
		}
	}

	// Dynamic lookup: call-site fields first, then the With context.
	return func(e *TemplateEncoder, rc *RenderContext, buf *buffer.Buffer) {
		if appendFieldValue(buf, rc.Fields, name, rc.Config) {
			return
		}
		appendSanitized(buf, rc.Config, e.context[name])
	}
}

// appendFieldValue writes the value of the named scalar field into buf.
// It reports whether anything was written; callers fall back to the null
// value when it returns false (missing, empty, or non-scalar field).
func appendFieldValue(buf *buffer.Buffer, fields []slcore.Field, name string, cfg *TemplateConfig) bool {
	for i := range fields {
		f := &fields[i]
		if f.Key != name {
			continue
		}
		switch f.Type {
		case slcore.StringType:
			// An empty string is treated as missing, so the caller can fall
			// back to the With context (and finally to null).
			if f.String == "" {
				return false
			}
			if cfg.NoEscape {
				buf.AppendString(f.String)
			} else {
				appendEscaped(buf, f.String)
			}
			return true
		case slcore.BoolType:
			buf.AppendBool(f.Integer == 1)
			return true
		case slcore.Int64Type, slcore.Int32Type, slcore.Int16Type, slcore.Int8Type:
			buf.AppendInt(f.Integer)
			return true
		case slcore.Uint64Type, slcore.Uint32Type, slcore.Uint16Type, slcore.Uint8Type:
			buf.AppendUint(uint64(f.Integer))
			return true
		case slcore.DurationType:
			buf.AppendInt(time.Duration(f.Integer).Milliseconds())
			return true
		case slcore.Float64Type:
			buf.AppendFloat(math.Float64frombits(uint64(f.Integer)), 64)
			return true
		case slcore.Float32Type:
			buf.AppendFloat(float64(math.Float32frombits(uint32(f.Integer))), 32)
			return true
		}
		// Complex types (objects, arrays, reflected values) are not
		// renderable into a positional template.
		return false
	}
	return false
}

// Fields returns the parsed template field names.
func (e *TemplateEncoder) Fields() []string {
	out := make([]string, len(e.fields))
	copy(out, e.fields)
	return out
}

// Clone implements slcore.Encoder.
func (e *TemplateEncoder) Clone() slcore.Encoder {
	clone := *e
	clone.context = make(map[string]string, len(e.context))
	for k, v := range e.context {
		clone.context[k] = v
	}
	return &clone
}

// EncodeEntry implements slcore.Encoder. It renders the template fields in
// order, separated by Separator, and appends the line ending.
func (e *TemplateEncoder) EncodeEntry(ent slcore.Entry, fields []slcore.Field) (*buffer.Buffer, error) {
	buf := bufferpool.Get()

	rc := RenderContext{
		Entry:  ent,
		Fields: fields,
		Config: e.cfg,
	}

	for i := range e.fields {
		if i > 0 {
			buf.AppendString(e.cfg.Separator)
		}
		e.appenders[i](e, &rc, buf)
	}
	buf.AppendString(e.cfg.LineEnding)
	return buf, nil
}

// renderField resolves one template field to a string. It is a convenience
// helper for custom renderers and tests; the encode hot path uses the
// precompiled appenders, which write straight into the output buffer.
func (e *TemplateEncoder) renderField(name string, rc *RenderContext) string {
	// 1. Explicit (custom or overridden) renderer.
	if r, ok := e.cfg.Renderers[name]; ok && r != nil {
		return r(*rc)
	}
	// 2. Built-in renderers (event-driven fields).
	if r, ok := builtinRenderers[name]; ok {
		return r(*rc)
	}
	// 3. Dynamic lookup: call-site fields first, then With context.
	if v := rc.lookup(name); v != "" {
		return v
	}
	if v := e.context[name]; v != "" {
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// ObjectEncoder implementation: values added via With are accumulated into
// the context map and participate in dynamic field lookup.
// ---------------------------------------------------------------------------

func (e *TemplateEncoder) AddArray(key string, arr slcore.ArrayMarshaler) error { return nil }
func (e *TemplateEncoder) AddObject(key string, obj slcore.ObjectMarshaler) error {
	return nil
}
func (e *TemplateEncoder) AddBinary(key string, val []byte)     { e.context[key] = string(val) }
func (e *TemplateEncoder) AddByteString(key string, val []byte) { e.context[key] = string(val) }
func (e *TemplateEncoder) AddBool(key string, val bool)         { e.context[key] = strconv.FormatBool(val) }
func (e *TemplateEncoder) AddComplex128(key string, val complex128) {
	e.context[key] = fmt.Sprint(val)
}
func (e *TemplateEncoder) AddComplex64(key string, val complex64) {
	e.context[key] = fmt.Sprint(val)
}
func (e *TemplateEncoder) AddDuration(key string, val time.Duration) {
	e.context[key] = strconv.FormatInt(val.Milliseconds(), 10)
}
func (e *TemplateEncoder) AddFloat64(key string, val float64) {
	e.context[key] = strconv.FormatFloat(val, 'f', -1, 64)
}
func (e *TemplateEncoder) AddFloat32(key string, val float32) {
	e.context[key] = strconv.FormatFloat(float64(val), 'f', -1, 32)
}
func (e *TemplateEncoder) AddInt(key string, val int) {
	e.context[key] = strconv.Itoa(val)
}
func (e *TemplateEncoder) AddInt64(key string, val int64) {
	e.context[key] = strconv.FormatInt(val, 10)
}
func (e *TemplateEncoder) AddInt32(key string, val int32) {
	e.context[key] = strconv.FormatInt(int64(val), 10)
}
func (e *TemplateEncoder) AddInt16(key string, val int16) {
	e.context[key] = strconv.FormatInt(int64(val), 10)
}
func (e *TemplateEncoder) AddInt8(key string, val int8) {
	e.context[key] = strconv.FormatInt(int64(val), 10)
}
func (e *TemplateEncoder) AddString(key string, val string) { e.context[key] = val }
func (e *TemplateEncoder) AddTime(key string, val time.Time) {
	e.context[key] = val.Format(e.cfg.TimeLayout)
}
func (e *TemplateEncoder) AddUint(key string, val uint) {
	e.context[key] = strconv.FormatUint(uint64(val), 10)
}
func (e *TemplateEncoder) AddUint64(key string, val uint64) {
	e.context[key] = strconv.FormatUint(val, 10)
}
func (e *TemplateEncoder) AddUint32(key string, val uint32) {
	e.context[key] = strconv.FormatUint(uint64(val), 10)
}
func (e *TemplateEncoder) AddUint16(key string, val uint16) {
	e.context[key] = strconv.FormatUint(uint64(val), 10)
}
func (e *TemplateEncoder) AddUint8(key string, val uint8) {
	e.context[key] = strconv.FormatUint(uint64(val), 10)
}
func (e *TemplateEncoder) AddUintptr(key string, val uintptr) {
	e.context[key] = strconv.FormatUint(uint64(val), 10)
}
func (e *TemplateEncoder) AddReflected(key string, value interface{}) error {
	e.context[key] = fmt.Sprint(value)
	return nil
}
func (e *TemplateEncoder) OpenNamespace(key string) {}

// compile-time interface checks.
var (
	_ slcore.Encoder       = (*TemplateEncoder)(nil)
	_ slcore.ObjectEncoder = (*TemplateEncoder)(nil)
)
