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
	"bytes"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
)

// newBufferCore builds a core that writes JSON to an in-memory buffer.
func newBufferCore(buf *bytes.Buffer, lvl slcore.Level) slcore.Core {
	enc := encoder.NewJSONEncoder(slcore.EncoderConfig{
		MessageKey:  "msg",
		LevelKey:    "level",
		EncodeLevel: slcore.LowercaseLevelEncoder,
		LineEnding:  slcore.DefaultLineEnding,
	})
	return slcore.NewCore(enc, slcore.AddSync(buf), lvl)
}

func TestParityCoreWrap(t *testing.T) {
	buf1, buf2 := &bytes.Buffer{}, &bytes.Buffer{}
	core1 := newBufferCore(buf1, slcore.DebugLevel)
	core2 := newBufferCore(buf2, slcore.DebugLevel)

	// NewTee
	tee := slcore.NewTee(core1, core2)
	ce := tee.Check(slcore.Entry{Level: slcore.InfoLevel, Message: "hi"}, &slcore.CheckedEntry{})
	if ce == nil {
		t.Fatal("expected non-nil checked entry from tee")
	}
	ce.Write(slcore.Field{Key: "k", Type: slcore.StringType, String: "v"})

	// NewLazyWith
	lazy := slcore.NewLazyWith(core1, []Field{String("lazy", "yes")})
	if lazy == nil {
		t.Fatal("NewLazyWith returned nil")
	}
	_ = lazy.With([]Field{Int("n", 1)})

	// NewIncreaseLevelCore raises the floor to InfoLevel.
	raised, err := slcore.NewIncreaseLevelCore(core1, slcore.InfoLevel)
	if err != nil {
		t.Fatalf("NewIncreaseLevelCore: %v", err)
	}
	if raised.Enabled(slcore.DebugLevel) {
		t.Error("after IncreaseLevel to Info, Debug should be disabled")
	}
	if !raised.Enabled(slcore.InfoLevel) {
		t.Error("after IncreaseLevel to Info, Info should be enabled")
	}

	// NewMapObjectEncoder
	me := slcore.NewMapObjectEncoder()
	me.AddString("a", "b")
	me.AddInt("c", 3)
	if me.Fields["a"] != "b" || me.Fields["c"] != 3 {
		t.Errorf("MapObjectEncoder did not populate fields: %+v", me.Fields)
	}
}

func TestParitySampler(t *testing.T) {
	buf := &bytes.Buffer{}
	core := newBufferCore(buf, slcore.DebugLevel)
	// first=2, thereafter=0 -> after the first 2 entries, all are dropped.
	sampled := slcore.NewSamplerWithOptions(core, time.Second, 2, 0)
	for i := 0; i < 5; i++ {
		ce := sampled.Check(slcore.Entry{Level: slcore.InfoLevel, Message: "repeat"}, &slcore.CheckedEntry{})
		if ce != nil {
			ce.Write(Int("i", i))
		}
	}
	// Exactly the first two entries ("i":0 and "i":1) should be written.
	if got := bytes.Count(buf.Bytes(), []byte(`{"i":`)); got != 2 {
		t.Errorf("sampler wrote %d entries, want 2; buf=%q", got, buf.String())
	}
}

func TestParityRegisterEncoderAndSink(t *testing.T) {
	if err := RegisterEncoder("test-json", func(cfg slcore.EncoderConfig) (slcore.Encoder, error) {
		return encoder.NewJSONEncoder(cfg), nil
	}); err != nil {
		t.Fatalf("RegisterEncoder: %v", err)
	}
	// Reserving an existing name must fail.
	if err := RegisterEncoder("json", func(cfg slcore.EncoderConfig) (slcore.Encoder, error) {
		return encoder.NewJSONEncoder(cfg), nil
	}); err == nil {
		t.Error("expected error registering reserved encoder name json")
	}
	// scheme must start with a letter
	if err := RegisterSink("9bad", func(*url.URL) (Sink, error) { return nil, nil }); err == nil {
		t.Error("expected error registering invalid sink scheme")
	}
}

