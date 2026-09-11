# sllogger

sllogger 是一个 **Go 结构化、高吞吐调用日志库**，面向微服务的 HTTP/RPC 调用日志、接口访问日志与业务调用日志。日志以 `|` 分隔的字段模板落盘，开箱即支持 CALL_INFO / RequestInfo 两类调用日志，并自带自研滚动文件写入器（RollingWriter）与异步批量落盘。

> Module 路径：`github.com/maxhaosl/sllogger`（Go 包名仍为 `sllogger`）。

## 特性

- **模板编码（content-template encoder）**：以 `|` 分隔的字段模板落盘；内置 CALL_INFO / RequestInfo 预置模板，也支持完全自定义模板。
- **字段按 key 动态解析**：字段从 `With` / 调用点 `Field` 中查找；未注入回退 `null`（可配 `NullValue`）；支持 `RegisterRenderer` 注册自定义渲染器。
- **三类调用日志 API**：`CallInfo`（入参 + 返回报文 + 总耗时，每请求一条）、`RequestInfo`（出错详细日志）、通用 `InfoCtx` / `WarnCtx` / `ErrorCtx`。
- **trace / span 自动注入**：`WithTrace(ctx, traceID, spanID)`，`CallInfo` / `InfoCtx` 自动带上 `trace_id` / `span_id`（兼容 OpenTelemetry `SpanContext` 抽取）。
- **自研 RollingWriter**：按大小 / 小时 / 天滚动；命名占位符 `{base} {date} {service} {app} {seq} {pid}`；自动续写与清理。
- **异步批量落盘**：多生产者单消费者；`BlockLevel` 策略让 ERROR 阻塞不丢、INFO 丢弃以保护业务。
- **字段自动转义**（`| \n \r \`），保证下游按 `|` 切分可靠。

## 安装

```bash
go get github.com/maxhaosl/sllogger@v1.0.0
```

```go
import (
    "github.com/maxhaosl/sllogger"
    "github.com/maxhaosl/sllogger/encoder"
    "github.com/maxhaosl/sllogger/writer"
)
```

## 快速开始

### 1) 最简：单条 CALL_INFO 日志（滚动文件）

```go
package main

import (
    "context"
    "github.com/maxhaosl/sllogger"
)

