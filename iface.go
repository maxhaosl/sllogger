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

// Package sllogger is a structured, high-throughput logging library. A Logger
// wraps an slcore.Core and offers ergonomic field constructors, leveled
// logging, sampling, and pluggable sinks/encoders.
//
// # Interface overview
//
// The public surface is intentionally small and composable:
//
//   - Logger      is the concrete logger (a struct, not an interface) built on
//     an slcore.Core; most callers depend on the concrete type, not an interface.
//   - Option      configures a Logger (WrapCore, Level, AddCaller, Hooks, ...).
//   - Sink        is a closeable WriteSyncer — the destination abstraction
//     registered via RegisterSink.
//   - TraceExtractor  pulls trace/span IDs out of a context (e.g. OpenTelemetry).
//   - CallInfoLogger  is the call-info logging surface whose ctx methods
//     auto-extract trace_id/span_id (design doc §11).
//
// Concrete cores, encoders and write-syncer implementations live in the slcore,
// encoder and writer packages; this package wires them together into a Logger.
package sllogger

import (
	"context"
	"io"

	"github.com/maxhaosl/sllogger/slcore"
)

// Option configures a Logger.
type Option interface {
	apply(*Logger)
}

// Sink defines the interface to write to and close logger destinations.
type Sink interface {
	slcore.WriteSyncer
	io.Closer
}

// TraceExtractor pluggably extracts trace information from a context, e.g.
// from OpenTelemetry span contexts.
type TraceExtractor interface {
	Extract(ctx context.Context) (traceID, spanID string)
}

// CallInfoLogger is the interface for call-info logging (design doc §11). The
// ctx-carrying methods automatically extract trace_id/span_id from context.
type CallInfoLogger interface {
	CallInfo(ctx context.Context, info CallInfo)
	RequestInfo(ctx context.Context, info RequestInfo)
	InfoCtx(ctx context.Context, msg string, fields ...Field)
	WarnCtx(ctx context.Context, msg string, fields ...Field)
	ErrorCtx(ctx context.Context, msg string, fields ...Field)
	Sync() error
	Close() error
}
