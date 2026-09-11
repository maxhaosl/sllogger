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
	"testing"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
)

func TestRegisterEncoder(t *testing.T) {
	called := false
	err := RegisterEncoder("myjson", func(cfg slcore.EncoderConfig) (slcore.Encoder, error) {
		called = true
		return encoder.NewJSONEncoder(cfg), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate registration is rejected.
	if err := RegisterEncoder("myjson", func(slcore.EncoderConfig) (slcore.Encoder, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("duplicate encoder registration should fail")
	}

	// Reserved names are rejected.
	for _, r := range []string{"json", "console"} {
		if err := RegisterEncoder(r, func(slcore.EncoderConfig) (slcore.Encoder, error) {
			return nil, nil
		}); err == nil {
			t.Fatalf("reserved encoder name %q should be rejected", r)
		}
	}

	// The registered constructor is wired into Config.buildEncoder.
	cfg := Config{Encoding: "myjson", EncoderConfig: encoder.DefaultJSONEncoderConfig()}
	enc, err := cfg.buildEncoder()
	if err != nil {
		t.Fatalf("buildEncoder failed: %v", err)
	}
	if enc == nil {
		t.Fatal("buildEncoder returned nil encoder")
	}
	if !called {
		t.Fatal("registered constructor was not invoked")
	}
}
