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

// Tests derived from go.uber.org/zap/zapcore/encoder_test.go.
package slcore

import (
	"strings"
	"testing"
	"time"
)

// capturingEncoder records the single primitive value emitted by a
// Level/Time/Duration/Caller/Name encoder.
type capturingEncoder struct {
	s    string
	f    float64
	i    int64
	u    uint64
	b    bool
	bs   []byte
	c128 complex128
	c64  complex64
	kind string
}

func (c *capturingEncoder) AppendString(s string)         { c.s, c.kind = s, "string" }
func (c *capturingEncoder) AppendFloat64(f float64)       { c.f, c.kind = f, "float64" }
func (c *capturingEncoder) AppendFloat32(f float32)       { c.f, c.kind = float64(f), "float32" }
func (c *capturingEncoder) AppendInt(i int)               { c.i, c.kind = int64(i), "int" }
func (c *capturingEncoder) AppendInt64(i int64)           { c.i, c.kind = i, "int64" }
func (c *capturingEncoder) AppendInt32(i int32)           { c.i, c.kind = int64(i), "int32" }
func (c *capturingEncoder) AppendInt16(i int16)           { c.i, c.kind = int64(i), "int16" }
func (c *capturingEncoder) AppendInt8(i int8)             { c.i, c.kind = int64(i), "int8" }
func (c *capturingEncoder) AppendUint(u uint)             { c.u, c.kind = uint64(u), "uint" }
func (c *capturingEncoder) AppendUint64(u uint64)         { c.u, c.kind = u, "uint64" }
func (c *capturingEncoder) AppendUint32(u uint32)         { c.u, c.kind = uint64(u), "uint32" }
func (c *capturingEncoder) AppendUint16(u uint16)         { c.u, c.kind = uint64(u), "uint16" }
func (c *capturingEncoder) AppendUint8(u uint8)           { c.u, c.kind = uint64(u), "uint8" }
func (c *capturingEncoder) AppendUintptr(u uintptr)       { c.u, c.kind = uint64(u), "uintptr" }
func (c *capturingEncoder) AppendBool(b bool)             { c.b, c.kind = b, "bool" }
func (c *capturingEncoder) AppendByteString(bs []byte)    { c.bs, c.kind = bs, "bytestring" }
func (c *capturingEncoder) AppendComplex128(v complex128) { c.c128, c.kind = v, "complex128" }
func (c *capturingEncoder) AppendComplex64(v complex64)   { c.c64, c.kind = v, "complex64" }

func TestLevelEncoders(t *testing.T) {
	tests := []struct {
		name string
		enc  LevelEncoder
		lvl  Level
		want string
	}{
		{"lowercase", LowercaseLevelEncoder, WarnLevel, "warn"},
		{"capital", CapitalLevelEncoder, WarnLevel, "WARN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got capturingEncoder
			tt.enc(tt.lvl, &got)
			if got.s != tt.want {
				t.Fatalf("got %q, want %q", got.s, tt.want)
			}
		})
	}
}

func TestLevelEncoderUnmarshalText(t *testing.T) {
	tests := map[string]LevelEncoder{
		"capital": CapitalLevelEncoder,
		"":        LowercaseLevelEncoder,
		"other":   LowercaseLevelEncoder,
	}
	for text, want := range tests {
		var got LevelEncoder
		if err := got.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		// Compare behaviour, since funcs are not comparable.
		var a, b capturingEncoder
		want(InfoLevel, &a)
		got(InfoLevel, &b)
		if a.s != b.s {
			t.Errorf("UnmarshalText(%q) produced %q, want %q", text, b.s, a.s)
		}
	}
}

