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
	"testing"
	"time"
)

// blockingSink 的 Write 会阻塞，直到测试释放 release。
// 用它把 worker 卡在 flush 阶段，从而让队列被填满、生产者阻塞在 select，
// 以确定性触发 Write 中 <-a.done 的分支。
type blockingSink struct {
	release chan struct{}
	entered chan struct{} // 首次进入 Write 时通知
	once    sync.Once
	mu      sync.Mutex
	n       int
}

func newBlockingSink() *blockingSink {
	return &blockingSink{
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
}

func (s *blockingSink) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release // 阻塞直到测试释放
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return len(p), nil
}

func (s *blockingSink) Sync() error { return nil }
func (s *blockingSink) Close() error {
	// 幂等释放，避免 Close 时 worker 永久阻塞。
	select {
	case <-s.release:
	default:
		close(s.release)
	}
	return nil
}

// TestAsyncWriteBlockedThenDone 覆盖阻塞模式下 Write 因 <-a.done 就绪而
// 返回 ErrClosed 的分支。
//
// 触发方式：让下游阻塞使 worker 停在 flush，队列随之填满；此时生产者的
// Write 会阻塞在 select 中；随后 Close 关闭 done，阻塞的 Write 命中
// <-a.done 分支。
func TestAsyncWriteBlockedThenDone(t *testing.T) {
	sink := newBlockingSink()

	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "blk",
		QueueSize:     2,
		BlockOnFull:   true,
		BatchSize:     1,
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	aw := NewAsyncWriter(sink, cfg)

	// 先写入一条，让 worker 取出并进入 flush（BatchSize=1，取到即 flush），
	// 从而被 blockingSink 卡住，队列不再被消费。
	aw.Write([]byte("a\n"))

	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 未进入下游 Write")
	}

	// worker 已卡住，填满队列（容量 2），使后续写入阻塞在 select 中。
	aw.Write([]byte("b\n"))
	aw.Write([]byte("c\n"))

	gotErr := make(chan error, 1)
	go func() {
		// 队列已满，这里会阻塞在 select；Close 后应命中 <-a.done。
		_, err := aw.Write([]byte("c\n"))
		gotErr <- err
	}()

	// 给生产者一点时间进入阻塞的 select。
	time.Sleep(50 * time.Millisecond)

	// Close 会关闭 done，唤醒阻塞的生产者。
	go aw.Close()

	select {
	case err := <-gotErr:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		// 释放下游，避免 goroutine 泄漏影响后续测试。
		sink.Close()
		t.Fatal("阻塞的 Write 未被 done 唤醒")
	}

	// 收尾：释放下游并等待 Close 完成。
	sink.Close()
	aw.Close()
}

// TestAsyncWriteDoneDrainsQueuedEntries 覆盖 worker 收到 done 后排空
// 内层队列的分支：关闭时队列里仍有数据，这些数据必须全部落盘。
//
// 这里用多轮并发提高命中"drain 之后仍有新条目入队"这一竞态窗口的概率。
func TestAsyncWriteDoneDrainsQueuedEntries(t *testing.T) {
	const rounds = 120
	hits := 0

	// 用内存 sink：本测试只关心 AsyncWriter 关闭时的排空语义，
	// 文件 IO 已由其他测试覆盖。去掉文件读写后耗时显著降低。
	sink := &countingSink{}

	for r := 0; r < rounds; r++ {
		sink.reset()

		cfg := (&Config{
			Dir:           t.TempDir(),
			BaseName:      "drain",
			QueueSize:     64,
			BlockOnFull:   true,
			BatchSize:     100000, // 很大，避免中途 flush
			FlushInterval: time.Hour,
			Shards:        1,
		}).withDefaults()

		aw := NewAsyncWriter(sink, cfg)

		// 并发生产者持续写入，**与 Close 同时进行**：这样才有机会让某条
		// 数据在 worker 的 drain 完成之后、内层 select 之前入队，
		// 从而覆盖 done 分支里内层队列的排空逻辑。
		var wg sync.WaitGroup
		var written int64
		var mu sync.Mutex
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					_, err := aw.Write([]byte("x\n"))
					if err != nil {
						// 关闭后 Write 返回 ErrClosed，生产者随之退出。
						if !errors.Is(err, ErrClosed) {
							mu.Lock()
							_ = err
							mu.Unlock()
						}
						return
					}
					mu.Lock()
					written++
					mu.Unlock()
				}
			}()
		}

		// 随机时刻关闭，制造与生产者的竞态。
		time.Sleep(time.Duration(r%5) * 100 * time.Microsecond)
		if err := aw.Close(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()

		// 无论是否命中竞态窗口，成功入队的条目都必须全部写入下游。
		mu.Lock()
		want := written
		mu.Unlock()
		if got := sink.count(); got != want {
			t.Fatalf("round %d: 下游收到 %d 条，期望 %d 条", r, got, want)
		}
		if m := aw.Metrics().Snapshot(); m.Dropped != 0 {
			t.Fatalf("round %d: dropped = %d, want 0 (阻塞模式)", r, m.Dropped)
		}
		hits++
	}

	if hits != rounds {
		t.Fatalf("只完成 %d/%d 轮", hits, rounds)
	}
}
