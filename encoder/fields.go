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

// Package encoder implements the content-template encoder of sllogger.
//
// 模板形如：
//
//	date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
//
// 渲染时每个字段按名称解析：
//
//   - date / log_level 等来自日志事件本身（Entry）；
//   - 其余字段按 key 从结构化字段（With 注入 + 调用点字段）中查找；
//   - 未找到的字段输出 NullValue（默认 "null"）。
//
// 业务可通过 RegisterRenderer 注册自定义字段渲染器。
package encoder

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/maxhaosl/sllogger/buffer"
	"github.com/maxhaosl/sllogger/slcore"
)

// Common field names used by the preset templates.
const (
	FieldDate        = "date"
	FieldLogLevel    = "log_level"
	FieldServiceID   = "service_id"
	FieldTraceID     = "trace_id"
	FieldSpanID      = "span_id"
	FieldMobile      = "mobile"
	FieldUserID      = "userId"
	FieldClientID    = "clientId"
	FieldURL         = "url"
	FieldMethod      = "method"
	FieldUseTime     = "useTime"
	FieldServerIP    = "serverIp"
	FieldBussID      = "buss_Id"
	FieldLogMsg      = "log_msg"
	FieldHeader      = "header"
	FieldReqHeader   = "reqheader"
	FieldRateLimiter = "rateLimiter"
	FieldContextPath = "contextPath"
	FieldLoggerName  = "logger_name"
	FieldCaller      = "caller"
)

// Renderer renders one template field into its textual representation.
type Renderer func(ctx RenderContext) string

// RenderContext carries everything a renderer may need.
type RenderContext struct {
	// Entry is the log event being encoded.
	Entry slcore.Entry
	// Fields holds the structured fields attached to the entry (both fields
	// added via With and fields given at the call site).
	Fields []slcore.Field
	// Config is the encoder configuration.
	Config *TemplateConfig
}

// lookup returns the string value of the named field, or "" when absent.
// Only scalar field types are rendered; complex types fall back to "null".
//
// Fields are looked up linearly on purpose: templates carry a handful of
// fields, and a linear scan avoids allocating a map on every log entry.
func (rc *RenderContext) lookup(name string) string {
	for i := range rc.Fields {
		f := &rc.Fields[i]
		if f.Key != name {
			continue
		}
		switch f.Type {
		case slcore.StringType:
			return f.String
		case slcore.BoolType:
			return strconv.FormatBool(f.Integer == 1)
		case slcore.Int64Type, slcore.Int32Type, slcore.Int16Type, slcore.Int8Type:
			return strconv.FormatInt(f.Integer, 10)
		case slcore.Uint64Type, slcore.Uint32Type, slcore.Uint16Type, slcore.Uint8Type:
			return strconv.FormatUint(uint64(f.Integer), 10)
		case slcore.DurationType:
			return strconv.FormatInt(time.Duration(f.Integer).Milliseconds(), 10)
		case slcore.Float64Type:
			return strconv.FormatFloat(math.Float64frombits(uint64(f.Integer)), 'f', -1, 64)
		case slcore.Float32Type:
			return strconv.FormatFloat(float64(math.Float32frombits(uint32(f.Integer))), 'f', -1, 32)
		}
	}
	return ""
}

// builtinRenderers resolve fields that come from the log event itself.
var builtinRenderers = map[string]Renderer{
	// date: formatted per TemplateConfig.TimeLayout
	// (yyyy-MM-dd HH:mm:ss.SSS by default).
	FieldDate: func(rc RenderContext) string {
		return rc.Entry.Time.Format(rc.Config.TimeLayout)
	},
	// log_level: INFO/DEBUG/ERROR/WARN style by default.
	FieldLogLevel: func(rc RenderContext) string {
		if rc.Config.LowercaseLevel {
			return rc.Entry.Level.String()
		}
		return rc.Entry.Level.CapitalString()
	},
	// service_id: fixed service identity, may be overridden by fields.
	FieldServiceID: func(rc RenderContext) string {
		if v := rc.lookup(FieldServiceID); v != "" {
			return v
		}
		return rc.Config.ServiceID
	},
	FieldLoggerName: func(rc RenderContext) string {
		return rc.Entry.LoggerName
	},
	FieldCaller: func(rc RenderContext) string {
		return rc.Entry.Caller.TrimmedPath()
	},
	// log_msg: fields["log_msg"] wins over the entry message.
	FieldLogMsg: func(rc RenderContext) string {
		if v := rc.lookup(FieldLogMsg); v != "" {
			return v
		}
		return rc.Entry.Message
	},
}

// EscapeField escapes separator-sensitive characters so downstream parsers can
// split by "|" reliably:
//
//	|  -> \|      \r -> \r
//	\n -> \n     \  -> \\
func EscapeField(value string) string {
	if !strings.ContainsAny(value, "|\n\r\\") {
		return value
	}
	var sb strings.Builder
	sb.Grow(len(value) + 8)
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '|':
			sb.WriteString(`\|`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\\':
			sb.WriteString(`\\`)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// sanitize returns the output representation of a field value: empty values
// become NullValue and the result is escaped unless NoEscape is set
// (design doc #15/#16).
func sanitize(cfg *TemplateConfig, value string) string {
	if value == "" {
		return cfg.NullValue
	}
	if !cfg.NoEscape {
		return EscapeField(value)
	}
	return value
}

// appendSanitized writes the field value straight into buf, avoiding the
// intermediate string allocation that sanitize() would incur.
func appendSanitized(buf *buffer.Buffer, cfg *TemplateConfig, value string) {
	if value == "" {
		buf.AppendString(cfg.NullValue)
		return
	}
	if cfg.NoEscape {
		buf.AppendString(value)
		return
	}
	appendEscaped(buf, value)
}

// appendEscaped writes value into buf, escaping separator-sensitive
// characters. Runs of ordinary characters are copied in bulk.
func appendEscaped(buf *buffer.Buffer, value string) {
	for len(value) > 0 {
		i := strings.IndexAny(value, escapedChars)
		if i < 0 {
			buf.AppendString(value)
			return
		}
		if i > 0 {
			buf.AppendString(value[:i])
		}
		switch value[i] {
		case '|':
			buf.AppendString(`\|`)
		case '\n':
			buf.AppendString(`\n`)
		case '\r':
			buf.AppendString(`\r`)
		default: // '\\'
			buf.AppendString(`\\`)
		}
		value = value[i+1:]
	}
}

// escapedChars are the characters that must be escaped to keep "|"
// separated parsing unambiguous.
const escapedChars = "|\n\r\\"
