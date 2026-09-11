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

// Command calllog 演示 sllogger 的三类日志同时落盘。
//
// 三个文件（默认输出到 build/logs，可用 -logdir 覆盖；三者命名规则相同）：
//
//	LOG_CALL_INFO  每个请求一条：入参 + 返回报文 + 总耗时（无论成功/失败）
//	LOG_COMM       业务自身需要记录的普通日志（一个请求可多条）
//	LOG_CALL_ERR   请求出错时的详细日志（header/reqheader/method/rateLimiter）
//
// 文件命名规范（{yyyy-MM-dd-HH}.%i.${AppName}）：
//
//	{base}.{yyyy-MM-dd-HH}.{seq}.${AppName}.log
//	LOG_CALL_INFO.2026-09-11-15.playurl.log       周期内第一个文件（无 %i）
//	LOG_CALL_INFO.2026-09-11-15.01.playurl.log    滚动后（%i 从 01 开始）
//
// 内容模板（"|" 分隔；字段名采用库约定名，见 encoder/fields.go）：
//
//	LOG_CALL_INFO  date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
//	LOG_COMM       date|log_type|service_Id|trace_id|span_id|mobile|userId|clientId|url|serverIp|buss_Id|log_msg
//	LOG_CALL_ERR   date|log_level|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
//
// 构建：make example-calllog   =>   build/bin/calllog
// 运行：./build/bin/calllog -app playurl -n 5 -fail-every 3
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/maxhaosl/sllogger"
	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/writer"
)

// 三个日志文件的 BaseName（也即文件名首段，见命名规范）。
const (
	baseCallInfo = "LOG_CALL_INFO"
	baseComm     = "LOG_COMM"
	baseCallErr  = "LOG_CALL_ERR"
)

// LOG_COMM（业务普通日志）的字段模板：比 CALL_INFO 少了 method/useTime。
//
// 注意：模板用的 log_type / service_Id 是设计规范里的字段名，库里并没有对应的
// 常量（预置模板归一化成 log_level / service_id）。在「模板编码」下，encoder 按
// key 动态解析字段，所以只要用同名 key 注入一个 Field，就能落盘；未注入的字段
// 会回退成 NullValue（默认 null）。下面 FLogType/FServiceID 就是“模拟定义”这两个
// 没有常量的字段。
const commTemplate = "date|log_type|service_Id|trace_id|span_id|mobile|userId|clientId|url|serverIp|buss_Id|log_msg"

// app 持有三类日志各自的 Logger；它们写不同文件，互不影响。
type app struct {
	appName  string
	serverIP string

	callInfo *sllogger.Logger // LOG_CALL_INFO
	comm     *sllogger.Logger // LOG_COMM
	callErr  *sllogger.Logger // LOG_CALL_ERR
}

