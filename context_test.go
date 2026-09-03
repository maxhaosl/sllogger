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
	"testing"
)

func TestWithTrace(t *testing.T) {
	ctx := WithTrace(context.Background(), "tid", "sid")
	gotTID, gotSID := TraceFromContext(ctx)
	if gotTID != "tid" || gotSID != "sid" {
		t.Fatalf("trace = (%q, %q)", gotTID, gotSID)
	}
}

func TestTraceFromContextMissing(t *testing.T) {
	gotTID, gotSID := TraceFromContext(context.Background())
	if gotTID != "" || gotSID != "" {
		t.Fatalf("trace = (%q, %q), want empty", gotTID, gotSID)
	}
}

func TestTraceFromNilContext(t *testing.T) {
	// A nil context must not panic.
	gotTID, gotSID := TraceFromContext(nil)
	if gotTID != "" || gotSID != "" {
		t.Fatalf("trace = (%q, %q)", gotTID, gotSID)
	}
}

func TestTraceExtractorTakesPrecedence(t *testing.T) {
	defer SetTraceExtractor(nil)

	SetTraceExtractorFunc(func(ctx context.Context) (string, string) {
		return "otel-tid", "otel-sid"
	})
	ctx := WithTrace(context.Background(), "ctx-tid", "ctx-sid")
	gotTID, gotSID := traceFromContext(ctx)
	if gotTID != "otel-tid" || gotSID != "otel-sid" {
		t.Fatalf("trace = (%q, %q), want the extractor's values", gotTID, gotSID)
	}
}

func TestTraceExtractorFallsBackOnEmpty(t *testing.T) {
	defer SetTraceExtractor(nil)

	// An extractor that finds nothing falls back to the context values.
	SetTraceExtractorFunc(func(ctx context.Context) (string, string) { return "", "" })
	ctx := WithTrace(context.Background(), "ctx-tid", "ctx-sid")
	gotTID, gotSID := traceFromContext(ctx)
	if gotTID != "ctx-tid" || gotSID != "ctx-sid" {
		t.Fatalf("trace = (%q, %q)", gotTID, gotSID)
	}
}

type testExtractor struct{ id, span string }

func (e testExtractor) Extract(context.Context) (string, string) { return e.id, e.span }

func TestSetTraceExtractorInterface(t *testing.T) {
	defer SetTraceExtractor(nil)
	SetTraceExtractor(testExtractor{id: "i", span: "s"})
	if gotTID, gotSID := traceFromContext(context.Background()); gotTID != "i" || gotSID != "s" {
		t.Fatalf("trace = (%q, %q)", gotTID, gotSID)
	}
}

func TestInfoCtxInjectsTrace(t *testing.T) {
	log, ws := testLogger(t)
	ctx := WithTrace(context.Background(), "tid", "sid")

	log.InfoCtx(ctx, "with trace")
	log.WarnCtx(ctx, "warn with trace")
	log.ErrorCtx(ctx, "error with trace")

	lines := ws.lines()
	for i, line := range lines {
		if !contains(line, `"trace_id":"tid"`) || !contains(line, `"span_id":"sid"`) {
			t.Errorf("line %d = %s, missing trace fields", i, line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
}
