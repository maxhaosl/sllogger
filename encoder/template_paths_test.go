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
	"math"
	"testing"

	"github.com/maxhaosl/sllogger/slcore"
)

// TestAppendFieldValueFloat 覆盖 appendFieldValue 中 Float64 / Float32
// 两个分支（此前未被覆盖）。
func TestAppendFieldValueFloat(t *testing.T) {
	cfg := &TemplateConfig{NullValue: "null"}
	pool := newTestPool()

	fields := []slcore.Field{
		{Key: "f64", Type: slcore.Float64Type, Integer: int64(math.Float64bits(2.5))},
		{Key: "f32", Type: slcore.Float32Type, Integer: int64(math.Float32bits(1.25))},
	}

	buf := pool.Get()
	if !appendFieldValue(buf, fields, "f64", cfg) {
		t.Fatal("f64 should be renderable")
	}
	got := buf.String()
	buf.Free()
	if got != "2.5" {
		t.Fatalf("f64 = %q, want 2.5", got)
	}

	buf = pool.Get()
	if !appendFieldValue(buf, fields, "f32", cfg) {
		t.Fatal("f32 should be renderable")
	}
	got = buf.String()
	buf.Free()
	if got != "1.25" {
		t.Fatalf("f32 = %q, want 1.25", got)
	}
}

// TestRenderFieldDynamicPaths 覆盖 renderField 中"调用点字段"与
// "With 上下文"两条查找路径。
func TestRenderFieldDynamicPaths(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "url",
		ServiceID: "svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 未设置任何来源 -> 空串。
	if got := enc.renderField("url", &RenderContext{Config: enc.cfg}); got != "" {
		t.Fatalf("no source: got %q, want empty", got)
	}

	// 路径 1：调用点字段命中。
	rc := &RenderContext{
		Config: enc.cfg,
		Fields: []slcore.Field{{Key: "url", Type: slcore.StringType, String: "from-fields"}},
	}
	if got := enc.renderField("url", rc); got != "from-fields" {
		t.Fatalf("call-site field: got %q", got)
	}

	// 路径 2：With 上下文命中（无调用点字段时）。
	child := enc.Clone().(*TemplateEncoder)
	child.AddString("url", "from-with")
	if got := child.renderField("url", &RenderContext{Config: child.cfg}); got != "from-with" {
		t.Fatalf("With context: got %q", got)
	}

	// 调用点字段优先于 With 上下文。
	rc2 := &RenderContext{
		Config: child.cfg,
		Fields: []slcore.Field{{Key: "url", Type: slcore.StringType, String: "fields-win"}},
	}
	if got := child.renderField("url", rc2); got != "fields-win" {
		t.Fatalf("precedence: got %q, want fields-win", got)
	}
}

// TestServiceIDOverriddenByCallSiteField 覆盖 service_id 被调用点字段
// 覆盖的分支（makeAppender 的 FieldServiceID 分支）。
func TestServiceIDOverriddenByCallSiteField(t *testing.T) {
	enc, err := NewTemplateEncoder(TemplateConfig{
		Template:  "service_id",
		ServiceID: "configured",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 无覆盖 -> 用配置值。
	if got := encodeToString(t, enc, slcore.Entry{}, nil); got != "configured\n" {
		t.Fatalf("got %q, want configured", got)
	}

	// 调用点字段覆盖。
	fields := []slcore.Field{
		{Key: FieldServiceID, Type: slcore.StringType, String: "overridden"},
	}
	if got := encodeToString(t, enc, slcore.Entry{}, fields); got != "overridden\n" {
		t.Fatalf("got %q, want overridden", got)
	}

	// With 上下文覆盖（当没有调用点字段时）。
	child := enc.Clone().(*TemplateEncoder)
	child.AddString(FieldServiceID, "from-with")
	if got := encodeToString(t, child, slcore.Entry{}, nil); got != "from-with\n" {
		t.Fatalf("got %q, want from-with", got)
	}
}
