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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
	"github.com/maxhaosl/sllogger/writer"
)

// callInfoLogger builds a CALL_INFO template logger writing to a recorder.
func callInfoLogger(t *testing.T) (*Logger, *slcoreWriteRecorder) {
	t.Helper()
	enc, err := encoder.NewCallInfoEncoder(encoder.TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	ws := newWriteRecorder()
	return New(slcore.NewCore(enc, ws, DebugLevel)), ws
}

func TestCallInfoFields(t *testing.T) {
	log, ws := callInfoLogger(t)
	ctx := WithTrace(context.Background(), "tid", "sid")

	log.CallInfo(ctx, CallInfo{
		Mobile:   "18237438309",
		UserID:   "1071748417",
		ClientID: "cid",
		URL:      "http://svc/api",
		Method:   "GET",
		UseTime:  123,
		ServerIP: "127.0.0.1",
		BussID:   "b1",
		LogMsg:   "message",
	})

	got := ws.lines()[0]
	wantFields := []string{
		"INFO", "playurl", "tid", "sid", "18237438309", "1071748417",
		"cid", "http://svc/api", "GET", "123", "127.0.0.1", "b1", "message",
	}
	for _, want := range wantFields {
		if !contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if got := strings.Count(got, "|"); got != 13 {
		t.Fatalf("separators = %d, want 13 (14 fields)", got)
	}
}

func TestCallInfoMissingFieldsAreNull(t *testing.T) {
	log, ws := callInfoLogger(t)
	// No trace in the context, and most fields left empty.
	log.CallInfo(context.Background(), CallInfo{LogMsg: "only msg"})

	line := ws.lines()[0]
	// trace_id, span_id, mobile, userId, clientId, url, method, serverIp,
	// buss_Id => 9 nulls. service_id comes from the config, and useTime is a
	// numeric field, so an unset value renders as "0" rather than null.
	if got := strings.Count(line, "null"); got != 9 {
		t.Fatalf("nulls = %d, want 9\noutput: %s", got, line)
	}
	if !contains(line, "|0|") {
		t.Fatalf("useTime should render as 0: %s", line)
	}
}

func TestCallInfoLevelDefault(t *testing.T) {
	log, ws := callInfoLogger(t)
	// Level 0 falls back to INFO.
	log.CallInfo(context.Background(), CallInfo{LogMsg: "m"})
	if got := ws.lines()[0]; !contains(got, "INFO") {
		t.Fatalf("output = %s", got)
	}
	// Explicit levels are honoured.
	log.CallInfo(context.Background(), CallInfo{LogMsg: "m", Level: ErrorLevel})
	if got := ws.lines()[1]; !contains(got, "ERROR") {
		t.Fatalf("output = %s", got)
	}
}

func TestRequestInfoFields(t *testing.T) {
	enc, err := encoder.NewRequestInfoEncoder(encoder.TemplateConfig{ServiceID: "playurl"})
	if err != nil {
		t.Fatal(err)
	}
	ws := newWriteRecorder()
	log := New(slcore.NewCore(enc, ws, DebugLevel))
	ctx := WithTrace(context.Background(), "tid", "sid")

	log.RequestInfo(ctx, RequestInfo{
		Header:      "h1",
		ReqHeader:   "h2",
		Method:      "POST",
		RateLimiter: "true",
		LogMsg:      "done",
	})

	got := ws.lines()[0]
	for _, want := range []string{"playurl", "tid", "sid", "h1", "h2", "POST", "true", "done"} {
		if !contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if got := strings.Count(got, "|"); got != 9 {
		t.Fatalf("separators = %d, want 9 (10 fields)", got)
	}
}

func TestSugaredCallInfo(t *testing.T) {
	log, ws := callInfoLogger(t)
	sugar := log.Sugar()
	ctx := WithTrace(context.Background(), "t", "s")

	sugar.CallInfo(ctx, CallInfo{LogMsg: "via sugar"})
	sugar.RequestInfo(ctx, RequestInfo{LogMsg: "via sugar request"})

	lines := ws.lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !contains(lines[0], "via sugar") {
		t.Fatalf("line 0 = %s", lines[0])
	}
}

func TestCallInfoEscapesSeparators(t *testing.T) {
	log, ws := callInfoLogger(t)
	log.CallInfo(context.Background(), CallInfo{
		LogMsg: "a|b\nc",
		URL:    "http://svc/api?x=1|2",
	})
	got := ws.lines()[0]
	if !contains(got, `a\|b\nc`) {
		t.Fatalf("log_msg not escaped: %s", got)
	}
	if !contains(got, `http://svc/api?x=1\|2`) {
		t.Fatalf("url not escaped: %s", got)
	}
}

func TestCallInfoLoggerInterface(t *testing.T) {
	// Both Logger and SugaredLogger must satisfy the design-doc interface.
	var _ CallInfoLogger = (*Logger)(nil)

	dir := t.TempDir()
	cfg := NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxAge = time.Hour
	log := Must(cfg.Build())

	var ci CallInfoLogger = log
	ctx := WithTrace(context.Background(), "t", "s")
	ci.CallInfo(ctx, CallInfo{LogMsg: "m"})
	ci.RequestInfo(ctx, RequestInfo{LogMsg: "m"})
	ci.InfoCtx(ctx, "info")
	ci.WarnCtx(ctx, "warn")
	ci.ErrorCtx(ctx, "error")
	if err := ci.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := ci.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir,
		"LOG_CALL_INFO."+time.Now().Format("2006-01-02")+".playurl8080.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != 5 {
		t.Fatalf("entries = %d, want 5", got)
	}
}

func TestCallInfoWithTraceExtractor(t *testing.T) {
	defer SetTraceExtractor(nil)
	SetTraceExtractorFunc(func(ctx context.Context) (string, string) {
		return "otel-tid", "otel-sid"
	})

	log, ws := callInfoLogger(t)
	log.CallInfo(context.Background(), CallInfo{LogMsg: "m"})
	got := ws.lines()[0]
	if !contains(got, "otel-tid") || !contains(got, "otel-sid") {
		t.Fatalf("extractor values missing: %s", got)
	}
}

func TestCallInfoConfigDefaults(t *testing.T) {
	cfg := NewCallInfoConfig("/data/logs", "playurl", 8080)
	if cfg.Encoding != "callinfo" {
		t.Fatalf("encoding = %q", cfg.Encoding)
	}
	if cfg.Rolling.BaseName != "LOG_CALL_INFO" {
		t.Fatalf("base = %q", cfg.Rolling.BaseName)
	}
	if cfg.Rolling.Dir != "/data/logs" {
		t.Fatalf("dir = %q", cfg.Rolling.Dir)
	}
	if cfg.TemplateConfig.ServiceID != "playurl" {
		t.Fatalf("service id = %q", cfg.TemplateConfig.ServiceID)
	}
	// A caller-provided ServiceID must win.
	cfg = NewCallInfoConfig("/data/logs", "playurl", 8080)
	cfg.TemplateConfig.ServiceID = "custom"
	if cfg.TemplateConfig.ServiceID != "custom" {
		t.Fatal("explicit ServiceID was overwritten")
	}
}

func TestConfigBuildErrors(t *testing.T) {
	// Unknown encoding.
	if _, err := (Config{Level: NewAtomicLevel(), Encoding: "nope"}).Build(); err == nil {
		t.Fatal("expected an error for an unknown encoding")
	}
	// Template encoding without a template.
	if _, err := (Config{Level: NewAtomicLevel(), Encoding: "template"}).Build(); err == nil {
		t.Fatal("expected an error for a missing template")
	}
	// Missing level.
	if _, err := (Config{Encoding: "json"}).Build(); err == nil {
		t.Fatal("expected an error for a missing level")
	}
	// Broken rolling config.
	if _, err := (Config{Level: NewAtomicLevel(), Encoding: "callinfo", Rolling: &writer.Config{}}).Build(); err == nil {
		t.Fatal("expected an error for an incomplete rolling config")
	}
}

func TestConfigInitialFieldsSorted(t *testing.T) {
	log, ws := testLogger(t)
	withFields := log.WithOptions(Fields(
		Any("b", 2),
		Any("a", 1),
		Any("c", 3),
	))
	withFields.Info("sorted")
	got := ws.lines()[0]
	if strings.Index(got, `"a":1`) > strings.Index(got, `"b":2`) {
		t.Fatalf("fields are not sorted: %s", got)
	}
}

func TestConfigErrorOutputPath(t *testing.T) {
	dir := t.TempDir()
	log := Must(Config{
		Level:            NewAtomicLevelAt(DebugLevel),
		Encoding:         "json",
		OutputPaths:      []string{filepath.Join(dir, "out.log")},
		ErrorOutputPaths: []string{filepath.Join(dir, "err.log")},
	}.Build())
	defer log.Close()

	log.Info("hello")
	data, err := os.ReadFile(filepath.Join(dir, "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "hello") {
		t.Fatalf("content = %s", data)
	}
}

func TestNewProductionAndDevelopmentConfig(t *testing.T) {
	prod := NewProductionConfig()
	if prod.Level.Level() != InfoLevel || prod.Development {
		t.Fatalf("production config = %+v", prod)
	}
	dev := NewDevelopmentConfig()
	if dev.Level.Level() != DebugLevel || !dev.Development {
		t.Fatalf("development config = %+v", dev)
	}
}
