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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCloseDrainRemainingWriteFails 覆盖 Close 兜底排空时下游写入失败的
// 分支（drainRemainingToInner 里的 WriteErrors 计数）。
//
// 触发条件：worker 退出后队列里仍有残留条目，且下游 Write 返回错误。
// 通过"下游阻塞 → 队列堆积 → 释放 → 关闭"来制造残留。
func TestCloseDrainRemainingWriteFails(t *testing.T) {
	sink := newGateSink()

	cfg := (&Config{
		Dir:           t.TempDir(),
		BaseName:      "gate",
		QueueSize:     4,
		BlockOnFull:   true,
		BatchSize:     1,
		FlushInterval: time.Hour,
		Shards:        1,
	}).withDefaults()

	aw := NewAsyncWriter(sink, cfg)

	// 让 worker 进入下游 Write 并被闸门卡住。
	aw.Write([]byte("a\n"))
	sink.waitEntered()

	// 队列随后被填满，堆积若干条目。
	for i := 0; i < 8; i++ {
		select {
		case aw.shards[0] <- []byte("x\n"):
		default:
		}
	}

	// 释放闸门，但让后续写入全部失败：worker 与兜底排空都会遇到错误。
	sink.Close()
	sink.fail.Store(true)

	if err := aw.Close(); err != nil {
		t.Fatalf("Close 不应向上抛出下游错误: %v", err)
	}
	// 兜底写入失败必须被计入指标，便于监控发现。
	if got := aw.Metrics().WriteErrors.Load(); got == 0 {
		t.Fatal("兜底写入失败未被计入 WriteErrors")
	}
}

// gateSink 的 Write 首次会阻塞在闸门上；闸门释放后，若 fail 为真则返回错误。
//
// fail 由测试 goroutine 写入、由 worker goroutine 读取，必须用原子量保护，
// 否则 `go test -race` 会报数据竞争。
type gateSink struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	fail    atomic.Bool
}

func newGateSink() *gateSink {
	return &gateSink{
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
}

func (s *gateSink) waitEntered() {
	<-s.entered
}

func (s *gateSink) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	if s.fail.Load() {
		return 0, errGateSink
	}
	return len(p), nil
}

func (s *gateSink) Sync() error { return nil }

func (s *gateSink) Close() error {
	select {
	case <-s.release:
	default:
		close(s.release)
	}
	return nil
}

var errGateSink = &gateError{}

type gateError struct{}

func (*gateError) Error() string { return "gate sink write failed" }

// TestCloseDrainRemainingFailsUnderRace 在"生产者持续写入 + 下游始终失败"
// 的组合下反复关闭，覆盖 drainRemainingToInner 中写入失败并计数的分支。
//
// 需要同时满足两个条件：worker 退出后队列仍有残留；且残留写入下游时返回
// 错误。两者都是竞态结果，因此用多轮提高命中概率。
func TestCloseDrainRemainingFailsUnderRace(t *testing.T) {
	const rounds = 100

	for r := 0; r < rounds; r++ {
		sink := &failingSink{}

		cfg := (&Config{
			Dir:           t.TempDir(),
			BaseName:      "racefail",
			QueueSize:     4, // 小队列，生产者常阻塞
			BlockOnFull:   true,
			BatchSize:     100000, // 不主动 flush，让条目堆积
			FlushInterval: time.Hour,
			Shards:        1,
		}).withDefaults()

		aw := NewAsyncWriter(sink, cfg)

		// 先同步写入若干条，确保 batch 非空（否则 flush 会直接返回，
		// 不会触及下游，也就不会计数）。
		for i := 0; i < 8; i++ {
			aw.Write([]byte("z\n"))
		}

		var wg sync.WaitGroup
		for g := 0; g < 3; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					if _, err := aw.Write([]byte("z\n")); err != nil {
						return
					}
				}
			}()
		}

		time.Sleep(time.Duration(r%8) * 50 * time.Microsecond)
		if err := aw.Close(); err != nil {
			t.Fatalf("round %d: Close = %v", r, err)
		}
		wg.Wait()

		// 下游始终失败，因此写入错误必然被计数（worker 或兜底路径）。
		if got := aw.Metrics().WriteErrors.Load(); got == 0 {
			t.Fatalf("round %d: 写入失败未被计数", r)
		}
	}
}
