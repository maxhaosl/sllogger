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

// Command multitype 演示 sllogger 的三类能力（对应需求 1/2/3）：
//
//  1. RollingWriter 相关配置（路径 / 命名占位符 / 大小·小时·天滚动 / 续写 / 清理）
//     通过 sllogger.Config.Rolling（或每个 Output 的 Rolling）直接暴露给用户设置。
//  2. 按日志级别分流到不同文件，并能选择性关闭某些级别：
//     - LOG_FEIGN.*.log        普通请求日志（INFO/DEBUG/TRACE/WARN/ERROR）
//     - LOG_FEIGN_ERR.*.log    ERROR/WARN 错误日志（级别白名单）
//     - -mode erroronly        上线只输出 ERROR/WARN：不配置普通 FEIGN 输出即可。
//  3. 每个日志文件支持不同格式：
//     - LOG_FEIGN   用 JSON 编码
//     - LOG_MGMONITOR 用 "^" 分隔的自定义埋点模板
//
// 为了能在几秒内演示跨多小时 / 跨多天，本示例用一个可控假时钟（slcore.Clock）驱动，
// 并把每个模拟小时文件的 mtime 设为该模拟时刻，使 MaxAge 清理立即生效。
//
// 构建：make example-multitype   =>   build/bin/multitype
// 运行：./build/bin/multitype -logdir build/logs/multitype -hours 26
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maxhaosl/sllogger"
	"github.com/maxhaosl/sllogger/encoder"
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
func (c *fakeClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }
func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func main() {
	logDir := flag.String("logdir", "build/logs/multitype", "日志输出目录")
	hours := flag.Int("hours", 26, "模拟写入的小时数")
	keep := flag.Duration("keep", 72*time.Hour, "保留时长（MaxAge）")
	mode := flag.String("mode", "full", "full=输出全部级别；erroronly=只输出 ERROR/WARN")
	flag.Parse()

	if err := run(*logDir, *hours, *keep, *mode); err != nil {
		fmt.Fprintf(os.Stderr, "multitype 示例失败: %v\n", err)
		os.Exit(1)
	}
}

