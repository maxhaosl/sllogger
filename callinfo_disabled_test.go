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
	"strings"
	"testing"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
)

// disabledCallInfoLogger 返回一个级别为 ErrorLevel 的 CALL_INFO 日志器，
// 用于验证 Info 级别的调用日志会被短路（不做字段构造、不编码、不写盘）。
func disabledCallInfoLogger(t *testing.T) (*Logger, *slcoreWriteRecorder) {
	t.Helper()
	enc, err := encoder.NewCallInfoEncoder(encoder.TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	ws := newWriteRecorder()
	return New(slcore.NewCore(enc, ws, ErrorLevel)), ws
}

// TestCallInfoDisabledLevelShortCircuits 覆盖 CallInfo 在级别未启用时的
// 提前返回分支：这是高频业务路径，必须零开销且不产生任何输出。
func TestCallInfoDisabledLevelShortCircuits(t *testing.T) {
	log, ws := disabledCallInfoLogger(t)
	ctx := WithTrace(context.Background(), "tid", "sid")

	// 默认 Info 级别，而日志器只允许 Error 及以上。
	log.CallInfo(ctx, CallInfo{
		Mobile: "18237438309",
		URL:    "http://svc/api",
		LogMsg: "should be dropped",
	})
	// 显式提升级别则应写入。
	log.CallInfo(ctx, CallInfo{LogMsg: "kept", Level: ErrorLevel})

	lines := ws.lines()
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (only the error-level entry)", len(lines))
	}
	if !contains(lines[0], "kept") {
		t.Fatalf("output = %s", lines[0])
	}
	if contains(lines[0], "should be dropped") {
		t.Fatalf("disabled entry was written: %s", lines[0])
	}
	// 被短路的调用不得残留任何输出，包括空行。
	if strings.TrimSpace(ws.String()) != strings.TrimSpace(lines[0]) {
		t.Fatalf("unexpected extra output: %q", ws.String())
	}
}

// TestRequestInfoDisabledLevelShortCircuits 覆盖 RequestInfo 的同类分支。
func TestRequestInfoDisabledLevelShortCircuits(t *testing.T) {
	enc, err := encoder.NewRequestInfoEncoder(encoder.TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, ErrorLevel))
	ctx := WithTrace(context.Background(), "tid", "sid")

	// 注意：不能用 FatalLevel，它会调用 os.Exit(1) 终止测试进程。
	log.RequestInfo(ctx, RequestInfo{LogMsg: "dropped", ReqHeader: "h"})
	log.RequestInfo(ctx, RequestInfo{LogMsg: "kept", Level: ErrorLevel})

	lines := ws.lines()
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !contains(lines[0], "kept") {
		t.Fatalf("output = %s", lines[0])
	}
}

// TestSugarCallInfoDisabledLevel 覆盖 SugaredLogger 委托路径的短路。
func TestSugarCallInfoDisabledLevel(t *testing.T) {
	log, ws := disabledCallInfoLogger(t)
	sugar := log.Sugar()
	ctx := WithTrace(context.Background(), "t", "s")

	sugar.CallInfo(ctx, CallInfo{LogMsg: "dropped"})
	sugar.RequestInfo(ctx, RequestInfo{LogMsg: "dropped"})

	if got := len(ws.lines()); got != 0 {
		t.Fatalf("lines = %d, want 0", got)
	}
}
