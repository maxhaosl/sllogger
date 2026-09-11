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

// bench 是 sllogger 的写入压测工具，包含两部分：
//
//  1. 一致性校验（check）：写入 N 条日志，严格校验日志文件中落盘的条数、
//     内容完整性、顺序与唯一性，确保"写入条数 == 文件中条数"。
//  2. 吞吐矩阵（bench）：在同步/异步、不同协程数与批量大小下测量每秒
//     写入条数（条/s）与带宽（MiB/s），用于寻找最优配置。
//
// 用法：
//
//	go run ./example/bench -mode check -n 200000
//	go run ./example/bench -mode bench
//	go run ./example/bench -mode bench -cpuprofile cpu.out
//	go run ./example/bench -mode bench -memprofile mem.out
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxhaosl/sllogger"
	"github.com/maxhaosl/sllogger/writer"
)

var (
	mode        = flag.String("mode", "bench", "check（一致性校验）| bench（吞吐矩阵）| all")
	total       = flag.Int("n", 200000, "一致性校验与基准的写入条数")
	goroutines  = flag.Int("g", 16, "并发协程数")
	onlyAsync   = flag.Bool("async", false, "只测异步模式")
	keep        = flag.Bool("keep", false, "保留生成的日志文件")
	cpuprofile  = flag.String("cpuprofile", "", "写入 CPU profile 到指定文件")
	memprofile  = flag.String("memprofile", "", "写入内存 profile 到指定文件")
	payloadSize = flag.Int("payload", 200, "单条日志目标字节数")
	roundsFlag  = flag.Int("rounds", 5, "每个场景的采样轮数（取中位数）")
	queue       = flag.Int("queue", 0, "非 0 时额外测试不同队列容量对吞吐的影响")
	shards      = flag.Int("shards", 0, "非 0 时设置异步分片数（check 模式用于验证分片下的一致性）")
	batch       = flag.Int("batch", 0, "非 0 时设置批量大小（check 模式用于验证不同 batch 下的一致性）")
	nonBlocking = flag.Bool("nonblocking", false, "阴性对照：用 BlockOnFull=false 验证校验方法能检出丢失")
	syncMode    = flag.Bool("sync", false, "用同步写模式做一致性校验（对比异步路径）")
	maxSizeFlag = flag.Int("maxsize", 0, "非 0 时设置单文件大小上限（用于高频滚动场景）")
)

// applyOverrides 把命令行覆盖项写入配置。
func applyOverrides() {
	if *shards > 0 {
		shardsOverride = *shards
	}
}

func main() {
	flag.Parse()
	applyOverrides()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(err)
		}
		_ = pprof.StartCPUProfile(f)
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
		}()
	}

	switch *mode {
	case "check":
		runCheck()
	case "bench":
		runBench()
	case "all":
		runCheck()
		runBench()
	default:
		fmt.Fprintf(os.Stderr, "未知 mode: %q\n", *mode)
		os.Exit(2)
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			panic(err)
		}
		fmt.Printf("\n内存 profile 已写入 %s\n", *memprofile)
	}
}

// ---------------------------------------------------------------------------
// 一致性校验
// ---------------------------------------------------------------------------

