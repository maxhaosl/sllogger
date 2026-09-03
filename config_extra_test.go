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

	"sllogger/slcore"
	"sllogger/writer"
)

// TestBuildOptionsVariants 覆盖 buildOptions 的开发模式、关闭调用方、
// 关闭调用栈与初始字段分支。
func TestBuildOptionsVariants(t *testing.T) {
	t.Run("development", func(t *testing.T) {
		cfg := NewDevelopmentConfig()
		opts := cfg.buildOptions(slcore.AddSync(&strings.Builder{}))
		log := NewNop().WithOptions(opts...)
		if !log.development {
			t.Fatal("Development option missing")
		}
		if log.addStack.Enabled(WarnLevel) != true {
			t.Fatal("development config must capture stacks from WarnLevel")
		}
	})
	t.Run("disable caller and stacktrace", func(t *testing.T) {
		cfg := Config{
			Level:             NewAtomicLevelAt(InfoLevel),
			Encoding:          "json",
			DisableCaller:     true,
			DisableStacktrace: true,
			OutputPaths:       []string{filepath.Join(t.TempDir(), "a.log")},
		}
		opts := cfg.buildOptions(slcore.AddSync(&strings.Builder{}))
		log := NewNop().WithOptions(opts...)
		if log.addCaller {
			t.Fatal("caller must be disabled")
		}
		// With stacktrace disabled, the level stays at the default.
		if log.addStack.Enabled(ErrorLevel) != false {
			t.Fatal("stacktrace must be disabled")
		}
	})
	t.Run("production default", func(t *testing.T) {
		cfg := NewProductionConfig()
		opts := cfg.buildOptions(slcore.AddSync(&strings.Builder{}))
		log := NewNop().WithOptions(opts...)
		if !log.addCaller {
			t.Fatal("caller must be enabled by default")
		}
		if !log.addStack.Enabled(ErrorLevel) {
			t.Fatal("stacktrace must be enabled from ErrorLevel")
		}
		if log.development {
			t.Fatal("development must be off")
		}
	})
	t.Run("initial fields", func(t *testing.T) {
		cfg := Config{
			Level:    NewAtomicLevelAt(InfoLevel),
			Encoding: "json",
			InitialFields: map[string]interface{}{
				"service": "playurl",
				"port":    8080,
			},
		}
		opts := cfg.buildOptions(slcore.AddSync(&strings.Builder{}))

		// Applying the options must attach both fields to the core.
		ws := newWriteRecorder()
		log := New(slcore.NewCore(newTestEncoder(t), ws, InfoLevel), opts...)
		log.Info("with initial fields")
		line := ws.lines()[0]
		if !contains(line, `"service":"playurl"`) || !contains(line, `"port":8080`) {
			t.Fatalf("output = %s", line)
		}
	})
}

// TestLevelToFuncAllLevels 覆盖 levelToFunc 的每个分支。
//
// 终端级别（DPanic/Panic/Fatal）的日志方法会 panic 或退出，因此只校验
// 函数解析成功，不实际调用；非终端级别则验证确实写入了一行。
func TestLevelToFuncAllLevels(t *testing.T) {
	log, ws := testLogger(t)

	safe := []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel}
	for i, lvl := range safe {
		fn, err := levelToFunc(log, lvl)
		if err != nil {
			t.Fatalf("levelToFunc(%v): %v", lvl, err)
		}
		fn("entry")
		if got := len(ws.lines()); got != i+1 {
			t.Fatalf("lines = %d after %v, want %d", got, lvl, i+1)
		}
	}

	// Terminal levels: only assert the lookup succeeds.
	for _, lvl := range []Level{DPanicLevel, PanicLevel, FatalLevel} {
		fn, err := levelToFunc(log, lvl)
		if err != nil {
			t.Fatalf("levelToFunc(%v): %v", lvl, err)
		}
		if fn == nil {
			t.Fatalf("levelToFunc(%v) returned nil", lvl)
		}
	}

	if _, err := levelToFunc(log, Level(99)); err == nil {
		t.Fatal("expected an error for an unknown level")
	}
}

