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
	"encoding/json"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

var jsonTime = time.Date(2026, 9, 3, 11, 22, 33, 0, time.Local)

func jsonEntry() slcore.Entry {
	return slcore.Entry{
		Level:      slcore.InfoLevel,
		Time:       jsonTime,
		LoggerName: "playurl",
		Message:    "hello",
	}
}

func encodeJSON(t *testing.T, enc *JSONEncoder, ent slcore.Entry, fields []slcore.Field) map[string]interface{} {
	t.Helper()
	buf, err := enc.EncodeEntry(ent, fields)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	line := buf.String()
	if line[len(line)-1] != '\n' {
		t.Fatalf("output must end with a line ending: %q", line)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	return out
}

func TestJSONEncoderDefaults(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	out := encodeJSON(t, enc, jsonEntry(), nil)

	if out["level"] != "INFO" {
		t.Errorf("level = %v", out["level"])
	}
	if out["msg"] != "hello" {
		t.Errorf("msg = %v", out["msg"])
	}
	if out["logger"] != "playurl" {
		t.Errorf("logger = %v", out["logger"])
	}
	if _, ok := out["ts"]; !ok {
		t.Error("ts missing")
	}
}

func TestJSONEncoderCustomKeys(t *testing.T) {
	enc := NewJSONEncoder(slcore.EncoderConfig{
		LevelKey:   "lvl",
		MessageKey: "message",
		TimeKey:    "time",
		EncodeLevel: func(l slcore.Level, enc slcore.PrimitiveArrayEncoder) {
			enc.AppendString(l.String())
		},
	})
	out := encodeJSON(t, enc, jsonEntry(), nil)
	if out["lvl"] != "info" {
		t.Errorf("lvl = %v", out["lvl"])
	}
	if out["message"] != "hello" {
		t.Errorf("message = %v", out["message"])
	}
	if _, ok := out["time"]; !ok {
		t.Error("time missing")
	}
}

func TestJSONEncoderOmitKeys(t *testing.T) {
	enc := NewJSONEncoder(slcore.EncoderConfig{
		LevelKey:   slcore.OmitKey,
		MessageKey: slcore.OmitKey,
		TimeKey:    slcore.OmitKey,
		NameKey:    slcore.OmitKey,
	})
	out := encodeJSON(t, enc, jsonEntry(), nil)
	for _, k := range []string{"level", "msg", "ts", "logger"} {
		if _, ok := out[k]; ok {
			t.Errorf("%s should be omitted", k)
		}
	}
}

func TestJSONEncoderFields(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	out := encodeJSON(t, enc, jsonEntry(), []slcore.Field{
		{Key: "s", Type: slcore.StringType, String: "v"},
		{Key: "i", Type: slcore.Int64Type, Integer: 7},
		{Key: "b", Type: slcore.BoolType, Integer: 1},
	})
	if out["s"] != "v" {
		t.Errorf("s = %v", out["s"])
	}
	if out["i"] != float64(7) {
		t.Errorf("i = %v", out["i"])
	}
	if out["b"] != true {
		t.Errorf("b = %v", out["b"])
	}
}

func TestJSONEncoderWithContext(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	child := enc.Clone().(*JSONEncoder)
	child.AddString("service", "playurl")
	child.AddInt("port", 8080)

	out := encodeJSON(t, child, jsonEntry(), nil)
	if out["service"] != "playurl" {
		t.Errorf("service = %v", out["service"])
	}
	if out["port"] != float64(8080) {
		t.Errorf("port = %v", out["port"])
	}

	// The parent must not see the child's context.
	parentOut := encodeJSON(t, enc, jsonEntry(), nil)
	if _, ok := parentOut["service"]; ok {
		t.Error("context leaked into the parent encoder")
	}
}

func TestJSONEncoderObjectAndArray(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	out := encodeJSON(t, enc, jsonEntry(), []slcore.Field{
		{Key: "obj", Type: slcore.ObjectMarshalerType, Interface: objMarshaler{}},
		{Key: "arr", Type: slcore.ArrayMarshalerType, Interface: arrMarshaler{}},
	})
	obj, ok := out["obj"].(map[string]interface{})
	if !ok {
		t.Fatalf("obj = %#v", out["obj"])
	}
	if obj["inner"] != "value" {
		t.Errorf("obj.inner = %v", obj["inner"])
	}
	arr, ok := out["arr"].([]interface{})
	if !ok {
		t.Fatalf("arr = %#v, want a JSON array", out["arr"])
	}
	if len(arr) != 1 || arr[0] != "first" {
		t.Errorf("arr = %v", arr)
	}
}

type objMarshaler struct{}

func (objMarshaler) MarshalLogObject(enc slcore.ObjectEncoder) error {
	enc.AddString("inner", "value")
	return nil
}

type arrMarshaler struct{}

func (arrMarshaler) MarshalLogArray(enc slcore.ArrayEncoder) error {
	enc.AppendString("first")
	return nil
}

func TestJSONEncoderCallerAndStack(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	ent := jsonEntry()
	ent.Caller = slcore.NewEntryCaller(1, "/home/u/proj/pkg/file.go", 12, true)
	ent.Stack = "stack-trace"
	out := encodeJSON(t, enc, ent, nil)
	if out["caller"] != "pkg/file.go:12" {
		t.Errorf("caller = %v", out["caller"])
	}
	if out["stacktrace"] != "stack-trace" {
		t.Errorf("stacktrace = %v", out["stacktrace"])
	}
}

func TestJSONEncoderLineEnding(t *testing.T) {
	cfg := DefaultJSONEncoderConfig()
	cfg.LineEnding = "\r\n"
	enc := NewJSONEncoder(cfg)
	buf, err := enc.EncodeEntry(jsonEntry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	if got := buf.String(); got[len(got)-2:] != "\r\n" {
		t.Fatalf("line ending = %q", got[len(got)-2:])
	}
}

func TestJSONEncoderResistsUnmarshalablePayload(t *testing.T) {
	enc := NewJSONEncoder(DefaultJSONEncoderConfig())
	// Channels cannot be marshaled; the encoder must degrade gracefully
	// instead of failing the log write.
	out := encodeJSON(t, enc, jsonEntry(), []slcore.Field{
		{Key: "bad", Type: slcore.ReflectType, Interface: make(chan int)},
	})
	if _, ok := out["msg"]; !ok {
		t.Fatalf("fallback payload expected, got %v", out)
	}
}
