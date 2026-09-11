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

// Command feign 演示 sllogger 按小时切分日志（LOG_FEIGN.{yyyy-MM-dd.hh}.{serverName}{serverPort}.log）
// 并保留 3 天（72h）后定时清理，且同目录其它类型日志（如 LOG_MGMONITOR.*）不受影响。
//
// 为了能在几秒内演示“跨多小时 / 跨多天”的切分与清理，本示例用一个可控的假时钟
// (slcore.Clock) 驱动 RollingWriter，并把每个模拟小时文件的 mtime 也设为该模拟时刻，
// 这样 MaxAge=72h 的清理逻辑无需真实等待 3 天即可验证。
//
// 构建：make example-feign   =>   build/bin/feign
// 运行：./build/bin/feign -logdir build/logs/feign -hours 80
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maxhaosl/sllogger"
	"github.com/maxhaosl/sllogger/writer"
)

// fakeClock 是可控时钟，用于在不等待真实时间的情况下驱动按小时切分与清理。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// NewTicker 在示例中仅用于支撑后台清理循环；演示期间 interval 不会真实触发，
// 清理由 Close 时的最终 Clean 统一执行。
func (c *fakeClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

const (
	baseName    = "LOG_FEIGN"
	serviceName = "feignsvc"
	servicePort = 9090
)

func main() {
	logDir := flag.String("logdir", "build/logs/feign", "日志输出目录")
	hours := flag.Int("hours", 80, "模拟写入的小时数（>72 即可演示清理）")
	keep := flag.Duration("keep", 72*time.Hour, "保留时长（MaxAge）")
	flag.Parse()

	if err := run(*logDir, *hours, *keep); err != nil {
		fmt.Fprintf(os.Stderr, "feign 示例失败: %v\n", err)
		os.Exit(1)
	}
}

func run(dir string, hours int, keep time.Duration) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	fc := &fakeClock{t: start}

	cfg := sllogger.Config{
		Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding: "json",
		Rolling: &writer.Config{
			Dir:         dir,
			BaseName:    baseName,
			DateLayout:  "2006-01-02.15", // yyyy-MM-dd.hh：文件名带小时
			ServiceName: serviceName,
			ServicePort: servicePort,
			MaxAge:      keep,  // 保留 keep（默认 72h = 3 天）
			Async:       false, // 同步写，便于演示时精确按小时落盘
			Clock:       fc,    // 注入假时钟
		},
	}
	log, err := cfg.Build()
	if err != nil {
		return err
	}

	fmt.Printf("开始模拟：%d 个模拟小时，保留 %s\n", hours, keep)
	fmt.Printf("文件名规则：%s.{yyyy-MM-dd.hh}.%s%d.log\n\n", baseName, serviceName, servicePort)

	// 同目录放一个其它类型日志，验证清理不会误删它。
	foreign := filepath.Join(dir, "LOG_MGMONITOR.2026-09-10-00.mgmonitor.log")
	if err := os.WriteFile(foreign, []byte("other-type log\n"), 0o644); err != nil {
		return err
	}

	for h := 0; h < hours; h++ {
		sim := start.Add(time.Duration(h) * time.Hour)
		fc.Set(sim)
		// 写一条日志；RollingWriter 按小时段切到新文件。
		log.Info("feign request handled",
			sllogger.Int("hour", h),
			sllogger.String("service", fmt.Sprintf("%s%d", serviceName, servicePort)),
		)
		// 把当前文件的 mtime 设为模拟时刻，使 MaxAge 可立即生效（无需真实等待）。
		name := fmt.Sprintf("%s.%s.%s%d.log", baseName, sim.Format("2006-01-02.15"), serviceName, servicePort)
		_ = os.Chtimes(filepath.Join(dir, name), sim, sim)

		if h < 3 || h == 23 || h == 47 {
			fmt.Printf("  模拟小时 %02d -> %s\n", h, name)
		}
	}

	// 推进时钟到最后一个模拟小时，关闭日志会触发一次最终清理（Close 内调用 Clean）。
	fmt.Printf("\n模拟结束，执行清理（保留 %s）...\n", keep)
	fc.Set(start.Add(time.Duration(hours-1) * time.Hour))
	if err := log.Close(); err != nil {
		return err
	}

	// 统计结果。
	entries, _ := os.ReadDir(dir)
	var feignKept int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		switch {
		case n == "LOG_MGMONITOR.2026-09-10-00.mgmonitor.log":
			fmt.Printf("  其它类型保留：%s\n", n)
		case len(n) >= len(baseName) && n[:len(baseName)] == baseName:
			feignKept++
		}
	}
	// MaxAge=72h 保留最近 73 个小时文件（含当前小时），故删除 hours-73 个最早文件。
	feignRemoved := hours - feignKept
	fmt.Printf("\n结果：LOG_FEIGN 保留 %d 个文件，清理掉最早 %d 个；其它类型不受影响。\n", feignKept, feignRemoved)
	return nil
}
