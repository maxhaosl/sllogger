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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/writer"
)

// readFirstFile returns the content of the (unique) log file whose name starts
// with prefix, e.g. "LOG_FEIGN." (note the trailing dot, so it won't match
// "LOG_FEIGN_ERR.").
func readFirstFile(t *testing.T, dir, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("no file with prefix %q in %s", prefix, dir)
	return ""
}

// TestMultiOutputLevelRouting verifies that levels are fanned out to the
// configured outputs: INFO/DEBUG/WARN go to the normal file, ERROR/WARN to the
// error file, and each level lands only where its whitelist allows.
func TestMultiOutputLevelRouting(t *testing.T) {
	dir := t.TempDir()
	feign := &writer.Config{Dir: dir, BaseName: "LOG_FEIGN", DateLayout: "2006-01-02", ServiceName: "playurl", ServicePort: 8080}
	ferr := &writer.Config{Dir: dir, BaseName: "LOG_FEIGN_ERR", DateLayout: "2006-01-02", ServiceName: "playurl", ServicePort: 8080}

	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		Outputs: []Output{
			{Name: "feign", Encoding: "json", Rolling: feign, Levels: []string{"info", "debug", "trace", "warn"}},
			{Name: "ferr", Encoding: "json", Rolling: ferr, Levels: []string{"error", "warn"}},
		},
	}
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	feignContent := readFirstFile(t, dir, "LOG_FEIGN.")
	ferrContent := readFirstFile(t, dir, "LOG_FEIGN_ERR.")

	for _, lvl := range []string{"INFO", "DEBUG", "WARN"} {
		if !strings.Contains(feignContent, `"level":"`+lvl+`"`) {
			t.Errorf("feign file missing level %q:\n%s", lvl, feignContent)
		}
	}
	if strings.Contains(feignContent, `"level":"ERROR"`) {
		t.Errorf("feign file must NOT contain error level:\n%s", feignContent)
	}

	if !strings.Contains(ferrContent, `"level":"ERROR"`) {
		t.Errorf("error file missing error level:\n%s", ferrContent)
	}
	if !strings.Contains(ferrContent, `"level":"WARN"`) {
		t.Errorf("error file missing warn level:\n%s", ferrContent)
	}
	for _, lvl := range []string{"INFO", "DEBUG"} {
		if strings.Contains(ferrContent, `"level":"`+lvl+`"`) {
			t.Errorf("error file must NOT contain %q:\n%s", lvl, ferrContent)
		}
	}
}

// TestMultiOutputSelectiveDisable verifies that when only an ERROR/WARN output is
// configured, INFO/DEBUG/TRACE entries are dropped entirely (no file gets them).
func TestMultiOutputSelectiveDisable(t *testing.T) {
	dir := t.TempDir()
	ferr := &writer.Config{Dir: dir, BaseName: "LOG_FEIGN_ERR", DateLayout: "2006-01-02", ServiceName: "playurl", ServicePort: 8080}

	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		// 上线只输出 ERROR/WARN：不配置 INFO/DEBUG/TRACE 输出即可。
		Outputs: []Output{
			{Name: "ferr", Encoding: "json", Rolling: ferr, Levels: []string{"error", "warn"}},
		},
	}
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	// 普通日志目录里不应出现 LOG_FEIGN.（非 ERR）文件。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "LOG_FEIGN.") && !strings.HasPrefix(e.Name(), "LOG_FEIGN_ERR.") {
			t.Fatalf("INFO/DEBUG must not be written, but found: %s", e.Name())
		}
	}

	ferrContent := readFirstFile(t, dir, "LOG_FEIGN_ERR.")
	if !strings.Contains(ferrContent, `"level":"ERROR"`) {
		t.Errorf("error file missing error level:\n%s", ferrContent)
	}
	if strings.Contains(ferrContent, `"level":"INFO"`) || strings.Contains(ferrContent, `"level":"DEBUG"`) {
		t.Errorf("error file must NOT contain info/debug:\n%s", ferrContent)
	}
}

// TestMultiOutputPerFileFormat verifies each output can use a different format:
// LOG_FEIGN uses JSON, LOG_MGMONITOR uses a "^"-separated template.
func TestMultiOutputPerFileFormat(t *testing.T) {
	dir := t.TempDir()
	feign := &writer.Config{Dir: dir, BaseName: "LOG_FEIGN", DateLayout: "2006-01-02", ServiceName: "playurl", ServicePort: 8080}
	mg := &writer.Config{Dir: dir, BaseName: "LOG_MGMONITOR", DateLayout: "2006-01-02", ServiceName: "mg", ServicePort: 0}

	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		Outputs: []Output{
			{Name: "feign", Encoding: "json", Rolling: feign},
			{Name: "mg", Encoding: "template", Rolling: mg,
				TemplateConfig: encoder.TemplateConfig{Separator: "^", Template: "log_level^date^log_msg"}},
		},
	}
	log, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}

	log.Info("hello")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	feignContent := readFirstFile(t, dir, "LOG_FEIGN.")
	if !strings.Contains(feignContent, `"level":"INFO"`) {
		t.Errorf("feign (json) file should contain json level field:\n%s", feignContent)
	}

	mgContent := readFirstFile(t, dir, "LOG_MGMONITOR.")
	// 自定义模板：大写级别 ^ 时间 ^ 消息，且不应出现 JSON 的 "level" 键。
	if !strings.Contains(mgContent, "INFO^") {
		t.Errorf("mgmonitor (template) file should use ^ separator with capital level:\n%s", mgContent)
	}
	if !strings.Contains(mgContent, "^hello") {
		t.Errorf("mgmonitor (template) file should contain the message:\n%s", mgContent)
	}
	if strings.Contains(mgContent, `"level"`) {
		t.Errorf("mgmonitor (template) file must NOT contain json level key:\n%s", mgContent)
	}
}

// TestBuildOutputInvalidLevels verifies that a Levels list containing no valid
// level names (e.g. all typos) fails fast instead of silently allowing every
// level through (which would disable the intended filter).
func TestBuildOutputInvalidLevels(t *testing.T) {
	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		Outputs: []Output{
			{Name: "bad", Encoding: "json", Levels: []string{"infox", "warnz"}},
		},
	}
	if _, err := cfg.Build(); err == nil {
		t.Fatal("expected error for Output with no valid Levels")
	}
}

// TestBuildOutputInvalidExcludeLevels verifies the same guard for ExcludeLevels.
func TestBuildOutputInvalidExcludeLevels(t *testing.T) {
	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		Outputs: []Output{
			{Name: "bad", Encoding: "json", ExcludeLevels: []string{"infox"}},
		},
	}
	if _, err := cfg.Build(); err == nil {
		t.Fatal("expected error for Output with no valid ExcludeLevels")
	}
}

// TestBuildOutputTraceAccepted confirms "trace" (mapped to DebugLevel) is a
// valid level name and does not trip the all-invalid guard.
func TestBuildOutputTraceAccepted(t *testing.T) {
	cfg := Config{
		Level: NewAtomicLevelAt(DebugLevel),
		Outputs: []Output{
			{Name: "t", Encoding: "json", Levels: []string{"trace"}},
		},
	}
	if _, err := cfg.Build(); err != nil {
		t.Fatalf("trace should be a valid level, got: %v", err)
	}
}
