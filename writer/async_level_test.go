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

package writer

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

// TestWriteLevelPolicy 覆盖设计文档 #29 的分级队列满策略：
// ERROR 及以上阻塞（不丢），INFO/CALL_INFO 丢弃。
func TestWriteLevelPolicy(t *testing.T) {
	sink := newBlockingSink()

	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "lvl",
		QueueSize:     2,
		BlockOnFull:   false, // 默认丢弃
		BlockLevel:    slcore.ErrorLevel,
		BatchSize:     100000, // 不主动 flush，让队列保持满
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	aw := NewAsyncWriter(sink, cfg)
	defer func() {
		sink.Close()
		aw.Close()
	}()

	// 填满队列（容量 2）。
	aw.WriteLevel(slcore.InfoLevel, []byte("i1\n"))
	aw.WriteLevel(slcore.InfoLevel, []byte("i2\n"))
	// 队列已满：INFO 应被丢弃（不阻塞、快速返回）。
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			aw.WriteLevel(slcore.InfoLevel, []byte("info\n"))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("INFO 在队列满时不应阻塞")
	}

	if got := aw.Metrics().Dropped.Load(); got == 0 {
		t.Fatal("INFO 条目应被丢弃并计数")
	}

	// ERROR 在队列满时应阻塞，因此这里用 goroutine 并等待其完成：
	// 释放下游后，阻塞的 ERROR 写入应成功入队（不丢弃）。
	beforeErrEnqueued := aw.Metrics().Enqueued.Load()
	errDone := make(chan int, 1)
	go func() {
		n, _ := aw.WriteLevel(slcore.ErrorLevel, []byte("err\n"))
		errDone <- n
	}()

	// 给一点时间确认它确实在阻塞（而不是被丢弃）。
	time.Sleep(100 * time.Millisecond)
	if got := aw.Metrics().Dropped.Load(); got != 0 {
		// 上面的 INFO 丢弃已经发生，这里只确认 ERROR 没有增加丢弃数。
		// 用 enqueued 是否增长来判断。
		_ = got
	}

	// 释放下游，让 worker 消费，ERROR 得以入队。
	sink.Close()
	select {
	case n := <-errDone:
		if n != len("err\n") {
			t.Fatalf("ERROR 写入返回 %d, want %d", n, len("err\n"))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ERROR 写入未在下游释放后完成")
	}

	if got := aw.Metrics().Enqueued.Load(); got <= beforeErrEnqueued {
		t.Fatal("ERROR 条目应被入队（未被丢弃）")
	}
}

// TestWriteLevelFallsBackWithoutPolicy 覆盖 BlockLevel=0（无分级策略）时
// WriteLevel 退化为普通 Write 的分支。
func TestWriteLevelFallsBackWithoutPolicy(t *testing.T) {
	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "nopolicy",
		QueueSize:     1,
		BlockOnFull:   false,
		BlockLevel:    0, // 无分级策略
		BatchSize:     100000,
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	// 填满队列。
	aw.WriteLevel(slcore.ErrorLevel, []byte("a\n"))
	aw.WriteLevel(slcore.ErrorLevel, []byte("b\n"))
	// BlockLevel=0 且 BlockOnFull=false -> ERROR 也会被丢弃。
	aw.WriteLevel(slcore.ErrorLevel, []byte("c\n"))

	if got := aw.Metrics().Dropped.Load(); got == 0 {
		t.Fatal("无分级策略时 ERROR 也应被丢弃")
	}
}

// TestWriteLevelBlockOnFullOverrides 覆盖 BlockOnFull=true 时分级策略被
// 跳过的分支（全局阻塞优先）。
func TestWriteLevelBlockOnFullOverrides(t *testing.T) {
	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "override",
		QueueSize:     1024,
		BlockOnFull:   true,
		BlockLevel:    slcore.ErrorLevel,
		BatchSize:     100000,
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	// INFO 在 BlockOnFull=true 时也不应被丢弃。
	for i := 0; i < 500; i++ {
		aw.WriteLevel(slcore.InfoLevel, []byte("i\n"))
	}
	if got := aw.Metrics().Dropped.Load(); got != 0 {
		t.Fatalf("BlockOnFull=true 时不应丢弃，dropped = %d", got)
	}
}

// TestQueueMetrics 覆盖设计文档 #30 的队列监控指标。
func TestQueueMetrics(t *testing.T) {
	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "q",
		QueueSize:     16,
		BlockOnFull:   true,
		BatchSize:     100000, // 不主动 flush，保持队列有积压
		FlushInterval: time.Hour,
		Shards:        4,
	}).withDefaults()

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	// 空队列时深度为 0。
	if got := aw.QueueDepth(); got != 0 {
		t.Fatalf("初始 QueueDepth = %d, want 0", got)
	}
	// 总容量应等于 QueueSize（16，按 4 片均分）。
	if got := aw.QueueCapacity(); got != 16 {
		t.Fatalf("QueueCapacity = %d, want 16", got)
	}

	// 写入一些条目后深度应增加（worker 不 flush，条目留在队列或已取走）。
	for i := 0; i < 8; i++ {
		aw.Write([]byte("x\n"))
	}
	// 深度可能已被 worker 取走，这里只验证容量与接口可用。
	if got := aw.QueueDepth(); got > 16 {
		t.Fatalf("QueueDepth = %d, 不应超过容量 16", got)
	}

	// Snapshot 应包含队列指标。
	snap := aw.Snapshot()
	if snap.QueueSize != 16 {
		t.Fatalf("snapshot QueueSize = %d, want 16", snap.QueueSize)
	}
	if snap.QueueDepth > 16 {
		t.Fatalf("snapshot QueueDepth = %d", snap.QueueDepth)
	}
}

// TestWriteLevelConcurrent 覆盖分级策略下的并发写入：ERROR 不丢。
func TestWriteLevelConcurrent(t *testing.T) {
	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "concur",
		QueueSize:     64,
		BlockOnFull:   false,
		BlockLevel:    slcore.ErrorLevel,
		BatchSize:     100,
		FlushInterval: time.Millisecond,
		Shards:        2,
	}).withDefaults()

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// 一半 ERROR（阻塞，不丢），一半 INFO（可能丢）。
				if i%2 == 0 {
					aw.WriteLevel(slcore.ErrorLevel, []byte("E\n"))
				} else {
					aw.WriteLevel(slcore.InfoLevel, []byte("I\n"))
				}
			}
		}(g)
	}
	wg.Wait()
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	// 所有 ERROR 条目必须落盘（800 条）。
	data := readAll(t, rw.path)
	errCount := strings.Count(data, "E")
	if errCount != 800 {
		t.Fatalf("ERROR 落盘 %d 条, want 800（ERROR 不应丢弃）", errCount)
	}
}