func main() {
	logDir := flag.String("logdir", "build/logs", "日志输出目录")
	appName := flag.String("app", "playurl", "应用名：文件名 ${AppName} 段与 service_id")
	reqs := flag.Int("n", 5, "模拟请求数")
	failEvery := flag.Int("fail-every", 3, "每 N 个请求模拟一次失败（0 表示全部成功）")
	maxSize := flag.Int64("max-size", 64<<20, "单文件大小上限（字节）；调小可演示 %i 滚动命名")
	flag.Parse()

	a, err := newApp(*logDir, *appName, *maxSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer a.close()

	for i := 1; i <= *reqs; i++ {
		failed := *failEvery > 0 && i%*failEvery == 0
		a.handleRequest(i, failed)
	}

	fmt.Printf("完成：%d 个请求 -> %s\n", *reqs, *logDir)
	fmt.Printf("文件命名：%s.{yyyy-MM-dd-HH}.{seq}.%s.log\n", baseCallInfo, *appName)
}

// newApp 构建三类日志的 Logger。
func newApp(dir, appName string, maxSize int64) (*app, error) {
	a := &app{appName: appName, serverIP: localIP()}

	var err error
	if a.callInfo, err = buildLogger(dir, appName, baseCallInfo, "callinfo", "", maxSize); err != nil {
		return nil, err
	}
	if a.comm, err = buildLogger(dir, appName, baseComm, "template", commTemplate, maxSize); err != nil {
		a.callInfo.Close()
		return nil, err
	}
	if a.callErr, err = buildLogger(dir, appName, baseCallErr, "requestinfo", "", maxSize); err != nil {
		a.callInfo.Close()
		a.comm.Close()
		return nil, err
	}
	return a, nil
}

// buildLogger 按统一命名规范构建单个滚动文件 Logger。
//
// encoding 取值：
//
//	"callinfo"    预置 CALL_INFO 模板
//	"requestinfo" 预置 RequestInfo 模板
//	"template"    使用自定义 tmpl
func buildLogger(dir, appName, base, encoding, tmpl string, maxSize int64) (*sllogger.Logger, error) {
	cfg := sllogger.Config{
		Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding: encoding,
		TemplateConfig: sllogger.TemplateConfig{
			Template:  tmpl, // "callinfo"/"requestinfo" 为空时用预置模板
			ServiceID: appName,
		},
		Rolling: &writer.Config{
			Dir:      dir,
			BaseName: base,

			// 命名规范：LOG_XXX.{yyyy-MM-dd-HH}.%i.${AppName}.log
			// {app} 只渲染应用名（不含端口）；{seq} 即 %i，滚动后才有。
			NamePattern:        "{base}.{date}.{app}.log",
			RotatedNamePattern: "{base}.{date}.{seq}.{app}.log",
			DateLayout:         "2006-01-02-15", // yyyy-MM-dd-HH，按小时切分

			// {service} 仍保留 "<name><port>" 语义；这里用 {app}，故端口置 0。
			ServiceName: appName,
			ServicePort: 0,

			// 滚动与清理策略（可按需调整）。
			MaxSize:          maxSize,            // 单文件大小上限
			RotationInterval: time.Hour,          // 每小时滚动
			MaxBackups:       24 * 7,             // 单类型最多 168 个文件
			MaxTotalSize:     4 << 30,            // 单类型总大小 4GB
			MaxAge:           7 * 24 * time.Hour, // 保留 7 天

			// 异步批量落盘；ERROR 满载时阻塞不丢，INFO 满载时丢弃以保护业务。
			Async:      true,
			BlockLevel: sllogger.ErrorLevel,
		},
	}
	return cfg.Build()
}

// handleRequest 模拟处理一个请求：写 LOG_COMM（若干条）、LOG_CALL_INFO（一条），
// 失败时额外写一条 LOG_CALL_ERR。
func (a *app) handleRequest(idx int, failed bool) {
	// 用 WithTrace 注入 trace_id/span_id，CallInfo/InfoCtx 会自动带上。
	traceID := fmt.Sprintf("3c06e3121c18f6114a2e9f2e38e5b8%04d", idx)
	spanID := fmt.Sprintf("b256f6eac12c9818%04d", idx)
	ctx := sllogger.WithTrace(context.Background(), traceID, spanID)

	const (
		url    = "http://play.miguvideo.com:443/playurl/v1/play/playurl"
		method = "GET"
		mobile = "18237438309"
		userID = "1071748417"
		client = "c6559e74a24df8d97a2296ff1e23387a"
		bussID = "null"
	)

	reqBody := fmt.Sprintf(`{"contentId":%d,"req":%d}`, 690894368, idx)
	start := time.Now()

	// ---- 业务普通日志（LOG_COMM）----
	a.commLog(ctx, mobile, userID, client, url, bussID,
		fmt.Sprintf("begin playurl, contentId=%d", 690894368))

	// 模拟业务处理耗时。
	time.Sleep(time.Duration(3+idx%5) * time.Millisecond)

	if failed {
		const errMsg = "upstream playurl timeout"

		// ---- 出错详细日志（LOG_CALL_ERR）----
		a.callErr.RequestInfo(ctx, sllogger.RequestInfo{
			Header:      "server=nginx; x-request-id=" + traceID,
			ReqHeader:   "user-agent=migu/8.0; accept=application/json",
			Method:      method,
			RateLimiter: "limit=1000;remain=998",
			ContextPath: "/playurl/v1/play/playurl",
			LogMsg:      fmt.Sprintf("^req:%s^resp:err=%s", reqBody, errMsg),
			Level:       sllogger.ErrorLevel,
		})

		// ---- 业务普通错误日志（LOG_COMM）----
		a.commLog(ctx, mobile, userID, client, url, bussID, "playurl failed: "+errMsg)

		// ---- 请求级日志（LOG_CALL_INFO）：一次请求仍只有一条 ----
		a.callInfo.CallInfo(ctx, sllogger.CallInfo{
			Mobile: mobile, UserID: userID, ClientID: client,
			URL: url, Method: method, UseTime: time.Since(start).Milliseconds(),
			ServerIP: a.serverIP, BussID: bussID,
			LogMsg: fmt.Sprintf("^req:%s^resp:err=%s", reqBody, errMsg),
			Level:  sllogger.ErrorLevel,
		})
		return
	}

	respBody := fmt.Sprintf(`{"code":0,"content":"^MG.getContent:[%d]"}`, 690894368+idx)

	// ---- 业务普通日志（LOG_COMM）----
	a.commLog(ctx, mobile, userID, client, url, bussID,
		fmt.Sprintf("^MG.getContent:[%d]", 690894368+idx))

	// ---- 请求级日志（LOG_CALL_INFO）：入参 + 返回报文 + 总耗时 ----
	a.callInfo.CallInfo(ctx, sllogger.CallInfo{
		Mobile: mobile, UserID: userID, ClientID: client,
		URL: url, Method: method, UseTime: time.Since(start).Milliseconds(),
		ServerIP: a.serverIP, BussID: bussID,
		LogMsg: fmt.Sprintf("^req:%s^resp:%s", reqBody, respBody),
		Level:  sllogger.InfoLevel,
	})
}

// commLog 追加一条 LOG_COMM 业务日志，补齐模板所需字段。
func (a *app) commLog(ctx context.Context, mobile, userID, client, url, bussID, msg string) {
	a.comm.InfoCtx(ctx, msg,
		// 库里没有常量的字段：模拟定义（按模板里的 key 名注入即可）。
		FLogType("INFO"),
		FServiceID(a.appName),
		sllogger.String(encoder.FieldMobile, mobile),
		sllogger.String(encoder.FieldUserID, userID),
		sllogger.String(encoder.FieldClientID, client),
		sllogger.String(encoder.FieldURL, url),
		sllogger.String(encoder.FieldServerIP, a.serverIP),
		sllogger.String(encoder.FieldBussID, bussID),
	)
}

// FLogType / FServiceID 模拟定义库里没有常量的字段名（设计规范用 log_type /
// service_Id）。在「模板编码」下，encoder 按 key 动态解析，因此用同名 key 的
// Field 注入即可落盘；若不注入，该字段会回退成 null（见 NullValue）。
func FLogType(level string) sllogger.Field {
	return sllogger.String("log_type", level)
}

func FServiceID(id string) sllogger.Field {
	return sllogger.String("service_Id", id)
}

// close 先排空异步队列（Sync）再关闭各自文件。
func (a *app) close() {
	_ = a.callInfo.Close()
	_ = a.comm.Close()
	_ = a.callErr.Close()
}

// localIP 返回本机首个非回环 IPv4，取不到时回退到示例地址。
func localIP() string {
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "10.172.60.157"
}
