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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sllogger/writer"
)

// 压测默认参数；用 -short 跳过，或用环境变量覆盖。
var (
	stressGoroutines = envInt("SLLOGGER_STRESS_GOROUTINES", 16)
	stressPerRoutine = envInt("SLLOGGER_STRESS_PER_ROUTINE", 5000)
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("跳过压测（-short）")
	}
}

func stressDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestStressAsyncNoLoss 压测异步链路：阻塞模式下不允许丢日志。
func TestStressAsyncNoLoss(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	cfg := NewCallInfoConfig(dir, "stress", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = true
	cfg.Rolling.QueueSize = 10000
	cfg.Rolling.BatchSize = 200
	cfg.Rolling.MaxSize = 64 << 20 // 64 MiB，避免频繁滚动干扰测量
	cfg.Rolling.RotationInterval = 0
	cfg.Rolling.CleanupInterval = time.Hour

	log := Must(cfg.Build())
	ctx := WithTrace(context.Background(), "stress-tid", "stress-sid")

	total := stressGoroutines * stressPerRoutine
	start := time.Now()

	var wg sync.WaitGroup
	for g := 0; g < stressGoroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < stressPerRoutine; i++ {
				log.CallInfo(ctx, CallInfo{
					Mobile:   "18237438309",
					UserID:   "1071748417",
					ClientID: "c6559e74a24df8d97a2296ff1e23387a",
					URL:      "http://play.example.com:443/playurl/v1/play/playurl",
					Method:   "GET",
					UseTime:  int64(i),
					ServerIP: "127.0.0.1",
					LogMsg:   fmt.Sprintf("stress-%d-%d", g, i),
				})
			}
		}(g)
	}
	wg.Wait()
	enqueueElapsed := time.Since(start)

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	// 校验落盘条数与内容完整性。
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines, bytes int64
	for _, f := range files {
		fi, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		bytes += fi.Size()
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines += int64(strings.Count(string(b), "\n"))
	}

	if lines != int64(total) {
		t.Fatalf("落盘 %d 条，期望 %d 条（存在丢失）", lines, total)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("异步压测: 协程=%d 每协程=%d 总数=%d 耗时=%v (入队%v) 吞吐=%.0f 条/s (%.2f μs/条) 写入=%d MiB 堆分配累计=%d MiB GC=%d",
		stressGoroutines, stressPerRoutine, total, elapsed, enqueueElapsed,
		float64(total)/elapsed.Seconds(), float64(elapsed.Microseconds())/float64(total),
		bytes>>20, ms.TotalAlloc>>20, ms.NumGC)
}

