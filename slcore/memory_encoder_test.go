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
	"time"
)

func TestMapObjectEncoderBasic(t *testing.T) {
	m := NewMapObjectEncoder()
	m.AddString("name", "foo")
	m.AddInt("age", 3)
	m.AddBool("ok", true)
	m.AddFloat64("pi", 3.14)
	m.AddUint("u", 9)

	if m.Fields["name"] != "foo" {
		t.Fatalf("name = %v", m.Fields["name"])
	}
	if m.Fields["age"] != 3 {
		t.Fatalf("age = %v", m.Fields["age"])
	}
	if m.Fields["ok"] != true {
		t.Fatalf("ok = %v", m.Fields["ok"])
	}
	if m.Fields["pi"] != 3.14 {
		t.Fatalf("pi = %v", m.Fields["pi"])
	}
	if m.Fields["u"] != uint(9) {
		t.Fatalf("u = %v", m.Fields["u"])
	}
}

func TestMapObjectEncoderTimeAndDuration(t *testing.T) {
	m := NewMapObjectEncoder()
	ts := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	m.AddTime("ts", ts)
	m.AddDuration("d", 90*time.Second)
	if !m.Fields["ts"].(time.Time).Equal(ts) {
		t.Fatalf("ts = %v", m.Fields["ts"])
	}
	if m.Fields["d"] != 90*time.Second {
		t.Fatalf("d = %v", m.Fields["d"])
	}
}

type memObj struct{ x string }

func (o memObj) MarshalLogObject(enc ObjectEncoder) error {
	enc.AddString("x", o.x)
	return nil
}

type memArr []int

func (a memArr) MarshalLogArray(enc ArrayEncoder) error {
	for _, v := range a {
		enc.AppendInt(v)
	}
	return nil
}

func TestMapObjectEncoderAddObject(t *testing.T) {
	m := NewMapObjectEncoder()
	m.AddObject("inner", memObj{"y"})
	ns, ok := m.Fields["inner"].(map[string]interface{})
	if !ok {
		t.Fatalf("inner should be a map, got %T", m.Fields["inner"])
	}
	if ns["x"] != "y" {
		t.Fatalf("inner.x = %v", ns["x"])
	}
}

func TestMapObjectEncoderAddArray(t *testing.T) {
	m := NewMapObjectEncoder()
	m.AddArray("arr", memArr{1, 2, 3})
	got, ok := m.Fields["arr"].([]interface{})
	if !ok {
		t.Fatalf("arr should be []interface{}, got %T", m.Fields["arr"])
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("unexpected arr %v", got)
	}
}

func TestMapObjectEncoderOpenNamespace(t *testing.T) {
	m := NewMapObjectEncoder()
	m.OpenNamespace("ns")
	m.AddString("k", "v")
	ns, ok := m.Fields["ns"].(map[string]interface{})
	if !ok {
		t.Fatalf("namespace should be a map, got %T", m.Fields["ns"])
	}
	if ns["k"] != "v" {
		t.Fatalf("ns.k = %v", ns["k"])
	}
}

func TestSliceArrayEncoder(t *testing.T) {
	a := &sliceArrayEncoder{elems: make([]interface{}, 0)}
	a.AppendBool(true)
	a.AppendInt(42)
	a.AppendInt64(43)
	a.AppendFloat64(1.5)
	a.AppendString("s")
	a.AppendUint(7)
	a.AppendComplex128(2 + 3i)
	if len(a.elems) != 7 {
		t.Fatalf("expected 7 elements, got %d", len(a.elems))
	}
	if a.elems[0] != true || a.elems[1] != 42 || a.elems[2] != int64(43) {
		t.Fatalf("unexpected numeric elements %v", a.elems)
	}
	if a.elems[3] != 1.5 || a.elems[4] != "s" || a.elems[5] != uint(7) {
		t.Fatalf("unexpected elements %v", a.elems)
	}
	if a.elems[6] != (2 + 3i) {
		t.Fatalf("complex element = %v", a.elems[6])
	}
}
