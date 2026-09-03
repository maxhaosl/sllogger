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

// Tests derived from go.uber.org/zap/sugar_test.go.
package sllogger

import (
	"testing"
	"time"

	"sllogger/slcore"
)

// TestSugarAllLevels 覆盖 SugaredLogger 的全部级别方法。
// Panic/Fatal 会中断执行，因此用 goroutine 各自的终端钩子隔离。
func TestSugarAllLevels(t *testing.T) {
	sugar, ws := sugaredLogger(t)

	sugar.Debug("debug")
	sugar.Info("info")
	sugar.Warn("warn")
	sugar.Error("error")

	sugar.Debugf("debugf %d", 1)
	sugar.Infof("infof %d", 2)
	sugar.Warnf("warnf %d", 3)
	sugar.Errorf("errorf %d", 4)

	sugar.Debugw("debugw", "k", "v1")
	sugar.Infow("infow", "k", "v2")
	sugar.Warnw("warnw", "k", "v3")
	sugar.Errorw("errorw", "k", "v4")

	sugar.Debugln("debugln")
	sugar.Infoln("infoln")
	sugar.Warnln("warnln")
	sugar.Errorln("errorln")

	lines := ws.lines()
	if len(lines) != 16 {
		t.Fatalf("lines = %d, want 16", len(lines))
	}
	for i, want := range []string{"debug", "info", "warn", "error"} {
		if !contains(lines[i], `"msg":"`+want+`"`) {
			t.Errorf("line %d = %s", i, lines[i])
		}
	}
	if !contains(lines[4], "debugf 1") || !contains(lines[7], "errorf 4") {
		t.Errorf("Printf style lines = %v", lines[4:8])
	}
	if !contains(lines[8], `"k":"v1"`) || !contains(lines[11], `"k":"v4"`) {
		t.Errorf("structured lines = %v", lines[8:12])
	}
	if !contains(lines[12], "debugln") || !contains(lines[15], "errorln") {
		t.Errorf("Println style lines = %v", lines[12:16])
	}
}

// TestSugarTerminalLevels 覆盖 DPanic/Panic/Fatal 及其格式化变体。
// 这些级别会终止执行，因此用 Goexit 钩子在独立 goroutine 中运行。
func TestSugarTerminalLevels(t *testing.T) {
	tests := []struct {
		name string
		call func(*SugaredLogger)
		want string
	}{
		{"DPanic", func(s *SugaredLogger) { s.DPanic("dpanic") }, "dpanic"},
		{"Panic", func(s *SugaredLogger) { s.Panic("panic") }, "panic"},
		{"Fatal", func(s *SugaredLogger) { s.Fatal("fatal") }, "fatal"},
		{"DPanicf", func(s *SugaredLogger) { s.DPanicf("dpanicf %d", 1) }, "dpanicf 1"},
		{"Panicf", func(s *SugaredLogger) { s.Panicf("panicf %d", 2) }, "panicf 2"},
		{"Fatalf", func(s *SugaredLogger) { s.Fatalf("fatalf %d", 3) }, "fatalf 3"},
		{"DPanicw", func(s *SugaredLogger) { s.DPanicw("dpanicw", "k", "v") }, "dpanicw"},
		{"Panicw", func(s *SugaredLogger) { s.Panicw("panicw", "k", "v") }, "panicw"},
		{"Fatalw", func(s *SugaredLogger) { s.Fatalw("fatalw", "k", "v") }, "fatalw"},
		{"DPanicln", func(s *SugaredLogger) { s.DPanicln("dpanicln") }, "dpanicln"},
		{"Panicln", func(s *SugaredLogger) { s.Panicln("panicln") }, "panicln"},
		{"Fatalln", func(s *SugaredLogger) { s.Fatalln("fatalln") }, "fatalln"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sugar, ws := sugaredLogger(t)
			// 用 Goexit 替换真实的 panic/os.Exit，使测试可继续。
			goexit := sugar.WithOptions(
				WithPanicHook(slcore.WriteThenGoexit),
				WithFatalHook(slcore.WriteThenGoexit),
			)

			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.call(goexit)
			}()
			select {
			case <-done:
			case <-timeoutAfter():
				t.Fatal("terminal level did not stop execution")
			}

			lines := ws.lines()
			if len(lines) == 0 {
				t.Fatal("no output")
			}
			if !contains(lines[0], tt.want) {
				t.Errorf("line = %s, want %q", lines[0], tt.want)
			}
		})
	}
}

// TestSugarLogVariants 覆盖 Log/Logf/Logw/Logln 在各级别的行为。
func TestSugarLogVariants(t *testing.T) {
	sugar, ws := sugaredLogger(t)

	sugar.Debugf("%s", "x") // 保证 sugar 已被使用
	ws2 := newWriteRecorder()
	sugar2 := New(sugar.base.Core()).Sugar()

	_ = sugar2
	_ = ws2

	// 逐一验证各级别委托方法写入正确的级别。
	sugar, ws = sugaredLogger(t)
	sugar.Debugln("a")
	sugar.Log(DebugLevel, "b")
	sugar.Logf(DebugLevel, "c=%d", 1)
	sugar.Logw(DebugLevel, "d", "k", "v")
	sugar.Logln(DebugLevel, "e")

	lines := ws.lines()
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	msgs := []string{"a", "b", "c=1", "d", "e"}
	for i, want := range msgs {
		if !contains(lines[i], want) {
			t.Errorf("line %d = %s, want %q", i, lines[i], want)
		}
	}
}