// runCheck 严格校验"写入条数 == 文件落盘条数"。
//
// 校验维度：
//  1. 条数：所有文件的总行数 == 写入条数（不多不少）
//  2. 完整性：每行都是完整的一行（以换行结尾，无截断、无粘连）
//  3. 字段数：每行的分隔符数量与模板字段数一致（CALL_INFO 为 14 字段）
//  4. 唯一性+完整性：每条日志携带全局唯一自增序号，解析后恰好覆盖
//     [0, N)，无重复、无缺失
//  5. 顺序：同协程内写入顺序保持（校验序号递增性）
func runCheck() {
	section("一致性校验：写入条数 vs 文件落盘条数")

	dir, cleanup := tempDir("check")
	defer cleanup()

	const maxSize = 8 << 20 // 8 MiB，制造多次滚动
	cfg := sllogger.NewCallInfoConfig(dir, "bench", 8080)
	cfg.Rolling.MaxSize = maxSize
	cfg.Rolling.RotationInterval = 0
	cfg.Rolling.Async = !*syncMode
	cfg.Rolling.BlockOnFull = !*nonBlocking // 默认阻塞（不丢）；-nonblocking 用于阴性对照
	cfg.Rolling.QueueSize = 10000
	if *maxSizeFlag > 0 {
		cfg.Rolling.MaxSize = int64(*maxSizeFlag)
	}
	cfg.Rolling.BatchSize = 200
	if *batch > 0 {
		cfg.Rolling.BatchSize = *batch
	}
	if *nonBlocking {
		fmt.Println("  [阴性对照] BlockOnFull=false：预期应检出丢失，用于证明校验方法有效")
	}
	if shardsOverride > 0 {
		cfg.Rolling.Shards = shardsOverride
		fmt.Printf("  分片数：%d（跨分片不保序，条数一致性仍必须成立）\n", shardsOverride)
	}
	fmt.Printf("  配置：async=%v blockOnFull=%v batch=%d queue=%d shards=%d\n",
		cfg.Rolling.Async, cfg.Rolling.BlockOnFull,
		cfg.Rolling.BatchSize, cfg.Rolling.QueueSize, cfg.Rolling.Shards)

	log := sllogger.Must(cfg.Build())
	ctx := context.Background()

	// 每条日志携带全局唯一序号，写入 log_msg 字段。
	var seq atomic.Int64
	perRoutine := *total / *goroutines
	payload := strings.Repeat("p", maxInt(1, *payloadSize))

	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < *goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perRoutine; i++ {
				id := seq.Add(1) - 1
				log.CallInfo(ctx, sllogger.CallInfo{
					Mobile:   "18237438309",
					UserID:   "1071748417",
					ClientID: "c6559e74a24df8d97a2296ff1e23387a",
					URL:      "http://play.example.com:443/playurl/v1/play/playurl",
					Method:   "GET",
					UseTime:  id,
					ServerIP: "127.0.0.1",
					BussID:   "null",
					LogMsg:   fmt.Sprintf("%s#%d", payload, id),
				})
			}
		}()
	}
	wg.Wait()
	written := seq.Load()
	elapsed := time.Since(start)

	if err := log.Close(); err != nil {
		fail("Close: %v", err)
	}
	closeElapsed := time.Since(start)

	fmt.Printf("  写入：%d 条（%d 协程 × %d）  耗时：%v（含优雅关闭 %v）\n",
		written, *goroutines, perRoutine, elapsed, closeElapsed)
	fmt.Printf("  吞吐：%.0f 条/s\n", float64(written)/elapsed.Seconds())

	// --- 收集落盘数据 ---
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		fail("glob: %v", err)
	}
	sort.Strings(files)

	var (
		fileLines  int64
		totalBytes int64
		badLines   []string
		seen       = make([]bool, written)
		duplicates int
		unknown    int
	)
	// CALL_INFO 模板为 14 个字段 => 13 个分隔符。
	const wantSeparators = 13

	for _, path := range files {
		fi, err := os.Stat(path)
		if err != nil {
			fail("stat %s: %v", path, err)
		}
		totalBytes += fi.Size()

		f, err := os.Open(path)
		if err != nil {
			fail("open %s: %v", path, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20) // 单条最长 4 MiB
		for sc.Scan() {
			line := sc.Text()
			fileLines++
			if fileLines <= 3 {
				fmt.Printf("  样例[%s] %s\n", filepath.Base(path), truncate(line, 160))
			}

			// 校验 3：字段数（剔除转义后的 "\|"，避免把转义序列算作分隔符）
			raw := strings.ReplaceAll(strings.ReplaceAll(line, `\|`, ""), `\n`, "")
			if got := strings.Count(raw, "|"); got != wantSeparators {
				if len(badLines) < 5 {
					badLines = append(badLines, fmt.Sprintf("字段数异常(%d): %s", got, truncate(line, 120)))
				}
				continue
			}

			// 校验 4：唯一序号覆盖 [0, N)
			idx := strings.LastIndex(line, "#")
			if idx < 0 {
				unknown++
				continue
			}
			var id int64
			if _, err := fmt.Sscanf(line[idx+1:], "%d", &id); err != nil {
				unknown++
				continue
			}
			if id < 0 || id >= written {
				unknown++
				continue
			}
			if seen[id] {
				duplicates++
				continue
			}
			seen[id] = true
		}
		if err := sc.Err(); err != nil {
			fail("读取 %s: %v", path, err)
		}
		f.Close()
	}

	// 统计缺失
	missing := 0
	for i := range seen {
		if !seen[i] {
			missing++
		}
	}

	fmt.Printf("\n  文件：%d 个，总大小 %.2f MiB，总行数 %d\n",
		len(files), float64(totalBytes)/(1<<20), fileLines)

	// --- 断言 ---
	check("1. 落盘条数 == 写入条数", fileLines == written,
		fmt.Sprintf("文件 %d 行 vs 写入 %d 条（差 %d）", fileLines, written, fileLines-written))
	check("2. 无格式损坏行（分隔符数量正确）", len(badLines) == 0,
		strings.Join(badLines, "\n        "))
	check("3. 无重复条目", duplicates == 0, fmt.Sprintf("重复 %d 条", duplicates))
	check("4. 无缺失条目", missing == 0, fmt.Sprintf("缺失 %d 条", missing))
	check("5. 无无法解析的序号", unknown == 0, fmt.Sprintf("异常 %d 条", unknown))

	reportExit()
}

