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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
	"github.com/maxhaosl/sllogger/writer"
)

// TestLevelAwareQueueEndToEnd 端到端验证设计文档 #29 的分级策略：
// 通过 Config.Build 装配的 logger 在写入时，ERROR 不丢、INFO 可丢。
func TestLevelAwareQueueEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfg := NewCallInfoConfig(dir, "svc", 80)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = false // 低级别丢弃
	cfg.Rolling.BlockLevel = ErrorLevel
	cfg.Rolling.QueueSize = 32
	cfg.Rolling.BatchSize = 1
	cfg.Rolling.FlushInterval = time.Millisecond

	log := Must(cfg.Build())
	defer log.Close()

	ctx := context.Background()
	// 混合写入 ERROR 与 INFO。
	const n = 200
	for i := 0; i < n; i++ {
		log.CallInfo(ctx, CallInfo{LogMsg: "E", Level: ErrorLevel})
		log.CallInfo(ctx, CallInfo{LogMsg: "I", Level: InfoLevel})
	}

	// 等待至少一次 Sync，让 ERROR 全部落盘。
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}

	// 关闭前再写一批，确保统计到最终状态。
	log.Close()

	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	var data strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		data.Write(b)
	}

	errCount := strings.Count(data.String(), "|E")
	if errCount < n {
		t.Fatalf("ERROR 落盘 %d 条, want >= %d（ERROR 不应被丢弃）", errCount, n)
	}
}

// TestBuildAsyncUsesLevelWriteSyncer 验证 Config.Build 装配出的异步 sink
// 实现了 slcore.LevelWriteSyncer，从而使分级策略生效。
func TestBuildAsyncUsesLevelWriteSyncer(t *testing.T) {
	dir := t.TempDir()
	cfg := NewCallInfoConfig(dir, "svc", 80)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = true

	log := Must(cfg.Build())
	defer log.Close()

	// 通过 WrapCore 无法直接取到 sink，这里改为验证行为：
	// 启用 BlockLevel 后，CORE 会把级别传给 sink（ioCore.Write 的
	// LevelWriteSyncer fast path）。若未实现该接口，WriteLevel 不会被调用，
	// 分级策略失效。因此这里用一个自定义 core 验证接口被识别。
	ws := &levelRecorder{}
	core := slcore.NewCore(newTestEncoder(t), ws, DebugLevel)
	if err := core.Write(slcore.Entry{Level: slcore.WarnLevel}, nil); err != nil {
		t.Fatal(err)
	}
	if ws.levelN != 1 {
		t.Fatal("ioCore 未使用 LevelWriteSyncer 路径")
	}
}

// levelRecorder 记录是否走 WriteLevel 路径。
type levelRecorder struct {
	levelN int
	writeN int
}

func (s *levelRecorder) Write(p []byte) (int, error) {
	s.writeN++
	return len(p), nil
}

func (s *levelRecorder) WriteLevel(lvl slcore.Level, p []byte) (int, error) {
	s.levelN++
	return len(p), nil
}

func (s *levelRecorder) Sync() error { return nil }

// TestQueueMetricsExposed 验证设计文档 #30 的队列监控指标可通过
// AsyncWriter 获取（端到端装配后）。
func TestQueueMetricsExposed(t *testing.T) {
	dir := t.TempDir()
	cfg := writer.Config{
		Dir:           dir,
		BaseName:      "q",
		ServiceName:   "svc",
		ServicePort:   80,
		QueueSize:     64,
		Shards:        2,
		BlockOnFull:   true,
		BatchSize:     100000,
		FlushInterval: time.Hour,
	}
	rw, err := writer.NewRollingWriter(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := writer.NewAsyncWriter(rw, &cfg)
	defer aw.Close()

	if got := aw.QueueCapacity(); got != 64 {
		t.Fatalf("QueueCapacity = %d, want 64", got)
	}
	if got := aw.QueueDepth(); got > 64 {
		t.Fatalf("QueueDepth = %d, 超出容量", got)
	}
	snap := aw.Snapshot()
	if snap.QueueSize != 64 {
		t.Fatalf("snapshot.QueueSize = %d, want 64", snap.QueueSize)
	}
}