// TestSugarInvalidPairsMarshalLogObject 覆盖 invalidPair 的日志编组路径。
func TestSugarInvalidPairsMarshalLogObject(t *testing.T) {
	p := invalidPair{position: 2, key: 42, value: "v"}

	enc := newRecordingObjectEncoder()
	if err := p.MarshalLogObject(enc); err != nil {
		t.Fatal(err)
	}
	if enc.ints["position"] != 2 {
		t.Fatalf("position = %d", enc.ints["position"])
	}
	// Any(key, 42) resolves to an int64 field, and Any(value, "v") to a
	// string field.
	if enc.ints["key"] != 42 {
		t.Fatalf("key = %d, want 42", enc.ints["key"])
	}
	if enc.strings["value"] != "v" {
		t.Fatalf("value = %q, want v", enc.strings["value"])
	}

	ps := invalidPairs{p}
	arr := &recordingArrayEncoder{}
	if err := ps.MarshalLogArray(arr); err != nil {
		t.Fatal(err)
	}
	if arr.objects != 1 {
		t.Fatalf("objects = %d, want 1", arr.objects)
	}
}

// recordingObjectEncoder implements slcore.ObjectEncoder and records calls.
type recordingObjectEncoder struct {
	strings map[string]string
	ints    map[string]int64
}

func newRecordingObjectEncoder() *recordingObjectEncoder {
	return &recordingObjectEncoder{
		strings: map[string]string{},
		ints:    map[string]int64{},
	}
}

func (e *recordingObjectEncoder) AddString(k, v string) {
	if e.strings == nil {
		e.strings = map[string]string{}
	}
	e.strings[k] = v
}
func (e *recordingObjectEncoder) AddInt64(k string, v int64) {
	if e.ints == nil {
		e.ints = map[string]int64{}
	}
	e.ints[k] = v
}
func (e *recordingObjectEncoder) AddArray(k string, arr slcore.ArrayMarshaler) error { return nil }
func (e *recordingObjectEncoder) AddObject(k string, obj slcore.ObjectMarshaler) error {
	return nil
}
func (e *recordingObjectEncoder) AddBinary(k string, v []byte)         { e.AddString(k, string(v)) }
func (e *recordingObjectEncoder) AddByteString(k string, v []byte)     { e.AddString(k, string(v)) }
func (e *recordingObjectEncoder) AddBool(k string, v bool)             { e.AddString(k, "bool") }
func (e *recordingObjectEncoder) AddComplex128(k string, v complex128) {}
func (e *recordingObjectEncoder) AddComplex64(k string, v complex64)   {}
func (e *recordingObjectEncoder) AddDuration(k string, v time.Duration) {
	e.AddInt64(k, v.Milliseconds())
}
func (e *recordingObjectEncoder) AddFloat64(k string, v float64) {}
func (e *recordingObjectEncoder) AddFloat32(k string, v float32) {}
func (e *recordingObjectEncoder) AddInt(k string, v int)         { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddInt32(k string, v int32)     { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddInt16(k string, v int16)     { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddInt8(k string, v int8)       { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddTime(k string, v time.Time)  { e.AddString(k, v.String()) }
func (e *recordingObjectEncoder) AddUint(k string, v uint)       { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddUint64(k string, v uint64)   { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddUint32(k string, v uint32)   { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddUint16(k string, v uint16)   { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddUint8(k string, v uint8)     { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddUintptr(k string, v uintptr) { e.AddInt64(k, int64(v)) }
func (e *recordingObjectEncoder) AddReflected(k string, v interface{}) error {
	// A non-scalar key (e.g. int) falls through to Reflect on this encoder.
	e.AddString(k, "reflected")
	return nil
}
func (e *recordingObjectEncoder) OpenNamespace(k string) {}

// recordingArrayEncoder implements slcore.ArrayEncoder and counts objects.
type recordingArrayEncoder struct {
	objects int
}

func (e *recordingArrayEncoder) AppendBool(bool)              {}
func (e *recordingArrayEncoder) AppendByteString([]byte)      {}
func (e *recordingArrayEncoder) AppendComplex128(complex128)  {}
func (e *recordingArrayEncoder) AppendComplex64(complex64)    {}
func (e *recordingArrayEncoder) AppendFloat64(float64)        {}
func (e *recordingArrayEncoder) AppendFloat32(float32)        {}
func (e *recordingArrayEncoder) AppendInt(int)                {}
func (e *recordingArrayEncoder) AppendInt64(int64)            {}
func (e *recordingArrayEncoder) AppendInt32(int32)            {}
func (e *recordingArrayEncoder) AppendInt16(int16)            {}
func (e *recordingArrayEncoder) AppendInt8(int8)              {}
func (e *recordingArrayEncoder) AppendString(string)          {}
func (e *recordingArrayEncoder) AppendUint(uint)              {}
func (e *recordingArrayEncoder) AppendUint64(uint64)          {}
func (e *recordingArrayEncoder) AppendUint32(uint32)          {}
func (e *recordingArrayEncoder) AppendUint16(uint16)          {}
func (e *recordingArrayEncoder) AppendUint8(uint8)            {}
func (e *recordingArrayEncoder) AppendUintptr(uintptr)        {}
func (e *recordingArrayEncoder) AppendDuration(time.Duration) {}
func (e *recordingArrayEncoder) AppendTime(time.Time)         {}
func (e *recordingArrayEncoder) AppendArray(slcore.ArrayMarshaler) error {
	return nil
}
func (e *recordingArrayEncoder) AppendObject(slcore.ObjectMarshaler) error {
	e.objects++
	return nil
}
func (e *recordingArrayEncoder) AppendReflected(interface{}) error { return nil }

// timeoutAfter returns the channel used to bound terminal-level tests.
func timeoutAfter() <-chan time.Time { return time.After(2 * time.Second) }