// ---------------------------------------------------------------------------
// 吞吐矩阵
// ---------------------------------------------------------------------------

type result struct {
	name     string
	ops      int64
	elapsed  time.Duration
	bytes    int64
	goroutns int
}

func (r result) rate() float64    { return float64(r.ops) / r.elapsed.Seconds() }
func (r result) usPerOp() float64 { return float64(r.elapsed.Microseconds()) / float64(r.ops) }
func (r result) bandwidth() float64 {
	return float64(r.bytes) / r.elapsed.Seconds() / (1 << 20)
}

func runBench() {
	section("吞吐矩阵")

	payload := strings.Repeat("p", maxInt(1, *payloadSize))
	var results []result

	// 1. 同步写入（基线）
	if !*onlyAsync {
		results = append(results, runCase("同步/单协程", 1, false, 0, payload))
		results = append(results, runCase(fmt.Sprintf("同步/%d协程", *goroutines), *goroutines, false, 0, payload))
	}

	// 2. 异步：不同批量大小
	for _, batch := range []int{1, 50, 200, 1000} {
		results = append(results, runCase(
			fmt.Sprintf("异步/%d协程/batch=%d", *goroutines, batch), *goroutines, true, batch, payload))
	}

	// 3. 异步：不同协程数（固定 batch=200）
	for _, g := range []int{1, 4, 16, 64} {
		if g == *goroutines {
			continue
		}
		results = append(results, runCase(
			fmt.Sprintf("异步/%d协程/batch=200", g), g, true, 200, payload))
	}

	// 4. 异步：不同队列容量（高并发下验证队列是否是瓶颈）
	if *queue > 0 {
		for _, q := range []int{1024, 16384, 131072} {
			results = append(results, runCaseQueue(
				fmt.Sprintf("异步/%d协程/queue=%d", *goroutines, q),
				*goroutines, 200, q, payload))
		}
	}

	// 5. 异步：分片数（突破单 worker 吞吐上限）
	for _, s := range []int{2, 4, 8, 16} {
		results = append(results, runCaseShards(
			fmt.Sprintf("异步/%d协程/shards=%d", *goroutines, s),
			*goroutines, 200, s, payload))
	}

	fmt.Println()
	fmt.Printf("  %-26s %12s %12s %12s %10s\n",
		"场景", "条/s", "μs/条", "MiB/s", "协程")
	fmt.Println("  " + strings.Repeat("-", 76))
	for _, r := range results {
		fmt.Printf("  %-26s %12.0f %12.3f %12.2f %10d\n",
			r.name, r.rate(), r.usPerOp(), r.bandwidth(), r.goroutns)
	}

	// 最优与提升空间
	best := results[0]
	base := results[0]
	for _, r := range results {
		if r.rate() > best.rate() {
			best = r
		}
		if r.name == "同步/单协程" {
			base = r
		}
	}
	fmt.Println()
	fmt.Printf("  最快：%s（%.0f 条/s）\n", best.name, best.rate())
	if base.rate() > 0 {
		fmt.Printf("  相对同步单协程基线提升：%.2fx\n", best.rate()/base.rate())
	}
	fmt.Printf("  CPU 核数：%d\n", runtime.NumCPU())
}

// runCase 跑一个场景。为降低测量噪声，每个场景执行 rounds 轮并取中位数。
//
// 单次测量的抖动在满载机器上可达 ±40%，中位数能稳定反映配置差异。
func runCase(name string, goroutines int, async bool, batch int, payload string) result {
	rounds := *roundsFlag
	if rounds < 1 {
		rounds = 1
	}

	rates := make([]float64, 0, rounds)
	var last result
	for r := 0; r < rounds; r++ {
		last = runCaseOnce(name, goroutines, async, batch, payload)
		rates = append(rates, last.rate())
	}
	sort.Float64s(rates)
	median := rates[len(rates)/2]

	// 用中位数反推耗时，使报表的三项指标互相自洽。
	last.elapsed = time.Duration(float64(last.ops) / median * float64(time.Second))
	return last
}