func TestParityOptionsAndConfigHelpers(t *testing.T) {
	buf := &bytes.Buffer{}
	core := newBufferCore(buf, slcore.DebugLevel)
	log := New(core).WithOptions(IncreaseLevel(slcore.WarnLevel))
	log.Info("should-be-silenced")
	log.Warn("visible")
	if bytes.Contains(buf.Bytes(), []byte("should-be-silenced")) {
		t.Error("IncreaseLevel option did not raise the level")
	}
	if !bytes.Contains(buf.Bytes(), []byte("visible")) {
		t.Error("log at Warn after IncreaseLevel should be visible")
	}

	if len(NewProductionEncoderConfig().MessageKey) == 0 {
		t.Error("NewProductionEncoderConfig produced empty config")
	}
	if len(NewDevelopmentEncoderConfig().MessageKey) == 0 {
		t.Error("NewDevelopmentEncoderConfig produced empty config")
	}
	if NewExample() == nil {
		t.Error("NewExample returned nil")
	}
	if LevelFlag("lvl", slcore.InfoLevel, "test") == nil {
		t.Error("LevelFlag returned nil")
	}
}

func TestParityFieldConstructors(t *testing.T) {
	cases := []struct {
		name string
		f    Field
		want slcore.FieldType
	}{
		{"Ints", Ints("x", []int{1, 2}), slcore.ArrayMarshalerType},
		{"Int64s", Int64s("x", []int64{1}), slcore.ArrayMarshalerType},
		{"Uints", Uints("x", []uint{1}), slcore.ArrayMarshalerType},
		{"Float64s", Float64s("x", []float64{1}), slcore.ArrayMarshalerType},
		{"Strings", Strings("x", []string{"a"}), slcore.ArrayMarshalerType},
		{"Bools", Bools("x", []bool{true}), slcore.ArrayMarshalerType},
		{"ByteStrings", ByteStrings("x", [][]byte{{1}}), slcore.ArrayMarshalerType},
		{"Times", Times("x", []time.Time{time.Now()}), slcore.ArrayMarshalerType},
		{"Durations", Durations("x", []time.Duration{time.Second}), slcore.ArrayMarshalerType},
		{"Complex128s", Complex128s("x", []complex128{1 + 2i}), slcore.ArrayMarshalerType},
		{"Uintptrs", Uintptrs("x", []uintptr{1}), slcore.ArrayMarshalerType},
		{"Errors", Errors("x", []error{errors.New("e")}), slcore.ArrayMarshalerType},
		{"Intp", Intp("x", intPtr(5)), slcore.Int64Type},
		{"Stringp", Stringp("x", strPtr("s")), slcore.StringType},
		{"Timep", Timep("x", timePtr(time.Now())), slcore.TimeFullType},
	}
	for _, c := range cases {
		if c.f.Type != c.want {
			t.Errorf("%s: got type %v, want %v", c.name, c.f.Type, c.want)
		}
	}

	// nil pointer -> nilField (ReflectType)
	np := Intp("x", nil)
	if np.Type != slcore.ReflectType {
		t.Errorf("nil Intp should be ReflectType, got %v", np.Type)
	}

	// Dict / Stack
	if Dict("d", String("a", "b")).Type != slcore.ObjectMarshalerType {
		t.Error("Dict field should be ObjectMarshalerType")
	}
	if Stack("s").Type != slcore.StringType {
		t.Error("Stack field should be StringType")
	}
	obj := DictObject(String("k", "v"))
	if obj == nil {
		t.Error("DictObject returned nil")
	}
	// Inline wraps an ObjectMarshaler; a MapObjectEncoder is an ObjectEncoder,
	// not a marshaler, so we construct a tiny marshaler here.
	_ = Inline(nilMarshaler{})
}

type nilMarshaler struct{}

func (nilMarshaler) MarshalLogObject(slcore.ObjectEncoder) error { return nil }

func TestParityAny(t *testing.T) {
	for _, v := range []interface{}{
		42, "str", []int{1, 2, 3}, []string{"a"}, time.Now(), errors.New("e"), []Field{String("a", "b")},
	} {
		if !isValidFieldType(Any("k", v).Type) {
			t.Errorf("Any(%T) produced invalid field type", v)
		}
	}
}

func isValidFieldType(ft slcore.FieldType) bool {
	return ft != slcore.SkipType
}

func intPtr(v int) *int              { return &v }
func strPtr(v string) *string        { return &v }
func timePtr(v time.Time) *time.Time { return &v }
