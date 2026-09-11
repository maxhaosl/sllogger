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

// basic 演示 sllogger 的通用日志能力：JSON 输出、级别、With 上下文、
// 结构化字段、Named 子日志器、Sugar 风格与全局日志器。
//
// 运行：
//
//	go run ./example/basic
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxhaosl/sllogger"
)

func main() {
	// 1. 生产配置：JSON 输出到 stderr，InfoLevel 及以上。
	prod := sllogger.NewProductionConfig()
	log := sllogger.Must(prod.Build())
	defer log.Close()

	log.Info("服务启动",
		sllogger.String("service", "playurl"),
		sllogger.Int("port", 8080),
	)
	log.Warn("配置缺少告警阈值", sllogger.String("key", "alarm.threshold"))
	log.Error("依赖不可用", sllogger.Error(errors.New("connect timeout")))

	// 2. 动态级别：运行期可调整。
	log.Info("级别调整前：这条会输出")
	log.Core() // 触发一次使用，避免被优化
	prod2 := sllogger.NewProductionConfig()
	lvl := sllogger.NewAtomicLevelAt(sllogger.WarnLevel)
	prod2.Level = lvl
	warnOnly := sllogger.Must(prod2.Build())
	warnOnly.Info("这条被级别过滤，不会输出")
	warnOnly.Warn("只有 warn 及以上会输出")
	lvl.SetLevel(sllogger.DebugLevel)
	warnOnly.Debug("降低级别后，这条会输出")
	warnOnly.Close()

	// 3. With 携带固定上下文，派生子日志器。
	apiLog := log.With(sllogger.String("module", "api"), sllogger.String("version", "v1"))
	apiLog.Info("处理请求", sllogger.String("path", "/playurl"), sllogger.Int64("cost_ms", 12))

	// 4. Named 追加日志器名称（点分层级）。
	dbLog := log.Named("dal")
	dbLog.Info("查询数据库", sllogger.Duration("elapsed", 3*time.Millisecond))

	// 5. Sugar：更宽松的 API。
	sugar := log.Sugar()
	sugar.Infof("共处理 %d 条记录", 1024)
	sugar.Infow("带结构化上下文", "userId", 1071748417, "vip", true)
	sugar.Infoln("支持", "Println", "风格")

	// 6. 全局日志器（便于存量代码直接使用）。
	restore := sllogger.ReplaceGlobals(log)
	defer restore()
	sllogger.S().Info("通过全局 Sugar 输出")
	sllogger.L().Info("通过全局 Logger 输出")

	// 7. CallInfo 业务日志：只需提交结构化数据。
	ctx := sllogger.WithTrace(context.Background(), "trace-1", "span-1")
	log.CallInfo(ctx, sllogger.CallInfo{
		Mobile:   "18237438309",
		UserID:   "1071748417",
		URL:      "http://play.example.com/playurl",
		Method:   "GET",
		UseTime:  23,
		ServerIP: "127.0.0.1",
		LogMsg:   "^MG.getContent:[690894368]",
	})
	log.RequestInfo(ctx, sllogger.RequestInfo{
		ReqHeader:   "user-agent=sllogger-demo",
		Method:      "GET",
		RateLimiter: "false",
		LogMsg:      "request success",
	})

	// 8. 把标准库 log 的输出重定向到 sllogger。
	stdLog := sllogger.NewStdLog(log)
	stdLog.Print("来自标准库 log 的输出")

	fmt.Println("---- 以上日志已输出到 stderr ----")
	fmt.Println()

	// 9. 队列满的分级策略（设计文档 #29）：
	//    ERROR 及以上阻塞等待（不丢），INFO/CALL_INFO 丢弃（保护业务）。
	demonstrateLevelPolicy()
}

func demonstrateLevelPolicy() {
	fmt.Println("==== 队列满分级策略（ERROR 不丢，INFO 可丢）====")

	dir, err := os.MkdirTemp("", "sllogger-policy")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = false // 低级别：队列满即丢弃
	cfg.Rolling.BlockLevel = sllogger.ErrorLevel
	cfg.Rolling.QueueSize = 32
	cfg.Rolling.BatchSize = 1

	log := sllogger.Must(cfg.Build())

	ctx := sllogger.WithTrace(context.Background(), "trace-demo", "span-demo")
	// 混合写入：ERROR 保证不丢，INFO 允许丢弃。
	for i := 0; i < 200; i++ {
		log.CallInfo(ctx, sllogger.CallInfo{LogMsg: "E", Level: sllogger.ErrorLevel})
		log.CallInfo(ctx, sllogger.CallInfo{LogMsg: "I", Level: sllogger.InfoLevel})
	}
	log.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	// 统计 ERROR 行数：CALL_INFO 模板的 log_level 字段紧随日期之后，
	// 形如 "2026-09-03 ...|ERROR|..."。用 "|ERROR|" 精确匹配，避免把
	// INFO 行里含 E 的其他字段（如 URL）误计。
	var total int
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		total += strings.Count(string(b), "|ERROR|")
		// 最后一行的 log_msg 是 E，此处按行首级别字段统计已足够。
	}
	fmt.Printf("写入 200 条 ERROR，实际落盘 %d 条（ERROR 不丢）\n", total)
	fmt.Println("INFO 在队列满时会被丢弃并计入 Metrics.Dropped，以保护业务线程")
}