func run(dir string, hours int, keep time.Duration, mode string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	fc := &fakeClock{t: start}

	// FEIGN logger：一个 logger 内用多个 Output 做“按级别分流”。
	feignOutputs := []sllogger.Output{
		{
			Name:     "feign",
			Encoding: "json",
			Rolling: &writer.Config{
				Dir: dir, BaseName: "LOG_FEIGN", DateLayout: "2006-01-02.15",
				ServiceName: "feignsvc", ServicePort: 9090, MaxAge: keep, Clock: fc,
			},
			// 普通请求日志：全部级别（上线不想要 INFO/DEBUG/TRACE 时，去掉这个 Output 即可）。
			Levels: []string{"info", "debug", "trace", "warn", "error"},
		},
		{
			Name:     "feign_err",
			Encoding: "json",
			Rolling: &writer.Config{
				Dir: dir, BaseName: "LOG_FEIGN_ERR", DateLayout: "2006-01-02.15",
				ServiceName: "feignsvc", ServicePort: 9090, MaxAge: keep, Clock: fc,
			},
			// 错误日志：仅 ERROR/WARN。
			Levels: []string{"error", "warn"},
		},
	}
	if mode == "erroronly" {
		// 上线只输出 ERROR/WARN：不配置普通 FEIGN 输出。
		feignOutputs = feignOutputs[1:]
	}

	feignLog, err := sllogger.Config{
		Level:   sllogger.NewAtomicLevelAt(sllogger.DebugLevel),
		Outputs: feignOutputs,
		Clock:   fc,
	}.Build()
	if err != nil {
		return err
	}

	// MGMONITOR logger：独立的日志类型，使用 "^" 分隔的自定义埋点模板。
	mgLog, err := sllogger.Config{
		Level: sllogger.NewAtomicLevelAt(sllogger.DebugLevel),
		Outputs: []sllogger.Output{
			{
				Name:     "mgmonitor",
				Encoding: "template",
				Rolling: &writer.Config{
					Dir: dir, BaseName: "LOG_MGMONITOR", DateLayout: "2006-01-02-15",
					ServiceName: "mgmonitor", ServicePort: 0, MaxAge: keep, Clock: fc,
					// ${AppName} 对应 {app}（不含端口）。
					NamePattern: "{base}.{date}.{app}.log",
				},
				TemplateConfig: encoder.TemplateConfig{
					Separator: "^",
					Template:  "log_level^date^dataType^operatorId^serviceId^useTime^result",
				},
			},
		},
		Clock: fc,
	}.Build()
	if err != nil {
		return err
	}

	fmt.Printf("模式=%s，模拟 %d 小时，保留 %s\n", mode, hours, keep)
	fmt.Printf("  LOG_FEIGN(.json)        普通请求日志\n")
	fmt.Printf("  LOG_FEIGN_ERR(.json)    ERROR/WARN 错误日志\n")
	fmt.Printf("  LOG_MGMONITOR(^模板)    埋点数据，自定义格式\n\n")

	for h := 0; h < hours; h++ {
		sim := start.Add(time.Duration(h) * time.Hour)
		fc.Set(sim)

		// FEIGN 请求日志（普通 + 偶发错误）。
		feignLog.Info("feign request handled",
			sllogger.String("uri", "/playurl/v1/play/playurl"),
			sllogger.Int("useTime", 5+h%7),
		)
		if h%9 == 4 {
			feignLog.Warn("feign slow response", sllogger.Int("useTime", 800))
		}
		if h%13 == 7 {
			feignLog.Error("feign downstream error", sllogger.String("upstream", "mgmonitor"))
		}

		// MGMONITOR 埋点日志（不同格式）。
		mgLog.Info("",
			sllogger.String("dataType", "recall"),
			sllogger.String("operatorId", fmt.Sprintf("op-%d", h%5)),
			sllogger.String("serviceId", "playurl"),
			sllogger.Int("useTime", 12+h%9),
			sllogger.String("result", "OK"),
		)

		// 把当前文件 mtime 设为模拟时刻，使 MaxAge 即时生效（无需真实等待）。
		fname := fmt.Sprintf("LOG_FEIGN.%s.feignsvc9090.log", sim.Format("2006-01-02.15"))
		_ = os.Chtimes(filepath.Join(dir, fname), sim, sim)
		ferr := fmt.Sprintf("LOG_FEIGN_ERR.%s.feignsvc9090.log", sim.Format("2006-01-02.15"))
		_ = os.Chtimes(filepath.Join(dir, ferr), sim, sim)
		mg := fmt.Sprintf("LOG_MGMONITOR.%s.mgmonitor.log", sim.Format("2006-01-02-15"))
		_ = os.Chtimes(filepath.Join(dir, mg), sim, sim)

		if h < 3 || h == 12 {
			fmt.Printf("  模拟小时 %02d: FEIGN+FEIGN_ERR(json) / MGMONITOR(^)\n", h)
		}
	}

	// 推进时钟到最后一个模拟小时，关闭日志触发最终清理。
	fc.Set(start.Add(time.Duration(hours-1) * time.Hour))
	if err := feignLog.Close(); err != nil {
		return err
	}
	if err := mgLog.Close(); err != nil {
		return err
	}

	// 统计结果：按 BaseName 前缀汇总（清理是每类型独立的）。
	entries, _ := os.ReadDir(dir)
	count := map[string]int{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch {
		case len(e.Name()) >= len("LOG_FEIGN_ERR") && e.Name()[:len("LOG_FEIGN_ERR")] == "LOG_FEIGN_ERR":
			count["LOG_FEIGN_ERR"]++
		case len(e.Name()) >= len("LOG_FEIGN") && e.Name()[:len("LOG_FEIGN")] == "LOG_FEIGN":
			count["LOG_FEIGN"]++
		case len(e.Name()) >= len("LOG_MGMONITOR") && e.Name()[:len("LOG_MGMONITOR")] == "LOG_MGMONITOR":
			count["LOG_MGMONITOR"]++
		}
	}
	fmt.Printf("\n结果：LOG_FEIGN=%d  LOG_FEIGN_ERR=%d  LOG_MGMONITOR=%d（各类按 MaxAge 独立清理）\n",
		count["LOG_FEIGN"], count["LOG_FEIGN_ERR"], count["LOG_MGMONITOR"])
	return nil
}