// TestTemplateConfigWithService 覆盖服务名回填的各分支。
func TestTemplateConfigWithService(t *testing.T) {
	// 1. Explicit ServiceID wins over the rolling config.
	cfg := Config{
		TemplateConfig: TemplateConfig{ServiceID: "explicit"},
		Rolling:        &writer.Config{Dir: "/tmp", ServiceName: "fromRolling"},
	}
	if got := cfg.templateConfigWithService().ServiceID; got != "explicit" {
		t.Fatalf("ServiceID = %q, want explicit", got)
	}

	// 2. Empty ServiceID falls back to the rolling service name.
	cfg = Config{
		Rolling: &writer.Config{Dir: "/tmp", ServiceName: "fromRolling"},
	}
	if got := cfg.templateConfigWithService().ServiceID; got != "fromRolling" {
		t.Fatalf("ServiceID = %q, want fromRolling", got)
	}

	// 3. No rolling config: stays empty.
	cfg = Config{}
	if got := cfg.templateConfigWithService().ServiceID; got != "" {
		t.Fatalf("ServiceID = %q, want empty", got)
	}

	// 4. Rolling config with an empty service name: stays empty.
	cfg = Config{Rolling: &writer.Config{Dir: "/tmp"}}
	if got := cfg.templateConfigWithService().ServiceID; got != "" {
		t.Fatalf("ServiceID = %q, want empty", got)
	}
}

// TestBuildEncoderVariants 覆盖 buildEncoder 的每个 encoding 分支。
func TestBuildEncoderVariants(t *testing.T) {
	base := func(encoding string) Config {
		return Config{
			Level:       NewAtomicLevelAt(InfoLevel),
			Encoding:    encoding,
			OutputPaths: []string{filepath.Join(t.TempDir(), "x.log")},
		}
	}

	for _, encoding := range []string{"", "json", "JSON"} {
		if _, err := base(encoding).buildEncoder(); err != nil {
			t.Errorf("encoding %q: %v", encoding, err)
		}
	}
	for _, encoding := range []string{"template", "callinfo", "requestinfo", "CALLINFO"} {
		cfg := base(encoding)
		cfg.TemplateConfig = TemplateConfig{Template: "date|log_msg", ServiceID: "svc"}
		if _, err := cfg.buildEncoder(); err != nil {
			t.Errorf("encoding %q: %v", encoding, err)
		}
	}
	if _, err := base("unknown").buildEncoder(); err == nil {
		t.Fatal("expected an error for an unknown encoding")
	}
}

// TestBuildWithRollingServiceFallback 覆盖 Rolling 无 ServiceName 时的装配。
func TestBuildWithRollingServiceFallback(t *testing.T) {
	dir := t.TempDir()
	log := Must(Config{
		Level:    NewAtomicLevelAt(InfoLevel),
		Encoding: "callinfo",
		Rolling: &writer.Config{
			Dir:         dir,
			BaseName:    "SVC",
			ServiceName: "playurl",
			ServicePort: 8080,
		},
	}.Build())
	defer log.Close()

	log.InfoCtx(context.Background(), "fallback service id")

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no log file created")
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	// The ServiceID must have been filled from the rolling config.
	if !contains(string(data), "playurl") {
		t.Fatalf("content = %s, want the service id from the rolling config", data)
	}
}

// TestOpenOutputDefaults 覆盖 OutputPaths 为空时回退到 stderr。
func TestOpenOutputDefaults(t *testing.T) {
	cfg := Config{Level: NewAtomicLevelAt(InfoLevel), Encoding: "json"}
	ws, closers, err := cfg.openOutput()
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil {
		t.Fatal("WriteSyncer is nil")
	}
	for _, c := range closers {
		c.Close()
	}
}

// TestConfigClockPropagates 覆盖 Clock 同时贯通日志时间与滚动时钟。
func TestConfigClockPropagates(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	cfg := NewCallInfoConfig(dir, "svc", 80)
	cfg.Clock = fixedClock{t: fixed}
	log := Must(cfg.Build())
	defer log.Close()

	log.CallInfo(context.Background(), CallInfo{LogMsg: "clocked"})

	// 文件名来自 Rolling.Clock，行内时间来自 Logger clock，两者都是 fixed。
	want := filepath.Join(dir, "LOG_CALL_INFO.2020-01-02.svc80.log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected file %s: %v", want, err)
	}
	if !contains(string(data), "2020-01-02 03:04:05.000") {
		t.Fatalf("content = %s, want the injected clock time", data)
	}
}