func TestTimeEncoders(t *testing.T) {
	ts := time.Date(2026, 9, 3, 10, 20, 30, 123456789, time.UTC)

	t.Run("epoch seconds", func(t *testing.T) {
		var got capturingEncoder
		EpochTimeEncoder(ts, &got)
		want := float64(ts.UnixNano()) / float64(time.Second)
		if got.f != want {
			t.Fatalf("got %v, want %v", got.f, want)
		}
	})
	t.Run("epoch millis", func(t *testing.T) {
		var got capturingEncoder
		EpochMillisTimeEncoder(ts, &got)
		want := float64(ts.UnixNano()) / float64(time.Millisecond)
		if got.f != want {
			t.Fatalf("got %v, want %v", got.f, want)
		}
	})
	t.Run("epoch nanos", func(t *testing.T) {
		var got capturingEncoder
		EpochNanosTimeEncoder(ts, &got)
		if got.i != ts.UnixNano() {
			t.Fatalf("got %v, want %v", got.i, ts.UnixNano())
		}
	})
	t.Run("iso8601", func(t *testing.T) {
		var got capturingEncoder
		ISO8601TimeEncoder(ts, &got)
		if !strings.Contains(got.s, "2026-09-03T10:20:30.123") {
			t.Fatalf("got %q", got.s)
		}
	})
	t.Run("rfc3339", func(t *testing.T) {
		var got capturingEncoder
		RFC3339TimeEncoder(ts, &got)
		if !strings.HasPrefix(got.s, "2026-09-03T10:20:30") {
			t.Fatalf("got %q", got.s)
		}
	})
	t.Run("rfc3339nano", func(t *testing.T) {
		var got capturingEncoder
		RFC3339NanoTimeEncoder(ts, &got)
		if !strings.Contains(got.s, "123456789") {
			t.Fatalf("got %q", got.s)
		}
	})
	t.Run("custom layout", func(t *testing.T) {
		var got capturingEncoder
		TimeEncoderOfLayout("2006/01/02 15:04")(ts, &got)
		if got.s != "2026/09/03 10:20" {
			t.Fatalf("got %q", got.s)
		}
	})
}

// layoutCapturingEncoder exercises encodeTimeLayout's fast path, which is
// used when the encoder supports AppendTimeLayout.
type layoutCapturingEncoder struct {
	capturingEncoder
	layout   string
	layoutTS time.Time
	viaFast  bool
}

func (c *layoutCapturingEncoder) AppendTimeLayout(t time.Time, layout string) {
	c.layoutTS, c.layout, c.viaFast = t, layout, true
}

func TestEncodeTimeLayoutFastPath(t *testing.T) {
	ts := time.Date(2026, 9, 3, 10, 20, 30, 0, time.UTC)

	var fast layoutCapturingEncoder
	ISO8601TimeEncoder(ts, &fast)
	if !fast.viaFast {
		t.Fatal("expected the AppendTimeLayout fast path")
	}
	if fast.layout != "2006-01-02T15:04:05.000Z0700" {
		t.Fatalf("layout = %q", fast.layout)
	}
	if !fast.layoutTS.Equal(ts) {
		t.Fatalf("time = %v", fast.layoutTS)
	}

	// Fallback path: a plain encoder receives the formatted string.
	var slow capturingEncoder
	ISO8601TimeEncoder(ts, &slow)
	if slow.kind != "string" || slow.s == "" {
		t.Fatalf("fallback path produced %+v", slow)
	}
}

func TestTimeEncoderUnmarshalText(t *testing.T) {
	tests := map[string]string{
		"rfc3339nano": "nanos-containing",
		"RFC3339Nano": "nanos-containing",
		"rfc3339":     "seconds",
		"RFC3339":     "seconds",
		"iso8601":     "iso",
		"ISO8601":     "iso",
		"millis":      "millis",
		"nanos":       "int-nanos",
		"unknown":     "float-seconds",
	}
	ts := time.Date(2026, 9, 3, 10, 20, 30, 123456789, time.UTC)

	for text, kind := range tests {
		var enc TimeEncoder
		if err := enc.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		var got capturingEncoder
		enc(ts, &got)

		switch kind {
		case "nanos-containing":
			if !strings.Contains(got.s, "123456789") {
				t.Errorf("%q: got %q", text, got.s)
			}
		case "seconds":
			if !strings.HasPrefix(got.s, "2026-09-03T10:20:30") {
				t.Errorf("%q: got %q", text, got.s)
			}
		case "iso":
			if !strings.Contains(got.s, "2026-09-03T10:20:30.123") {
				t.Errorf("%q: got %q", text, got.s)
			}
		case "millis":
			want := float64(ts.UnixNano()) / float64(time.Millisecond)
			if got.f != want {
				t.Errorf("%q: got %v, want %v", text, got.f, want)
			}
		case "int-nanos":
			if got.i != ts.UnixNano() {
				t.Errorf("%q: got %v", text, got.i)
			}
		case "float-seconds":
			want := float64(ts.UnixNano()) / float64(time.Second)
			if got.f != want {
				t.Errorf("%q: got %v, want %v", text, got.f, want)
			}
		}
	}
}

