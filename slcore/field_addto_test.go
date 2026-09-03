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

package slcore

import (
	"testing"
)

type addToObject struct{ value string }

func (o addToObject) MarshalLogObject(enc ObjectEncoder) error {
	enc.AddString("value", o.value)
	return nil
}

type addToArray struct{}

func (addToArray) MarshalLogArray(enc ArrayEncoder) error {
	enc.AppendString("item")
	return nil
}

// TestFieldAddToMarshalerTypes 覆盖 AddTo 中 ArrayMarshaler /
// ObjectMarshaler / InlineMarshaler 三个分支。
func TestFieldAddToMarshalerTypes(t *testing.T) {
	t.Run("ArrayMarshaler", func(t *testing.T) {
		enc := newMapObjectEncoder()
		Field{Key: "arr", Type: ArrayMarshalerType, Interface: addToArray{}}.AddTo(enc)
		if _, ok := enc["arr"]; !ok {
			t.Fatalf("array field not encoded: %v", enc)
		}
	})

	t.Run("ObjectMarshaler", func(t *testing.T) {
		enc := newMapObjectEncoder()
		Field{Key: "obj", Type: ObjectMarshalerType, Interface: addToObject{"v"}}.AddTo(enc)
		sub, ok := enc["obj"].(map[string]interface{})
		if !ok {
			t.Fatalf("object field = %#v", enc["obj"])
		}
		if sub["value"] != "v" {
			t.Fatalf("object value = %v", sub)
		}
	})

	t.Run("InlineMarshaler 内联到父级", func(t *testing.T) {
		enc := newMapObjectEncoder()
		Field{Type: InlineMarshalerType, Interface: addToObject{"inline"}}.AddTo(enc)
		// 内联：字段直接落在父级，不产生嵌套 key。
		if enc["value"] != "inline" {
			t.Fatalf("inline field = %v, want value=inline", enc)
		}
	})

	t.Run("marshaler 返回错误时落到 kError", func(t *testing.T) {
		enc := newMapObjectEncoder()
		Field{Key: "obj", Type: ObjectMarshalerType, Interface: failingObjectMarshaler{}}.AddTo(enc)
		if _, ok := enc["objError"]; !ok {
			t.Fatalf("error not surfaced as objError: %v", enc)
		}
	})

	t.Run("数组 marshaler 返回错误", func(t *testing.T) {
		enc := newMapObjectEncoder()
		Field{Key: "arr", Type: ArrayMarshalerType, Interface: failingArrayMarshaler{}}.AddTo(enc)
		if _, ok := enc["arrError"]; !ok {
			t.Fatalf("error not surfaced as arrError: %v", enc)
		}
	})
}

type failingObjectMarshaler struct{}

func (failingObjectMarshaler) MarshalLogObject(ObjectEncoder) error { return errStub }

type failingArrayMarshaler struct{}

func (failingArrayMarshaler) MarshalLogArray(ArrayEncoder) error { return errStub }

// TestMultiWriteSyncerShortWrite 覆盖 multiWriteSyncer.Write 中
// "n < nWritten" 的分支：返回所有 sink 中写入最少者的字节数。
func TestMultiWriteSyncerShortWrite(t *testing.T) {
	long := &countingWriteSyncer{}
	short := shortWriteSyncer{n: 3}

	ws := NewMultiWriteSyncer(long, short)
	n, err := ws.Write([]byte("abcdefgh"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n = %d, want 3 (smallest write wins)", n)
	}
	// 第一个 sink 仍应收到完整的写入。
	if got := long.String(); got != "abcdefgh" {
		t.Fatalf("long sink = %q", got)
	}
}

// TestCaptureStackEmpty 覆盖 CaptureStack 中 runtime.Callers 返回 0 的分支
// （skip 极大时没有栈帧可采集）。
func TestCaptureStackEmpty(t *testing.T) {
	if got := CaptureStack(1 << 20); got != "" {
		t.Fatalf("CaptureStack with a huge skip = %q, want empty", got)
	}
}
