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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sllogger/slcore"
)

// levelConfig 返回一个启用了分级策略（ERROR 阻塞）的配置。
func levelConfig(dir string, queueSize int) *Config {
	return (&Config{
		Dir:           dir,
		BaseName:      "lvl",
		ServiceName:   "svc",
		ServicePort:   80,
		QueueSize:     queueSize,
		BlockOnFull:   false,
		BlockLevel:    slcore.ErrorLevel,
		BatchSize:     100000, // 不主动 flush，便于填满队列
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()
}

// TestWriteBlockingAfterClose 覆盖 writeBlocking 中"首个 closed 检查"
// 的分支：关闭后再写入必须立即返回 ErrClosed。
func TestWriteBlockingAfterClose(t *testing.T) {
	cfg := levelConfig(t.TempDir(), 8)
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	// 关闭后，ERROR 级别（走 writeBlocking）必须返回 ErrClosed。
	n, err := aw.WriteLevel(slcore.ErrorLevel, []byte("x\n"))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}
}

// TestWriteBlockingDoneBranch 覆盖 writeBlocking 中 <-a.done 的分支：
// 队列已满时，阻塞中的 ERROR 写入应因关闭而返回 ErrClosed。
func TestWriteBlockingDoneBranch(t *testing.T) {
	sink := newBlockingSink()

	cfg := (&Config{
		Dir:       t.TempDir(),
		BaseName:  "blkdone",
		QueueSize: 1,
		// BatchSize=1：worker 取到条目就 flush，从而被 blockingSink 卡住，
		// 队列随之填满 —— 这是让 writeBlocking 阻塞的前提。
		BatchSize:     1,
		BlockOnFull:   false,
		BlockLevel:    slcore.ErrorLevel,
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	aw := NewAsyncWriter(sink, cfg)

	// 第一条进入 worker 并被下游卡住（worker 停在 flush）。
	aw.WriteLevel(slcore.ErrorLevel, []byte("a\n"))
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 未进入下游 Write")
	}

	// 填满队列（容量 1）。
	aw.WriteLevel(slcore.InfoLevel, []byte("b\n"))

	// ERROR 写入会走 writeBlocking 并在队列满时阻塞。
	gotErr := make(chan error, 1)
	go func() {
		_, err := aw.WriteLevel(slcore.ErrorLevel, []byte("c\n"))
		gotErr <- err
	}()

	// 让 goroutine 进入阻塞的 select。
	time.Sleep(50 * time.Millisecond)

	// 关闭：阻塞的 writeBlocking 应命中 <-a.done 并返回 ErrClosed。
	go aw.Close()

	select {
	case err := <-gotErr:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		sink.Close()
		t.Fatal("writeBlocking 未被 done 唤醒")
	}

	// 收尾，避免 goroutine 泄漏。
	sink.Close()
	aw.Close()
}

// TestWriteLevelClosedRace 覆盖 writeBlocking 中"二次 closed 检查"的分支。
//
// 该分支位于 inFlight 注册之后，用于拦截"Closed 检查通过后、入队前"
// 恰好发生关闭的窗口。窗口极窄，因此用多轮并发提高命中概率；
// 无论是否命中，返回值都必须是 ErrClosed 或成功（不能 panic 或丢失）。
func TestWriteLevelClosedRace(t *testing.T) {
	const rounds = 200

	// 用内存 sink 代替 RollingWriter：本测试只关心 AsyncWriter 的并发语义，
	// 文件 IO 已由其他测试覆盖。去掉每轮的文件创建与读写后，耗时显著降低。
	sink := &countingSink{}

	for r := 0; r < rounds; r++ {
		// 每轮开始前清空，避免上一轮的残留影响本轮断言。
		sink.reset()

		cfg := levelConfig(t.TempDir(), 4)
		aw := NewAsyncWriter(sink, cfg)

		// 生产者持续写入 ERROR（走 writeBlocking），与 Close 竞争。
		// accepted/closed 被两个 goroutine 访问，必须用原子量（否则 -race 报错）。
		done := make(chan struct{})
		var accepted, closedCount atomic.Int64
		go func() {
			defer close(done)
			for {
				_, err := aw.WriteLevel(slcore.ErrorLevel, []byte("e\n"))
				if err != nil {
					if !errors.Is(err, ErrClosed) {
						t.Errorf("unexpected error: %v", err)
					} else {
						closedCount.Add(1)
					}
					return
				}
				accepted.Add(1)
			}
		}()

		// 随机时刻关闭，制造与 inFlight 注册之间的竞态。
		time.Sleep(time.Duration(r%5) * 50 * time.Microsecond)
		if err := aw.Close(); err != nil {
			t.Fatal(err)
		}
		<-done

		// 阻塞模式（ERROR 走 writeBlocking）下不应有丢弃。
		if m := aw.Snapshot(); m.Dropped != 0 {
			t.Fatalf("round %d: dropped = %d, want 0 for ERROR", r, m.Dropped)
		}
		// 关键断言：被接受的条目必须全部写入下游，不能丢失。
		// Close 之后 worker 已 flush 且兜底排空已完成，下游计数为最终值。
		want := accepted.Load()
		if got := sink.count(); got != want {
			t.Fatalf("round %d: 下游收到 %d 条, 接受 %d 条（应相等）", r, got, want)
		}
	}
}

// countingSink 统计写入次数，用于无文件 IO 的并发语义测试。
type countingSink struct {
	mu    sync.Mutex
	lines int
}

func (s *countingSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.lines += countLines(string(p))
	s.mu.Unlock()
	return len(p), nil
}

func (s *countingSink) Sync() error  { return nil }
func (s *countingSink) Close() error { return nil }

func (s *countingSink) count() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(s.lines)
}

func (s *countingSink) reset() {
	s.mu.Lock()
	s.lines = 0
	s.mu.Unlock()
}
