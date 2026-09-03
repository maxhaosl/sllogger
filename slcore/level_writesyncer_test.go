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
	"strings"
	"testing"
)

// levelSink records the level it received via WriteLevel.
type levelSink struct {
	buf      strings.Builder
	levels   []Level
	writeN   int
	levelN   int
	levelErr error
}

func (s *levelSink) Write(p []byte) (int, error) {
	s.writeN++
	return s.buf.Write(p)
}

func (s *levelSink) WriteLevel(lvl Level, p []byte) (int, error) {
	s.levelN++
	s.levels = append(s.levels, lvl)
	if s.levelErr != nil {
		return 0, s.levelErr
	}
	return s.buf.Write(p)
}

func (s *levelSink) Sync() error { return nil }

// TestLevelWriteSyncerUsedByCore 覆盖 ioCore.Write 中
// LevelWriteSyncer 的 fast-path 分支。
func TestLevelWriteSyncerUsedByCore(t *testing.T) {
	sink := &levelSink{}
	core := NewCore(newStubEncoder("out"), sink, DebugLevel)

	if err := core.Write(Entry{Level: ErrorLevel}, nil); err != nil {
		t.Fatal(err)
	}
	if err := core.Write(Entry{Level: InfoLevel}, nil); err != nil {
		t.Fatal(err)
	}

	if sink.levelN != 2 {
		t.Fatalf("WriteLevel calls = %d, want 2", sink.levelN)
	}
	if sink.writeN != 0 {
		t.Fatalf("Write calls = %d, want 0 (level path preferred)", sink.writeN)
	}
	if len(sink.levels) != 2 || sink.levels[0] != ErrorLevel || sink.levels[1] != InfoLevel {
		t.Fatalf("levels = %v, want [error info]", sink.levels)
	}
}

// TestPlainWriteSyncerFallback 覆盖 ioCore.Write 中 sink 未实现
// LevelWriteSyncer 时回退到普通 Write 的分支。
func TestPlainWriteSyncerFallback(t *testing.T) {
	sink := &countingWriteSyncer{}
	core := NewCore(newStubEncoder("out"), sink, DebugLevel)

	if err := core.Write(Entry{Level: ErrorLevel}, nil); err != nil {
		t.Fatal(err)
	}
	writes, _ := sink.counts()
	if writes != 1 {
		t.Fatalf("writes = %d, want 1", writes)
	}
	if got := sink.String(); got != "out\n" {
		t.Fatalf("content = %q", got)
	}
}

// TestLevelWriteSyncerErrorPropagates 覆盖 WriteLevel 返回错误时
// 向上传播的分支。
func TestLevelWriteSyncerErrorPropagates(t *testing.T) {
	sink := &levelSink{levelErr: errStub}
	core := NewCore(newStubEncoder("out"), sink, DebugLevel)

	if err := core.Write(Entry{Level: ErrorLevel}, nil); err != errStub {
		t.Fatalf("err = %v, want %v", errStub, errStub)
	}
}

// TestLevelWriteSyncerWithClone 覆盖 With 派生 core 时 LevelWriteSyncer
// 仍被使用（out 是共享的）。
func TestLevelWriteSyncerWithClone(t *testing.T) {
	sink := &levelSink{}
	core := NewCore(newStubEncoder("out"), sink, DebugLevel)
	child := core.With([]Field{{Key: "k", Type: StringType, String: "v"}})

	if err := child.Write(Entry{Level: WarnLevel}, nil); err != nil {
		t.Fatal(err)
	}
	if sink.levelN != 1 || sink.levels[0] != WarnLevel {
		t.Fatalf("levels = %v", sink.levels)
	}
}

// TestLevelWriteSyncerInterfaceCompliance 确认接口定义正确。
func TestLevelWriteSyncerInterfaceCompliance(t *testing.T) {
	var _ LevelWriteSyncer = (*levelSink)(nil)
	var _ WriteSyncer = (*levelSink)(nil)
}