func main() {
    // NewCallInfoConfig 已配好滚动文件 + callinfo 模板，
    // 文件名形如 /data/logs/LOG_CALL_INFO.2026-09-12.playurl8080.log
    cfg := sllogger.NewCallInfoConfig("/data/logs", "playurl", 8080)
    log, err := cfg.Build()
    if err != nil {
        panic(err)
    }
    defer log.Close() // 排空异步队列并关闭文件

    ctx := sllogger.WithTrace(context.Background(), "trace-001", "span-001")
    log.CallInfo(ctx, sllogger.CallInfo{
        Mobile:   "18237438309",
        UserID:   "1071748417",
        ClientID: "c6559e74a24df8d97a2296ff1e23387a",
        URL:      "http://play.miguvideo.com:443/playurl/v1/play/playurl",
        Method:   "GET",
        UseTime:  12, // 毫秒
        ServerIP: "10.172.60.157",
        BussID:   "null",
        LogMsg:   "^req:{...}^resp:{...}",
        Level:    sllogger.InfoLevel,
    })
}
```

落盘内容（模板 `date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg`）：

```text
2026-09-12 10:00:00.123|INFO|playurl8080|trace-001|span-001|18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|http://...|GET|12|10.172.60.157|null|^req:{...}^resp:{...}
```

> 不想处理错误时可用 `sllogger.Must(cfg.Build())`。

### 2) 三类日志同时落盘（LOG_CALL_INFO / LOG_COMM / LOG_CALL_ERR）

完整可运行示例见 [`example/calllog`](example/calllog)。要点：构建三个独立的 `*sllogger.Logger`，各自写不同文件。

```go
func newLogger(dir, app string) (*sllogger.Logger, error) {
    cfg := sllogger.Config{
        Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
        Encoding: "template",
        TemplateConfig: sllogger.TemplateConfig{
            Template:  "date|log_type|service_Id|trace_id|span_id|mobile|userId|clientId|url|serverIp|buss_Id|log_msg",
            ServiceID: app,
        },
        Rolling: &writer.Config{
            Dir:                dir,
            BaseName:           "LOG_COMM",
            NamePattern:        "{base}.{date}.{app}.log",
            RotatedNamePattern: "{base}.{date}.{seq}.{app}.log",
            DateLayout:         "2006-01-02-15", // 按小时切分
            ServiceName:        app,
            ServicePort:        0,
            MaxSize:            64 << 20,
            RotationInterval:   time.Hour,
            MaxAge:             7 * 24 * time.Hour,
            Async:              true,
            BlockLevel:         sllogger.ErrorLevel, // ERROR 阻塞不丢，INFO 丢弃保护业务
        },
    }
    return cfg.Build()
}
```

业务普通日志（LOG_COMM）——注入库里没有常量的字段 `log_type` / `service_Id`：

```go
log.InfoCtx(ctx, "begin playurl",
    sllogger.String("log_type", "INFO"), // 设计规范原名，库无常量，按 key 注入即可
    sllogger.String("service_Id", app),
    sllogger.String(encoder.FieldMobile, mobile),
    sllogger.String(encoder.FieldUserID, userID),
    sllogger.String(encoder.FieldClientID, client),
    sllogger.String(encoder.FieldURL, url),
    sllogger.String(encoder.FieldServerIP, serverIP),
    sllogger.String(encoder.FieldBussID, bussID),
)
```

## 字段模板与自定义字段

模板是 `|` 分隔的字段名序列，渲染时每个字段按名称解析：

- `date` / `log_level` 来自日志事件本身；
- 其余字段（`mobile`、`userId`、`trace_id` …）按 key 从 `With` / 调用点 `Field` 中查找；
- 未找到的字段输出 `null`（由 `TemplateConfig.NullValue` 控制，默认 `null`）。

预置模板常量：`encoder.CallInfoTemplate`、`encoder.RequestInfoTemplate`。`Config.Encoding` 取值：

| Encoding       | 说明                              |
| -------------- | --------------------------------- |
| `""` / `"json"` | 单行 JSON                          |
| `"template"`    | 自定义模板（`TemplateConfig.Template`） |
| `"callinfo"`    | 预置 CALL_INFO 模板               |
| `"requestinfo"` | 预置 RequestInfo 模板             |

**自定义字段**：模板里可以写任意 key（如设计规范原名 `log_type` / `service_Id`），只要用同名 key 注入 `Field` 即可落盘：

```go
sllogger.String("log_type", "INFO") // 等价于库内置字段的用法
```

字段名常量见 `encoder/fields.go`（`FieldMobile` `FieldUserID` `FieldClientID` `FieldURL` `FieldServerIP` `FieldBussID` `FieldTraceID` `FieldSpanID` 等）；也可直接用字符串 key（如 `"mobile"`）。

## 文件命名规范

滚动文件名由 `writer.Config` 的 `NamePattern` / `RotatedNamePattern` / `DateLayout` 决定。

占位符：`{base}` `{date}` `{service}` `{app}` `{seq}` `{pid}`

- `{service}` = `<ServiceName><ServicePort>`（如 `playurl8080`）
- `{app}` = 仅 `<ServiceName>`（如 `playurl`）
- `{seq}` = 滚动序号，至少两位（`01`、`02` …）

默认（随 `NewCallInfoConfig`）：

```text
LOG_CALL_INFO.2026-09-12.playurl8080.log      # 首文件
LOG_CALL_INFO.2026-09-12.playurl8080.01.log   # 滚动后
```

示例（按小时、去掉端口，使用 `{app}`）：

```text
LOG_COMM.2026-09-12-15.playurl.log
LOG_COMM.2026-09-12-15.01.playurl.log
```

## 配置参考（writer.Config 关键字段）

| 字段            | 说明                                                         |
| --------------- | ------------------------------------------------------------ |
| `Dir`           | 日志目录，如 `/data/logs`                                    |
| `BaseName`      | 文件名首段，如 `LOG_CALL_INFO`                               |
| `NamePattern` / `RotatedNamePattern` | 文件名模板（首文件 / 滚动后）                |
| `DateLayout`    | `{date}` 时间布局，亦控制按日期滚动粒度；默认 `2006-01-02`（按天） |
| `ServiceName` / `ServicePort` | 渲染进 `{service}`                              |
| `MaxSize`       | 单文件大小上限（字节），0 关闭                               |
| `RotationInterval` | 时间间隔滚动，如 `time.Hour`                              |
| `MaxBackups` / `MaxTotalSize` / `MaxAge` | 清理策略（保留数量 / 总大小 / 天数） |
| `Async`         | 开启异步批量落盘                                             |
| `QueueSize` / `BatchSize` / `FlushInterval` | 队列容量 / 每批条数 / 最大刷新间隔   |
| `BlockLevel`    | 队列满策略：`>=BlockLevel` 阻塞不丢，`<BlockLevel` 丢弃（建议 `ErrorLevel`） |
| `Shards`        | 异步队列分片数（>1 提升并发吞吐，可能乱序）                   |

## 完整示例

[`example/calllog`](example/calllog)：三类日志同时落盘，含滚动命名、自定义字段演示：

```bash
go run ./example/calllog -app playurl -n 5 -fail-every 3
# 或 make example-calllog 生成 build/bin/calllog
```

## 文档

- 设计文档：[`doc/go_call_info_logger_design.md`](doc/go_call_info_logger_design.md)
- 变更记录：[`CHANGELOG.md`](CHANGELOG.md)

## License

各源文件头部包含 MIT 风格许可声明；如需独立 `LICENSE` 文件可后续补充。
