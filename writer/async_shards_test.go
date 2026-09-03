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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestShardsNoLoss 验证分片模式下不丢日志：总条数必须严格相等。
func TestShardsNoLoss(t *testing.T) {
	for _, shards := range []int{1, 2, 4, 8} {
		t.Run(strings.Join([]string{"shards", string(rune('0' + shards))}, "="), func(t *testing.T) {
			cfg, _ := testConfig(t)
			cfg.MaxSize = 0
			cfg.RotationInterval = 0
			cfg.Async = true
			cfg.BlockOnFull = true
			cfg.QueueSize = 4096
			cfg.BatchSize = 50
			cfg.FlushInterval = time.Millisecond
			cfg.Shards = shards

			rw, err := NewRollingWriter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			aw := NewAsyncWriter(rw, cfg)

			const (
				goroutines = 8
				perRoutine = 500
			)
			var wg sync.WaitGroup
			for g := 0; g < goroutines; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < perRoutine; i++ {
						aw.Write([]byte("line\n"))
					}
				}()
			}
			wg.Wait()
			if err := aw.Close(); err != nil {
				t.Fatal(err)
			}

			// 所有分片的条目最终都汇聚到同一个 RollingWriter。
			var total int
			files := listFiles(t, cfg.Dir)
			for _, f := range files {
				b, err := os.ReadFile(filepath.Join(cfg.Dir, f))
				if err != nil {
					t.Fatal(err)
				}
				total += strings.Count(string(b), "line")
			}
			if total != goroutines*perRoutine {
				t.Fatalf("落盘 %d 条，期望 %d 条（shards=%d）",
					total, goroutines*perRoutine, shards)
			}
			if m := aw.Metrics().Snapshot(); m.Dropped != 0 {
				t.Fatalf("dropped = %d, want 0", m.Dropped)
			}
		})
	}
}

// TestShardsSyncWaitsForAllWorkers 验证 Sync 会等待所有分片排空，
// 即 Sync 返回后所有已入队数据都已落盘。
func TestShardsSyncWaitsForAllWorkers(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.Async = true
	cfg.BlockOnFull = true
	cfg.QueueSize = 8192
	cfg.BatchSize = 1000
	cfg.FlushInterval = time.Hour // 排除定时 flush 的干扰
	cfg.Shards = 8

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	const n = 4000
	for i := 0; i < n; i++ {
		aw.Write([]byte("sync\n"))
	}
	if err := aw.Sync(); err != nil {
		t.Fatal(err)
	}

	// Sync 返回后，所有分片的数据都必须已经写入下游。
	var total int
	for _, f := range listFiles(t, cfg.Dir) {
		b, err := os.ReadFile(filepath.Join(cfg.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		total += strings.Count(string(b), "sync")
	}
	if total != n {
		t.Fatalf("Sync 后落盘 %d 条，期望 %d 条", total, n)
	}
}

// TestShardsCloseIsIdempotent 分片模式下 Close 必须幂等且排空所有分片。
func TestShardsCloseIsIdempotent(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Async = true
	cfg.BlockOnFull = true
	cfg.Shards = 4
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	for i := 0; i < 500; i++ {
		aw.Write([]byte("x\n"))
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	if got := countLines(readAll(t, rw.path)); got != 500 {
		t.Fatalf("lines = %d, want 500", got)
	}
}

// TestShardsDefaultsToOne 未配置时分片数为 1（保持原有单队列语义）。
func TestShardsDefaultsToOne(t *testing.T) {
	cfg, _ := testConfig(t)
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	if len(aw.shards) != 1 {
		t.Fatalf("shards = %d, want 1 by default", len(aw.shards))
	}
}

// TestShardsQueueCapacitySplit 总分片容量应与 QueueSize 相当。
func TestShardsQueueCapacitySplit(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.QueueSize = 1000
	cfg.Shards = 4

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	if len(aw.shards) != 4 {
		t.Fatalf("shards = %d, want 4", len(aw.shards))
	}
	total := 0
	for _, ch := range aw.shards {
		total += cap(ch)
	}
	// 每片 250，合计 1000（QueueSize 被均分）。
	if total != 1000 {
		t.Fatalf("total capacity = %d, want 1000", total)
	}
}

// TestShardsConcurrentProducers 分片下的高并发写入不丢数据、不死锁。
func TestShardsConcurrentProducers(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.Async = true
	cfg.BlockOnFull = true
	cfg.QueueSize = 1024
	cfg.Shards = 4
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	const (
		goroutines = 32
		perRoutine = 200
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perRoutine; i++ {
				aw.Write([]byte("c\n"))
			}
		}()
	}
	wg.Wait()

	done := make(chan struct{})
	go func() {
		aw.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close 在分片模式下死锁")
	}

	want := goroutines * perRoutine
	if got := countLines(readAll(t, rw.path)); got != want {
		t.Fatalf("lines = %d, want %d", got, want)
	}
}
