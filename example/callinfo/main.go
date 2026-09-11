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

// callinfo 示例：演示 CALL_INFO 调用日志、异步滚动文件输出与优雅关闭。
//
// 运行：
//
//	go run ./example/callinfo
//
// 输出（默认写入 /tmp/sllogger-example）：
//
//	/tmp/sllogger-example/LOG_CALL_INFO.2026-09-03.playurl8080.log
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/maxhaosl/sllogger"
	"github.com/maxhaosl/sllogger/writer"
)

func main() {
	dir := flag.String("dir", "/tmp/sllogger-example", "log directory")
	flag.Parse()

	// 1. 构造 CALL_INFO 日志器（需求 1/2/4/5 及清理策略 6-9 一并配置）。
	cfg := sllogger.NewCallInfoConfig(*dir, "playurl", 8080)
	cfg.Rolling.MaxSize = writer.DefaultMaxSize // 需求4：单文件 1GB 上限
	cfg.Rolling.RotationInterval = time.Hour    // 需求5：1 小时滚动一次
	cfg.Rolling.MaxBackups = 48                 // 需求6：最多保留 48 个文件
	cfg.Rolling.MaxTotalSize = 10 << 30         // 需求7：单类型总量 10GB
	cfg.Rolling.MaxDirSize = 20 << 30           // 需求8：目录总量 20GB
	cfg.Rolling.MaxAge = 3 * 24 * time.Hour     // 需求9：保留 3 天
	cfg.Rolling.Async = true                    // 异步批量写入
	cfg.Rolling.BatchSize = 100
	cfg.Rolling.FlushInterval = 100 * time.Millisecond

	log, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	defer log.Close()

	// 2. 通过 context 携带 trace 信息（也可注册 otel TraceExtractor）。
	ctx := sllogger.WithTrace(context.Background(),
		"3c06e3121c18f6114a2e9f2e38e5b8fe",
		"b256f6eac12c9818")

	// 3. 记录 CALL_INFO 日志：业务只提交结构化数据，不拼接 "|" 字符串。
	start := time.Now()
	log.CallInfo(ctx, sllogger.CallInfo{
		Mobile:   "18237438309",
		UserID:   "1071748417",
		ClientID: "c6559e74a24df8d97a2296ff1e23387a",
		URL:      "http://play.example.com:443/playurl/v1/play/playurl",
		Method:   "GET",
		UseTime:  time.Since(start).Milliseconds(),
		ServerIP: "127.0.0.1",
		BussID:   "null",
		LogMsg:   "^MG.getContent:[690894368]",
	})

	// 4. 记录 RequestInfo 日志（另一种预置模板）。
	log.RequestInfo(ctx, sllogger.RequestInfo{
		Header:      "content-type=application/json",
		ReqHeader:   "user-agent=demo",
		Method:      "GET",
		RateLimiter: "false",
		LogMsg:      "request success",
	})

	// 5. 自定义内容模板（需求3）：任意字段、任意顺序。
	tplLog, err := sllogger.Config{
		Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding: "template",
		TemplateConfig: sllogger.TemplateConfig{
			Template:  "date|log_level|service_id|trace_id|url|log_msg",
			ServiceID: "playurl",
		},
		Rolling: &writer.Config{
			Dir:         *dir,
			BaseName:    "CUSTOM_TPL",
			ServiceName: "playurl",
			ServicePort: 8080,
		},
	}.Build()
	if err != nil {
		panic(err)
	}
	defer tplLog.Close()
	tplLog.InfoCtx(ctx, "custom template message", sllogger.String("url", "http://svc/api"))

	fmt.Println("done, see files under", *dir)
}
