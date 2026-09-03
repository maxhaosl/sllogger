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
)

// failingSink 的 Write 总是返回错误，用于覆盖"关闭兜底写入失败"的分支。
type failingSink struct {
	mu    sync.Mutex
	calls int
}

func (s *failingSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return 0, errors.New("sink write failed")
}
func (s *failingSink) Sync() error  { return nil }
func (s *failingSink) Close() error { return nil }

// TestCloseDrainRemainingWriteError 覆盖 Close 兜底排空时下游写入失败的
// 分支（async.go 中 drainRemainingToInner 的 WriteErrors 计数）。
func TestCloseDrainRemainingWriteError(t *testing.T) {
	sink := &failingSink{}

	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "fail",
		QueueSize:     64,
		BlockOnFull:   true,
		BatchSize:     100000, // 很大，worker 不会主动 flush
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	aw := NewAsyncWriter(sink, cfg)

	// 写入一批数据（worker 会取走攒着，但不 flush）。
	for i := 0; i < 50; i++ {
		aw.Write([]byte("x\n"))
	}
	// 关闭时兜底排空会尝试写入这批数据，下游失败 -> 计入 WriteErrors。
	if err := aw.Close(); err != nil {
		t.Fatalf("Close 不应抛出下游错误: %v", err)
	}
	if got := aw.Metrics().WriteErrors.Load(); got == 0 {
		t.Fatal("下游写入失败未被计入 WriteErrors")
	}
}

// TestCloseDrainRemainingRace 反复在"生产者持续写入"的同时关闭，覆盖
// worker 收到 done 后排空内层队列的竞态分支（async.go run 的 done 分支）。
//
// 这个窗口很窄：drain 取空之后、内层 select 之前，生产者刚好又写入一条。
// 因此用多轮并发来提高命中概率；无论是否命中，都必须保证零丢失。
func TestCloseDrainRemainingRace(t *testing.T) {
	// 轮数需在"测试耗时"与"竞态命中率"之间权衡：这些轮次既验证零丢失，
	// 也是覆盖 worker 关闭竞态分支的主要手段。
	const rounds = 120

	// 用内存 sink：本测试只关心关闭竞态下的数据完整性，
	// 文件 IO 已由其他测试覆盖。
	sink := &countingSink{}

	for r := 0; r < rounds; r++ {
		sink.reset()

		cfg := (&Config{
			Dir:           t.TempDir(),
			BaseName:      "race",
			QueueSize:     8, // 小队列，更容易满
			BlockOnFull:   true,
			BatchSize:     100000, // 不主动 flush
			FlushInterval: time.Hour,
			Shards:        1,
		}).withDefaults()

		aw := NewAsyncWriter(sink, cfg)

		var accepted atomic.Int64
		closed := make(chan struct{})

		// 生产者：忙等写入，直到 Write 返回错误（关闭后）。
		var wg sync.WaitGroup
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					if _, err := aw.Write([]byte("y\n")); err != nil {
						return
					}
					accepted.Add(1)
				}
			}()
		}

		// 随机时刻关闭，与生产者竞争。
		time.Sleep(time.Duration(r%10) * 50 * time.Microsecond)
		go func() {
			aw.Close()
			close(closed)
		}()

		wg.Wait()
		<-closed

		// 核心断言：Write 返回 nil 的条目必须全部写入下游。
		want := accepted.Load()
		if got := sink.count(); got != want {
			t.Fatalf("round %d: 下游收到 %d 条，但 Write 成功 %d 条（数据丢失）",
				r, got, want)
		}
		if m := aw.Metrics().Snapshot(); m.Dropped != 0 {
			t.Fatalf("round %d: dropped = %d, want 0", r, m.Dropped)
		}
	}
}
