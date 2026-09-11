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
	"sync/atomic"
)

// traceKey is the context key used by WithTrace.
type traceKey struct{}

// Trace holds per-request tracing information.
type Trace struct {
	TraceID string
	SpanID  string
}

// WithTrace stores trace/span ids into the context.
func WithTrace(ctx context.Context, traceID, spanID string) context.Context {
	return context.WithValue(ctx, traceKey{}, Trace{TraceID: traceID, SpanID: spanID})
}

// TraceFromContext extracts the trace stored by WithTrace.
func TraceFromContext(ctx context.Context) (traceID, spanID string) {
	if ctx == nil {
		return "", ""
	}
	if t, ok := ctx.Value(traceKey{}).(Trace); ok {
		return t.TraceID, t.SpanID
	}
	return "", ""
}

// traceExtractorFunc adapts a function to the TraceExtractor interface.
type traceExtractorFunc func(ctx context.Context) (string, string)

func (f traceExtractorFunc) Extract(ctx context.Context) (string, string) {
	return f(ctx)
}

// globalExtractor stores the installed *TraceExtractor, or nil when unset.
var globalExtractor atomic.Pointer[TraceExtractor]

// SetTraceExtractor installs a custom extractor used by CallInfo/RequestInfo
// to resolve trace_id/span_id from the request context. Installing an
// extractor takes precedence over WithTrace. Passing nil removes any
// previously installed extractor.
func SetTraceExtractor(ex TraceExtractor) {
	if ex == nil {
		globalExtractor.Store(nil)
		return
	}
	globalExtractor.Store(&ex)
}

// SetTraceExtractorFunc is a convenience wrapper around SetTraceExtractor.
func SetTraceExtractorFunc(fn func(ctx context.Context) (string, string)) {
	SetTraceExtractor(traceExtractorFunc(fn))
}

func traceFromContext(ctx context.Context) (traceID, spanID string) {
	if p := globalExtractor.Load(); p != nil && *p != nil {
		if id, sid := (*p).Extract(ctx); id != "" || sid != "" {
			return id, sid
		}
	}
	return TraceFromContext(ctx)
}
