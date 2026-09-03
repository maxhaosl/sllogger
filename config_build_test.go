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
)

// TestBuildWithOptions 覆盖 Config.Build(opts...) 传入额外 Option 的分支。
//
// 这是公开 API 的一部分（允许调用方在声明式配置之上追加命令式选项），
// 此前 Build 一直以无参形式被测，该分支未被覆盖。
func TestBuildWithOptions(t *testing.T) {
	dir := t.TempDir()

	var hooked int
	cfg := NewProductionConfig()
	cfg.OutputPaths = []string{filepath.Join(dir, "app.log")}

	log := Must(cfg.Build(
		Fields(String("component", "api")),
		Hooks(func(slcore.Entry) error { hooked++; return nil }),
	))
	defer log.Close()

	log.Info("with options")
	if hooked != 1 {
		t.Fatalf("Build 传入的 Hooks 未被应用: hooked = %d", hooked)
	}

	// Fields 也必须生效，且不污染同配置构建的其他 logger。
	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), `"component":"api"`) {
		t.Fatalf("Build 传入的 Fields 未生效: %s", data)
	}

	// 无参构建的 logger 不应带上次的 Option。
	log2 := Must(cfg.Build())
	defer log2.Close()
	log2.Info("no options")
	if hooked != 1 {
		t.Fatalf("Option 泄漏到另一次 Build: hooked = %d", hooked)
	}
}

// TestBuildWithOptionsOnRolling 覆盖滚动配置 + 额外 Option 的组合。
func TestBuildWithOptionsOnRolling(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local)

	cfg := NewCallInfoConfig(dir, "svc", 80)
	cfg.Clock = fixedClock{t: fixed}

	log := Must(cfg.Build(Fields(String("region", "cn-east"))))
	defer log.Close()

	log.CallInfo(context.Background(), CallInfo{
		URL:    "http://play.example.com:443/playurl/v1/play/playurl",
		Method: "GET",
		LogMsg: "rolling with options",
	})

	data, err := os.ReadFile(filepath.Join(dir, "LOG_CALL_INFO.2026-09-03.svc80.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "rolling with options") {
		t.Fatalf("content = %s", data)
	}
}

// TestBuildErrorOutputFailure 覆盖错误输出路径打开失败时的清理分支：
// 已打开的输出资源必须被关闭，且 Build 返回错误。
func TestBuildErrorOutputFailure(t *testing.T) {
	dir := t.TempDir()
	// 错误输出指向一个无法打开的目录（文件作为父目录）。
	blocked := filepath.Join(dir, "out.log")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := NewProductionConfig()
	cfg.OutputPaths = []string{filepath.Join(dir, "ok.log")}
	cfg.ErrorOutputPaths = []string{filepath.Join(blocked, "err.log")}

	log, err := cfg.Build()
	if err == nil {
		log.Close()
		t.Fatal("expected an error when the error output path cannot be opened")
	}
	// 错误信息应包含原始原因，便于定位。
	if !strings.Contains(err.Error(), "open output") {
		t.Fatalf("err = %v, want it to mention opening the output", err)
	}
}

// TestOpenOutputFailure 覆盖 openOutput 中输出路径打开失败的分支。
func TestOpenOutputFailure(t *testing.T) {
	dir := t.TempDir()
	// 用普通文件充当父目录，使 Open 必然失败。
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Level:       NewAtomicLevelAt(InfoLevel),
		Encoding:    "json",
		OutputPaths: []string{filepath.Join(blocked, "out.log")},
	}
	if _, err := cfg.Build(); err == nil {
		t.Fatal("expected an error when the output path cannot be opened")
	}
}

// TestCheckTerminalLevelNotEnabled 覆盖 check 中"级别为 Panic/Fatal 但
// core 未启用"的分支：ce 为 nil 时应提前返回，不做后续标注。
func TestCheckTerminalLevelNotEnabled(t *testing.T) {
	// core 完全禁用（级别高于 Fatal），但 Panic/Fatal 仍会走完 check 流程。
	log := New(slcore.NewCore(newTestEncoder(t), newWriteRecorder(), FatalLevel+1),
		WithPanicHook(slcore.WriteThenGoexit))

	// 在独立 goroutine 中运行，因为 Panic 会触发 Goexit。
	done := make(chan struct{})
	go func() {
		defer close(done)
		log.Panic("should not be written")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Panic 在 core 未启用时未正常返回")
	}

	// 同样的场景对 Fatal 成立。
	log2 := New(slcore.NewCore(newTestEncoder(t), newWriteRecorder(), FatalLevel+1),
		WithFatalHook(slcore.WriteThenGoexit))
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		log2.Fatal("should not be written")
	}()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("Fatal 在 core 未启用时未正常返回")
	}
}

// TestInvalidPairsMarshalLogArrayError 覆盖 MarshalLogArray 中
// AppendObject 返回错误的分支：必须把错误向上传播。
func TestInvalidPairsMarshalLogArrayError(t *testing.T) {
	ps := invalidPairs{
		{position: 0, key: 1, value: "a"},
		{position: 1, key: 2, value: "b"},
	}

	if err := ps.MarshalLogArray(failingArrayEncoder{}); err == nil {
		t.Fatal("expected AppendObject's error to be propagated")
	}
}

// failingArrayEncoder 的 AppendObject 总是返回错误。
type failingArrayEncoder struct{}

func (failingArrayEncoder) AppendBool(bool)                         {}
func (failingArrayEncoder) AppendByteString([]byte)                 {}
func (failingArrayEncoder) AppendComplex128(complex128)             {}
func (failingArrayEncoder) AppendComplex64(complex64)               {}
func (failingArrayEncoder) AppendFloat64(float64)                   {}
func (failingArrayEncoder) AppendFloat32(float32)                   {}
func (failingArrayEncoder) AppendInt(int)                           {}
func (failingArrayEncoder) AppendInt64(int64)                       {}
func (failingArrayEncoder) AppendInt32(int32)                       {}
func (failingArrayEncoder) AppendInt16(int16)                       {}
func (failingArrayEncoder) AppendInt8(int8)                         {}
func (failingArrayEncoder) AppendString(string)                     {}
func (failingArrayEncoder) AppendUint(uint)                         {}
func (failingArrayEncoder) AppendUint64(uint64)                     {}
func (failingArrayEncoder) AppendUint32(uint32)                     {}
func (failingArrayEncoder) AppendUint16(uint16)                     {}
func (failingArrayEncoder) AppendUint8(uint8)                       {}
func (failingArrayEncoder) AppendUintptr(uintptr)                   {}
func (failingArrayEncoder) AppendDuration(d time.Duration)          {}
func (failingArrayEncoder) AppendTime(time.Time)                    {}
func (failingArrayEncoder) AppendArray(slcore.ArrayMarshaler) error { return nil }
func (failingArrayEncoder) AppendObject(slcore.ObjectMarshaler) error {
	return errArrayAppend
}
func (failingArrayEncoder) AppendReflected(interface{}) error { return nil }

var errArrayAppend = &testErr{msg: "array append failed"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