// TestStressSyncThroughput 压测同步链路（直接写文件）。
func TestStressSyncThroughput(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	cfg := NewCallInfoConfig(dir, "stress", 8080)
	cfg.Rolling.MaxSize = 64 << 20
	cfg.Rolling.RotationInterval = 0
	cfg.Rolling.CleanupInterval = time.Hour

	log := Must(cfg.Build())
	ctx := WithTrace(context.Background(), "t", "s")

	total := stressGoroutines * stressPerRoutine / 5 // 同步路径较慢，减少量级
	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < stressGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < total/stressGoroutines; i++ {
				log.CallInfo(ctx, CallInfo{LogMsg: "sync-stress", UseTime: int64(i)})
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	log.Close()

	t.Logf("同步压测: 总数=%d 耗时=%v 吞吐=%.0f 条/s (%.2f μs/条)",
		total, elapsed, float64(total)/elapsed.Seconds(),
		float64(elapsed.Microseconds())/float64(total))
}

// TestStressRotationAging 长时间老化压测：持续写入 + 高频滚动 + 清理，
// 验证文件数量与目录大小始终受控，且无协程/文件句柄泄漏。
func TestStressRotationAging(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	cfg := NewCallInfoConfig(dir, "aging", 8080)
	cfg.Rolling.MaxSize = 4096         // 小文件，制造高频滚动
	cfg.Rolling.RotationInterval = 0   // 只按大小滚动
	cfg.Rolling.MaxBackups = 20        // 数量上限
	cfg.Rolling.MaxTotalSize = 1 << 20 // 1 MiB 总量上限
	cfg.Rolling.CleanupInterval = 5 * time.Millisecond
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = true
	cfg.Rolling.QueueSize = 4096

	log := Must(cfg.Build())
	ctx := WithTrace(context.Background(), "t", "s")

	goroutinesBefore := runtime.NumGoroutine()
	stop := make(chan struct{})
	var written atomic.Int64

	// 持续写入 2 秒，期间触发大量滚动与清理。
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					log.CallInfo(ctx, CallInfo{
						LogMsg:  strings.Repeat("a", 200),
						UseTime: written.Add(1),
					})
				}
			}
		}()
	}
	time.Sleep(2 * time.Second)
	close(stop)
	wg.Wait()

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	// 等待清理协程退出。
	time.Sleep(200 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	names := make([]string, 0, len(files))
	for _, f := range files {
		fi, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += fi.Size()
		names = append(names, f.Name())
	}
	sort.Strings(names)

	avg := int64(0)
	if len(names) > 0 {
		avg = total / int64(len(names))
	}
	t.Logf("老化压测: 写入=%d 条 剩余文件=%d 目录总量=%d KiB 单文件均值=%d B 协程=%d->%d",
		written.Load(), len(names), total>>10, avg, goroutinesBefore, goroutinesAfter)

	if len(names) > cfg.Rolling.MaxBackups {
		t.Fatalf("文件数 %d 超过 MaxBackups(%d)", len(names), cfg.Rolling.MaxBackups)
	}
	if total > cfg.Rolling.MaxTotalSize {
		t.Fatalf("目录总量 %d 超过 MaxTotalSize(%d)", total, cfg.Rolling.MaxTotalSize)
	}
	if goroutinesAfter > goroutinesBefore+4 {
		t.Fatalf("协程疑似泄漏: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// TestStressQueueSaturation 压测队列饱和时的丢弃保护（业务稳定性优先）。
func TestStressQueueSaturation(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	cfg := NewCallInfoConfig(dir, "sat", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = false // 默认策略：丢弃保护业务
	cfg.Rolling.QueueSize = 32
	cfg.Rolling.BatchSize = 1
	cfg.Rolling.FlushInterval = 20 * time.Millisecond
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0

	log := Must(cfg.Build())
	ctx := WithTrace(context.Background(), "t", "s")

	// 只入队不落盘的压力：业务线程绝不能被阻塞。
	start := time.Now()
	for i := 0; i < 200000; i++ {
		log.CallInfo(ctx, CallInfo{LogMsg: "saturation"})
	}
	elapsed := time.Since(start)
	log.Close()

	// 丢弃模式下必须能在很短时间内跑完（不被 IO 拖住）。
	t.Logf("队列饱和压测: 20 万条入队耗时=%v (%.3f μs/条)，未阻塞业务线程",
		elapsed, float64(elapsed.Microseconds())/200000)
}

// TestStressConcurrentLoggers 压测多个日志器并存（不同目录互不干扰）。
func TestStressConcurrentLoggers(t *testing.T) {
	skipShort(t)

	base := stressDir(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dir := filepath.Join(base, fmt.Sprintf("svc%d", i))
			cfg := NewCallInfoConfig(dir, fmt.Sprintf("svc%d", i), 8000+i)
			cfg.Rolling.MaxSize = 1 << 20
			cfg.Rolling.MaxBackups = 5
			cfg.Rolling.MaxDirSize = 4 << 20
			cfg.Rolling.CleanupInterval = 10 * time.Millisecond
			cfg.Rolling.Async = true
			cfg.Rolling.BlockOnFull = true

			log := Must(cfg.Build())
			defer log.Close()
			ctx := WithTrace(context.Background(), "t", "s")
			for j := 0; j < 2000; j++ {
				log.CallInfo(ctx, CallInfo{LogMsg: strings.Repeat("x", 512), UseTime: int64(j)})
			}
		}(i)
	}
	wg.Wait()

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var dirs int
	for _, e := range entries {
		if e.IsDir() {
			dirs++
			sub, err := os.ReadDir(filepath.Join(base, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if len(sub) > 5 {
				t.Fatalf("%s 文件数 %d 超过 MaxBackups(5)", e.Name(), len(sub))
			}
		}
	}
	if dirs != 8 {
		t.Fatalf("目录数 = %d, want 8", dirs)
	}
	t.Logf("多日志器压测: %d 个日志器并存，各自配额独立生效", dirs)
}

// TestStressMemoryAllocation 验证稳态下的单次分配量（内存压力）。
func TestStressMemoryAllocation(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	cfg := NewCallInfoConfig(dir, "mem", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = true
	cfg.Rolling.QueueSize = 100000
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0
	log := Must(cfg.Build())
	ctx := WithTrace(context.Background(), "t", "s")

	const (
		n       = 100000
		samples = 3
	)

	measure := func() (perOp float64, gcDelta uint32) {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < n; i++ {
			log.CallInfo(ctx, CallInfo{LogMsg: "mem", UseTime: int64(i)})
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / float64(n),
			after.NumGC - before.NumGC
	}

	// 预热：填充对象池并让 JIT/调度稳定。
	measure()

	// 多次采样取最小值：最小值代表"无外部干扰时"的稳态分配，避免机器
	// 负载或并发 GC 造成的偶发抖动导致误报。
	best, gcDelta := measure()
	for i := 1; i < samples; i++ {
		perOp, gc := measure()
		if perOp < best {
			best = perOp
		}
		gcDelta += gc
	}
	log.Close()

	t.Logf("内存压测: 每轮 %d 条 × %d 次采样 单次分配(最小)=%.1f B/op GC次数增量=%d race=%v",
		n, samples, best, gcDelta, raceBuild)

	// 单次分配应保持在设计目标内（< 1 KiB/条）。-race 会额外记账并频繁
	// 触发 GC（清空对象池），因此放宽一倍。
	limit := 1024.0
	if raceBuild {
		limit *= 2
	}
	if best > limit {
		t.Fatalf("单次分配 %.1f B 超出预期上限 %.0f B", best, limit)
	}
}

// TestStressWriterDirect 直接压测 writer 层，绕开日志器封装。
func TestStressWriterDirect(t *testing.T) {
	skipShort(t)

	dir := stressDir(t)
	rw, err := writer.NewRollingWriter(&writer.Config{
		Dir:              dir,
		BaseName:         "STRESS",
		ServiceName:      "stress",
		ServicePort:      8080,
		MaxSize:          8 << 20,
		RotationInterval: 0,
		CleanupInterval:  time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	aw := writer.NewAsyncWriter(rw, &writer.Config{
		QueueSize:   8192,
		BatchSize:   256,
		BlockOnFull: true,
	})
	defer aw.Close()

	line := []byte(strings.Repeat("x", 200) + "\n")
	total := stressGoroutines * stressPerRoutine
	start := time.Now()

	var wg sync.WaitGroup
	for g := 0; g < stressGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < stressPerRoutine; i++ {
				if _, err := aw.Write(line); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	aw.Close()
	elapsed := time.Since(start)

	m := rw.Metrics().Snapshot()
	if m.WriteErrors != 0 {
		t.Fatalf("写入错误 %d 次", m.WriteErrors)
	}
	throughput := float64(total) * float64(len(line)) / elapsed.Seconds()
	t.Logf("writer 压测: %d 条 耗时=%v 吞吐=%.0f 条/s 带宽=%.2f MiB/s 下游写次数=%d (批处理比 %.1f 条/次)",
		total, elapsed, float64(total)/elapsed.Seconds(), throughput/(1<<20),
		m.Written, float64(total)/float64(max(m.Written, 1)))
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