func TestDurationEncoders(t *testing.T) {
	d := 1500 * time.Millisecond

	t.Run("seconds", func(t *testing.T) {
		var got capturingEncoder
		SecondsDurationEncoder(d, &got)
		if got.f != 1.5 {
			t.Fatalf("got %v, want 1.5", got.f)
		}
	})
	t.Run("nanos", func(t *testing.T) {
		var got capturingEncoder
		NanosDurationEncoder(d, &got)
		if got.i != int64(d) {
			t.Fatalf("got %v, want %v", got.i, int64(d))
		}
	})
	t.Run("millis", func(t *testing.T) {
		var got capturingEncoder
		MillisDurationEncoder(d, &got)
		if got.i != 1500 {
			t.Fatalf("got %v, want 1500", got.i)
		}
	})
	t.Run("string", func(t *testing.T) {
		var got capturingEncoder
		StringDurationEncoder(d, &got)
		if got.s != "1.5s" {
			t.Fatalf("got %q, want 1.5s", got.s)
		}
	})
}

func TestDurationEncoderUnmarshalText(t *testing.T) {
	d := 2 * time.Second
	tests := map[string]string{
		"string":  "string",
		"nanos":   "nanos",
		"ms":      "ms",
		"":        "seconds",
		"unknown": "seconds",
	}
	for text, kind := range tests {
		var enc DurationEncoder
		if err := enc.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		var got capturingEncoder
		enc(d, &got)
		switch kind {
		case "string":
			if got.s != "2s" {
				t.Errorf("%q: got %q", text, got.s)
			}
		case "nanos":
			if got.i != int64(d) {
				t.Errorf("%q: got %v", text, got.i)
			}
		case "ms":
			if got.i != 2000 {
				t.Errorf("%q: got %v", text, got.i)
			}
		case "seconds":
			if got.f != 2.0 {
				t.Errorf("%q: got %v", text, got.f)
			}
		}
	}
}

func TestCallerEncoders(t *testing.T) {
	caller := NewEntryCaller(1, "/home/u/proj/pkg/file.go", 42, true)

	t.Run("full", func(t *testing.T) {
		var got capturingEncoder
		FullCallerEncoder(caller, &got)
		if got.s != "/home/u/proj/pkg/file.go:42" {
			t.Fatalf("got %q", got.s)
		}
	})
	t.Run("short", func(t *testing.T) {
		var got capturingEncoder
		ShortCallerEncoder(caller, &got)
		if got.s != "pkg/file.go:42" {
			t.Fatalf("got %q", got.s)
		}
	})
}

func TestCallerEncoderUnmarshalText(t *testing.T) {
	caller := NewEntryCaller(1, "/home/u/proj/pkg/file.go", 42, true)
	tests := map[string]string{
		"full":    "/home/u/proj/pkg/file.go:42",
		"":        "pkg/file.go:42",
		"unknown": "pkg/file.go:42",
	}
	for text, want := range tests {
		var enc CallerEncoder
		if err := enc.UnmarshalText([]byte(text)); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", text, err)
		}
		var got capturingEncoder
		enc(caller, &got)
		if got.s != want {
			t.Errorf("%q: got %q, want %q", text, got.s, want)
		}
	}
}

func TestNameEncoders(t *testing.T) {
	var got capturingEncoder
	FullNameEncoder("api.v1", &got)
	if got.s != "api.v1" {
		t.Fatalf("got %q", got.s)
	}
}

func TestCheckWriteActionOnWrite(t *testing.T) {
	// WriteThenNoop must not interrupt execution.
	WriteThenNoop.OnWrite(nil, nil)

	// WriteThenGoexit terminates only the calling goroutine, so run it in a
	// goroutine and observe that the deferred statement does NOT run.
	done := make(chan struct{})
	go func() {
		defer close(done)
		WriteThenGoexit.OnWrite(nil, nil)
		t.Error("execution continued after WriteThenGoexit")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WriteThenGoexit did not terminate the goroutine")
	}
}

func TestCheckWriteActionOnWritePanicAndFatal(t *testing.T) {
	// WriteThenPanic panics with the entry message.
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected a panic")
			}
			if r != "boom" {
				t.Fatalf("panic value = %v, want boom", r)
			}
		}()
		ent := Entry{Message: "boom"}
		ce := getCheckedEntry()
		ce.Entry = ent
		WriteThenPanic.OnWrite(ce, nil)
	}()

	// The zero value of CheckWriteAction is WriteThenNoop and must be a
	// valid CheckWriteHook.
	var hook CheckWriteHook = CheckWriteAction(0)
	if hook != CheckWriteHook(WriteThenNoop) {
		t.Fatal("zero CheckWriteAction must equal WriteThenNoop")
	}
}

func TestDefaultLineEndingAndOmitKey(t *testing.T) {
	if DefaultLineEnding != "\n" {
		t.Fatalf("DefaultLineEnding = %q", DefaultLineEnding)
	}
	if OmitKey != "" {
		t.Fatalf("OmitKey = %q, want empty string", OmitKey)
	}
}
