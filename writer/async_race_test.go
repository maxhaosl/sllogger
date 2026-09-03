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
	"testing"
	"time"
)

// TestAsyncWriterConcurrentSyncAndClose 在关闭的同时并发 Sync/Write，
// 覆盖 Sync 等待 worker 退出（<-a.done）的路径。该路径只在极窄的竞态
// 窗口命中，因此这里用多轮并发来稳定触达，并断言"不死锁、不 panic"。
func TestAsyncWriterConcurrentSyncAndClose(t *testing.T) {
	for round := 0; round < 50; round++ {
		cfg, _ := testConfig(t)
		cfg.MaxSize = 0
		cfg.RotationInterval = 0
		cfg.FlushInterval = time.Millisecond
		rw, err := NewRollingWriter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		aw := NewAsyncWriter(rw, cfg)

		var wg sync.WaitGroup
		// 并发写入。
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					aw.Write([]byte("x\n"))
				}
			}()
		}
		// 并发 Sync：与 Close 竞争，命中 Sync 的 <-a.done 分支。
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					aw.Sync()
				}
			}()
		}
		// 在写入/Sync 进行中关闭。
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(round%5) * 100 * time.Microsecond)
			aw.Close()
		}()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("round %d: deadlock between Sync and Close", round)
		}
	}
}

// TestAsyncWriterSyncBlocksUntilFlushed 验证 Sync 返回后数据已落到下游。
func TestAsyncWriterSyncBlocksUntilFlushed(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.FlushInterval = time.Hour // 排除定时 flush 的干扰
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	for i := 0; i < 500; i++ {
		aw.Write([]byte("line\n"))
	}
	// Sync 必须等到所有已入队数据写入下游后才返回。
	if err := aw.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := rw.Metrics().Written.Load(); got == 0 {
		t.Fatal("Sync returned before anything was written downstream")
	}
	if got := countLines(readAll(t, rw.path)); got != 500 {
		t.Fatalf("lines = %d, want 500", got)
	}
}

func countLines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
