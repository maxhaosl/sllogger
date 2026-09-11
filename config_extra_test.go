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

	"github.com/maxhaosl/sllogger/slcore"
)

func TestNewProductionEncoderConfig(t *testing.T) {
	cfg := NewProductionEncoderConfig()
	if cfg.TimeKey != "ts" {
		t.Fatalf("TimeKey = %q, want ts", cfg.TimeKey)
	}
	if cfg.MessageKey != "msg" {
		t.Fatalf("MessageKey = %q, want msg", cfg.MessageKey)
	}
	if cfg.LevelKey != "level" {
		t.Fatalf("LevelKey = %q, want level", cfg.LevelKey)
	}
	if cfg.EncodeTime == nil {
		t.Fatal("production config should set EncodeTime")
	}
}

func TestNewDevelopmentEncoderConfig(t *testing.T) {
	cfg := NewDevelopmentEncoderConfig()
	if cfg.LevelKey != "L" {
		t.Fatalf("LevelKey = %q, want L", cfg.LevelKey)
	}
	if cfg.MessageKey != "M" {
		t.Fatalf("MessageKey = %q, want M", cfg.MessageKey)
	}
	if cfg.NameKey != "N" {
		t.Fatalf("NameKey = %q, want N", cfg.NameKey)
	}
}

func TestBuildEncoderRegistered(t *testing.T) {
	// buildEncoder should fall back to the registered encoder registry for
	// custom encodings.
	called := false
	if err := RegisterEncoder("buildenc", func(cfg slcore.EncoderConfig) (slcore.Encoder, error) {
		called = true
		return nil, nil
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	cfg := Config{Encoding: "buildenc", EncoderConfig: NewProductionEncoderConfig()}
	if _, err := cfg.buildEncoder(); err != nil {
		t.Fatalf("buildEncoder returned error: %v", err)
	}
	if !called {
		t.Fatal("registered encoder constructor was not invoked")
	}
}

func TestNewExampleProducesLogger(t *testing.T) {
	log := NewExample()
	if log == nil {
		t.Fatal("NewExample returned nil")
	}
}