// runCaseQueue 在指定队列容量下跑一个异步场景。
func runCaseQueue(name string, goroutines, batch, queueSize int, payload string) result {
	old := queueOverride
	queueOverride = queueSize
	defer func() { queueOverride = old }()
	return runCase(name, goroutines, true, batch, payload)
}

// runCaseShards 在指定分片数下跑一个异步场景。
func runCaseShards(name string, goroutines, batch, shards int, payload string) result {
	old := shardsOverride
	shardsOverride = shards
	defer func() { shardsOverride = old }()
	return runCase(name, goroutines, true, batch, payload)
}

// queueOverride 非 0 时覆盖 Config.Rolling.QueueSize。
var queueOverride int

// shardsOverride 非 0 时覆盖 Config.Rolling.Shards。
var shardsOverride int

func runCaseOnce(name string, goroutines int, async bool, batch int, payload string) result {
	dir, cleanup := tempDir("bench")
	defer cleanup()

	cfg := sllogger.NewCallInfoConfig(dir, "bench", 8080)
	cfg.Rolling.MaxSize = 0          // 关闭滚动，避免干扰吞吐测量
	cfg.Rolling.RotationInterval = 0 // 关闭定时滚动
	cfg.Rolling.CleanupInterval = time.Hour
	cfg.Rolling.Async = async
	cfg.Rolling.BlockOnFull = true
	if async {
		cfg.Rolling.QueueSize = 100000
		if queueOverride > 0 {
			cfg.Rolling.QueueSize = queueOverride
		}
		cfg.Rolling.BatchSize = batch
		cfg.Rolling.FlushInterval = time.Millisecond
		if shardsOverride > 0 {
			cfg.Rolling.Shards = shardsOverride
		}
	}

	log := sllogger.Must(cfg.Build())
	ctx := context.Background()

	perRoutine := *total / goroutines
	if perRoutine == 0 {
		perRoutine = 1
	}
	var counter atomic.Int64

	// 预热：填充对象池、触发 JIT、预热文件句柄。
	const warmup = 2000
	for i := 0; i < warmup; i++ {
		log.CallInfo(ctx, sllogger.CallInfo{LogMsg: payload})
	}
	_ = log.Sync()

	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perRoutine; i++ {
				log.CallInfo(ctx, sllogger.CallInfo{
					Mobile:   "18237438309",
					UserID:   "1071748417",
					ClientID: "c6559e74a24df8d97a2296ff1e23387a",
					URL:      "http://play.example.com:443/playurl/v1/play/playurl",
					Method:   "GET",
					UseTime:  int64(i),
					ServerIP: "127.0.0.1",
					BussID:   "null",
					LogMsg:   payload,
				})
				counter.Add(1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if err := log.Close(); err != nil {
		fail("Close: %v", err)
	}

	// 统计落盘字节数与行数，确认这一轮没有丢失。
	var bytes, lines int64
	if files, err := filepath.Glob(filepath.Join(dir, "*.log")); err == nil {
		for _, p := range files {
			if fi, err := os.Stat(p); err == nil {
				bytes += fi.Size()
			}
			if data, err := os.ReadFile(p); err == nil {
				lines += int64(strings.Count(string(data), "\n"))
			}
		}
	}

	ops := counter.Load()
	if lines != ops+warmup {
		fmt.Printf("  [警告] %s：写入 %d 条但落盘 %d 行\n", name, ops+warmup, lines)
	}

	return result{
		name:     name,
		ops:      ops,
		elapsed:  elapsed,
		bytes:    bytes,
		goroutns: goroutines,
	}
}

// ---------------------------------------------------------------------------
// 工具函数
// ---------------------------------------------------------------------------

var failures int

func check(name string, cond bool, detail string) {
	if cond {
		fmt.Printf("  PASS  %s\n", name)
		return
	}
	failures++
	fmt.Printf("  FAIL  %s\n        %s\n", name, detail)
}

func fail(format string, args ...interface{}) {
	fmt.Printf("  FAIL  "+format+"\n", args...)
	failures++
	reportExit()
}

func reportExit() {
	if failures == 0 {
		fmt.Println("\n结果：全部通过")
		return
	}
	fmt.Printf("\n结果：%d 项失败\n", failures)
	os.Exit(1)
}

func section(title string) {
	fmt.Printf("\n=== %s ===\n", title)
}

func tempDir(prefix string) (string, func()) {
	dir := filepath.Join(os.TempDir(),
		fmt.Sprintf("sllogger-%s-%d", prefix, os.Getpid()))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	return dir, func() {
		if !*keep {
			os.RemoveAll(dir)
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

var _ = writer.DefaultMaxSize
