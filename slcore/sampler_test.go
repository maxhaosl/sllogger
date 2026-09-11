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

// sample runs Check+Write n times against a sampler wrapping core, using the
// same message/level so every entry collides on the same counter bucket.
func sample(core *spyCore, level Level, msg string, n, first, thereafter int) {
	s := NewSampler(core, time.Hour, first, thereafter)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		e := Entry{Level: level, Message: msg, Time: now}
		ce := s.Check(e, nil)
		if ce != nil {
			ce.Write()
		}
	}
}

func TestSamplerBasic(t *testing.T) {
	core := newSpyCore(DebugLevel)
	sample(core, InfoLevel, "dup", 10, 2, 0)
	if got := len(core.rec.writes); got != 2 {
		t.Fatalf("expected 2 sampled writes (first,after), got %d", got)
	}
}

func TestSamplerThereafterKeepsAll(t *testing.T) {
	core := newSpyCore(DebugLevel)
	// thereafter=1 means every subsequent entry after the first N is kept.
	sample(core, InfoLevel, "dup", 10, 2, 1)
	if got := len(core.rec.writes); got != 10 {
		t.Fatalf("expected all 10 writes, got %d", got)
	}
}

func TestSamplerEveryThird(t *testing.T) {
	core := newSpyCore(DebugLevel)
	// first=2, thereafter=3: sampled for n in {1,2,5,8,11} within 11 entries.
	sample(core, InfoLevel, "dup", 11, 2, 3)
	if got := len(core.rec.writes); got != 5 {
		t.Fatalf("expected 5 sampled writes, got %d", got)
	}
}

func TestSamplerDistinctMessages(t *testing.T) {
	core := newSpyCore(DebugLevel)
	for m := 0; m < 3; m++ {
		sample(core, InfoLevel, "msg"+string(rune('A'+m)), 10, 2, 0)
	}
	if got := len(core.rec.writes); got != 6 {
		t.Fatalf("expected 6 writes (2 per distinct message), got %d", got)
	}
}

func TestSamplerDisabledLevel(t *testing.T) {
	core := newSpyCore(WarnLevel) // enabled only at Warn+
	s := NewSampler(core, time.Hour, 2, 0)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		e := Entry{Level: InfoLevel, Message: "dup", Time: now} // below Warn
		if ce := s.Check(e, nil); ce != nil {
			ce.Write()
		}
	}
	if got := len(core.rec.writes); got != 0 {
		t.Fatalf("expected 0 writes for disabled level, got %d", got)
	}
}

func TestSamplerHook(t *testing.T) {
	core := newSpyCore(DebugLevel)
	var sampled, dropped int
	hook := SamplerHook(func(_ Entry, dec SamplingDecision) {
		if dec&LogSampled != 0 {
			sampled++
		}
		if dec&LogDropped != 0 {
			dropped++
		}
	})
	s := NewSamplerWithOptions(core, time.Hour, 2, 0, hook)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		e := Entry{Level: InfoLevel, Message: "dup", Time: now}
		if ce := s.Check(e, nil); ce != nil {
			ce.Write()
		}
	}
	if sampled != 2 {
		t.Fatalf("expected 2 sampled decisions, got %d", sampled)
	}
	if dropped != 8 {
		t.Fatalf("expected 8 dropped decisions, got %d", dropped)
	}
}

func TestSamplerWithOptionsDefaultHook(t *testing.T) {
	core := newSpyCore(DebugLevel)
	// Without a hook, NewSamplerWithOptions uses a no-op hook and still samples.
	s := NewSamplerWithOptions(core, time.Hour, 2, 0)
	sampleWith(s, InfoLevel, "dup", 10)
	if got := len(core.rec.writes); got != 2 {
		t.Fatalf("expected 2 sampled writes, got %d", got)
	}
}

func sampleWith(s Core, level Level, msg string, n int) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		e := Entry{Level: level, Message: msg, Time: now}
		if ce := s.Check(e, nil); ce != nil {
			ce.Write()
		}
	}
}
