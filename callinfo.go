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
	"sync"

	"github.com/maxhaosl/sllogger/encoder"
)

// CallInfo 描述一次 HTTP/RPC 调用信息（设计文档第 8 节）。业务只提交结构化
// 数据，不允许自行拼接 "|" 分隔字符串（设计文档 5.1）。
//
// 字段 key 与 encoder 预置 CALL_INFO 模板一致：
//
//	date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
type CallInfo struct {
	// Mobile 手机号。
	Mobile string
	// UserID 用户 ID。
	UserID string
	// ClientID 客户端 ID。
	ClientID string

	// URL 请求地址。
	URL string
	// Method HTTP/RPC 方法。
	Method string
	// UseTime 请求耗时（毫秒）。
	UseTime int64

	// ServerIP 服务端 IP。
	ServerIP string
	// BussID 业务 ID。
	BussID string

	// LogMsg 日志消息（请求和响应信息）。
	LogMsg string

	// Level 日志级别，默认 INFO。
	Level Level
}

// RequestInfo 描述请求/响应头信息（设计文档第 9 节）：
//
//	date|log_type|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
type RequestInfo struct {
	// Header 用户请求头和响应头信息。
	Header string
	// ReqHeader 请求头信息。
	ReqHeader string
	// Method 请求方法。
	Method string
	// RateLimiter 服务限流值。
	RateLimiter string
	// ContextPath 服务 contextPath。
	ContextPath string

	// LogMsg 日志消息。
	LogMsg string

	// Level 日志级别，默认 INFO。
	Level Level
}

// Field slices for the call-info paths are pooled: these are the hottest
// paths in the library, and the slices are consumed synchronously by
// CheckedEntry.Write, so they can be recycled as soon as it returns.
//
// Constraint: hooks registered with Hooks must not retain the []Field they
// receive, which mirrors the existing rule that CheckedEntry references are
// not retained after Write.
var (
	callInfoFieldPool = sync.Pool{New: func() any {
		s := make([]Field, 0, 12)
		return &s
	}}
	requestInfoFieldPool = sync.Pool{New: func() any {
		s := make([]Field, 0, 8)
		return &s
	}}
)

// CallInfo records one CALL_INFO log. trace_id/span_id are resolved from ctx
// (WithTrace or a registered TraceExtractor).
func (log *Logger) CallInfo(ctx context.Context, info CallInfo) {
	lvl := info.Level
	if lvl == 0 {
		lvl = InfoLevel
	}
	msg := info.LogMsg
	if ce := log.check(lvl, msg); ce == nil {
		return
	} else {
		fp := callInfoFieldPool.Get().(*[]Field)
		fields := (*fp)[:0]
		appendCallInfoFields(&fields, ctx, info)
		ce.Write(fields...)
		*fp = fields[:0]
		callInfoFieldPool.Put(fp)
	}
}

// RequestInfo records one RequestInfo log.
func (log *Logger) RequestInfo(ctx context.Context, info RequestInfo) {
	lvl := info.Level
	if lvl == 0 {
		lvl = InfoLevel
	}
	msg := info.LogMsg
	if ce := log.check(lvl, msg); ce == nil {
		return
	} else {
		fp := requestInfoFieldPool.Get().(*[]Field)
		fields := (*fp)[:0]
		appendRequestInfoFields(&fields, ctx, info)
		ce.Write(fields...)
		*fp = fields[:0]
		requestInfoFieldPool.Put(fp)
	}
}

func appendCallInfoFields(dst *[]Field, ctx context.Context, info CallInfo) {
	traceID, spanID := traceFromContext(ctx)
	*dst = append(*dst,
		String(encoder.FieldTraceID, traceID),
		String(encoder.FieldSpanID, spanID),
		String(encoder.FieldMobile, info.Mobile),
		String(encoder.FieldUserID, info.UserID),
		String(encoder.FieldClientID, info.ClientID),
		String(encoder.FieldURL, info.URL),
		String(encoder.FieldMethod, info.Method),
		Int64(encoder.FieldUseTime, info.UseTime),
		String(encoder.FieldServerIP, info.ServerIP),
		String(encoder.FieldBussID, info.BussID),
		String(encoder.FieldLogMsg, info.LogMsg),
	)
}

func appendRequestInfoFields(dst *[]Field, ctx context.Context, info RequestInfo) {
	traceID, spanID := traceFromContext(ctx)
	*dst = append(*dst,
		String(encoder.FieldTraceID, traceID),
		String(encoder.FieldSpanID, spanID),
		String(encoder.FieldHeader, info.Header),
		String(encoder.FieldReqHeader, info.ReqHeader),
		String(encoder.FieldMethod, info.Method),
		String(encoder.FieldRateLimiter, info.RateLimiter),
		String(encoder.FieldContextPath, info.ContextPath),
		String(encoder.FieldLogMsg, info.LogMsg),
	)
}

// CallInfo 是 SugaredLogger 上的同名便捷方法。
func (s *SugaredLogger) CallInfo(ctx context.Context, info CallInfo) {
	s.base.CallInfo(ctx, info)
}

// RequestInfo 是 SugaredLogger 上的同名便捷方法。
func (s *SugaredLogger) RequestInfo(ctx context.Context, info RequestInfo) {
	s.base.RequestInfo(ctx, info)
}

// Info records a plain message with trace information from ctx.
func (log *Logger) InfoCtx(ctx context.Context, msg string, fields ...Field) {
	traceID, spanID := traceFromContext(ctx)
	all := make([]Field, 0, len(fields)+2)
	all = append(all,
		String(encoder.FieldTraceID, traceID),
		String(encoder.FieldSpanID, spanID),
	)
	all = append(all, fields...)
	log.Info(msg, all...)
}

// Warn records a plain message with trace information from ctx.
func (log *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	traceID, spanID := traceFromContext(ctx)
	all := make([]Field, 0, len(fields)+2)
	all = append(all,
		String(encoder.FieldTraceID, traceID),
		String(encoder.FieldSpanID, spanID),
	)
	all = append(all, fields...)
	log.Warn(msg, all...)
}

// Error records a plain message with trace information from ctx.
func (log *Logger) ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	traceID, spanID := traceFromContext(ctx)
	all := make([]Field, 0, len(fields)+2)
	all = append(all,
		String(encoder.FieldTraceID, traceID),
		String(encoder.FieldSpanID, spanID),
	)
	all = append(all, fields...)
	log.Error(msg, all...)
}

// compile-time check that *Logger implements the design doc interface.
var _ CallInfoLogger = (*Logger)(nil)
