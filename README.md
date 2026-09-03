# sllogger

sllogger 是一个 Go 分层与高性能日志库，参考了日志库的zap实现，并实现了日志管理功能
面向微服务调用日志（CALL_INFO / RequestInfo）场景补齐了：

- 自研 `RollingWriter`：按**日期 / 定时间隔 / 单文件大小**三种条件滚动；
- 完整的日志文件**生命周期管理**：按文件个数、单类型总大小、目录总大小、保留时长自动清理；
- **自定义内容模板**编码器：`|` 分隔的 CALL_INFO / RequestInfo 预置格式与任意自定义模板；
- **异步批量写入**：多生产者 + 单消费者 + 队列满丢弃保护，业务线程不做文件 IO。

核心引擎层（`buffer`、`internal/pool`、`slcore`）与根包 API（`Logger`/`SugaredLogger`/
`Config`/`Field`/`global`）拷贝自 zap 并按本库命名重构，遵循原 MIT 许可证。

## 目录结构

```text
sllogger/
├── buffer/                 # [zap 重构] 零分配字节缓冲 + 对象池
├── internal/
│   ├── bufferpool/         # [zap 重构] 共享缓冲池
│   ├── exit/               # os.Exit 可测试封装
│   └── pool/               # [zap 重构] 泛型对象池
├── slcore/                 # [zapcore 重构] 引擎层
│   ├── level.go            #   日志级别与 LevelEnabler
│   ├── entry.go            #   Entry/CheckedEntry（池化）
│   ├── field.go            #   Field 联合类型
│   ├── encoder.go          #   Encoder/ObjectEncoder 接口与预置编码器
│   ├── core.go             #   Core 接口 + ioCore
│   ├── write_syncer.go     #   WriteSyncer/Lock/Multi
│   ├── clock.go            #   Clock 抽象（滚动测试依赖）
│   ├── hooks.go            #   RegisterHooks
│   ├── error.go            #   error 字段编码 + errors.Join 聚合
│   └── stacktrace.go       #   调用方/栈捕获
├── encoder/                # 内容模板编码器（需求3）
│   ├── fields.go           #   字段渲染器注册表、转义、null 处理
│   ├── template.go         #   TemplateEncoder（实现 slcore.Encoder）
│   ├── presets.go          #   CALL_INFO / RequestInfo 预置模板
│   └── json.go             #   轻量 JSON 行编码器
├── writer/                 # 自研文件层（需求1/2/4-9）
│   ├── config.go           #   RollingConfig 与默认值
│   ├── naming.go           #   文件命名策略与解析
│   ├── rolling_writer.go   #   RollingWriter（日期/定时/大小滚动）
│   ├── cleanup.go          #   历史文件清理（个数/类型总量/目录总量/保留时长）
│   ├── async.go            #   异步批量写入 + 优雅关闭
│   └── metrics.go          #   写入指标
├── logger.go               # [zap 重构] Logger（含 Close）
├── sugar.go                # [zap 重构] SugaredLogger
├── options.go              # [zap 重构] Option
├── config.go               # [zap 重构] Config.Build 总装配
├── level.go                # [zap 重构] AtomicLevel
├── field.go / error.go     # [zap 重构] Field 构造器
├── global.go               # [zap 重构] L/S/ReplaceGlobals
├── writer.go               # [zap 重构] Open/CombineWriteSyncers
├── context.go              # ctx 提取 trace_id/span_id（可插拔 otel）
├── callinfo.go             # CallInfo/RequestInfo 业务 API
├── doc/                    # 设计文档
└── example/callinfo/       # 完整示例
```

## 构建与验证

项目提供 `Makefile` 作为统一入口：

```bash
make            # 格式化检查 + 构建 + vet + 单元测试
make test       # 单元测试                make race    # 竞态检测
make cover      # 覆盖率报告              make bench   # 全模块基准
make stress     # 压力测试                make verify  # 功能自检
make check      # 一致性校验（写入条数 vs 落盘条数）
make ci         # CI 全量流水线（复现 GitHub Actions）
make clean      # 清理产物
```

参数可通过环境变量覆盖：

```bash
make check N=1000000 G=64          # 100 万条 / 64 协程一致性校验
make throughput N=200000 G=16 ROUNDS=5
```

CI（`.github/workflows/ci.yml`）在 Go 1.21 / 1.22 / stable 上执行
构建、单元测试、竞态检测与覆盖率，并运行端到端校验。

## 快速开始

```go
cfg := sllogger.NewCallInfoConfig("/data/logs", "playurl", 8080)
cfg.Rolling.MaxSize = 1 << 30                // 需求4：单文件 1GB
cfg.Rolling.RotationInterval = time.Hour     // 需求5：1 小时滚动
cfg.Rolling.MaxBackups = 48                  // 需求6：文件个数上限
cfg.Rolling.MaxTotalSize = 10 << 30          // 需求7：单类型总大小
cfg.Rolling.MaxDirSize = 20 << 30            // 需求8：目录总大小
cfg.Rolling.MaxAge = 72 * time.Hour          // 需求9：保留 3 天
cfg.Rolling.Async = true                     // 异步批量写入

log, err := cfg.Build()
if err != nil { panic(err) }
defer log.Close()                            // 优雅关闭：drain + flush + sync

ctx := sllogger.WithTrace(ctx, traceID, spanID)
log.CallInfo(ctx, sllogger.CallInfo{
    Mobile: "18237438309", UserID: "1071748417",
    ClientID: "c6559e74a24df8d97a2296ff1e23387a",
    URL: "http://play.example.com:443/playurl/v1/play/playurl",
    Method: "GET", UseTime: 123,
    ServerIP: "127.0.0.1", BussID: "null",
    LogMsg: "^MG.getContent:[690894368]",
})
```

输出：

```text
2021-11-14 20:37:34.376|INFO|playurl|3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|http://play.example.com:443/playurl/v1/play/playurl|GET|123|127.0.0.1|null|^MG.getContent:[690894368]
```

## 文件命名规则

```text
/data/logs/LOG_CALL_INFO.%d{yyyy-MM-dd}.{serviceName}{servicePort}.log
```

同一天内滚动时通过序号区分（第一个文件无序号，序号至少两位）：

```text
/data/logs/
    LOG_CALL_INFO.2026-09-03.playurl8080.log
    LOG_CALL_INFO.2026-09-03.playurl8080.01.log
    LOG_CALL_INFO.2026-09-03.playurl8080.02.log
    ...
```

文件名可通过 `NamePattern` / `RotatedNamePattern` / `DateLayout` 完全自定义，
支持的占位符：`{base}`、`{date}`、`{service}`、`{seq}`、`{pid}`。

## 滚动条件

每次写入前依次检查（任一满足即滚动）：

| 条件 | 配置 | 说明 |
|---|---|---|
| 日期变化 | `DateLayout`（默认 `2006-01-02`） | 文件名含日期，跨天必须滚动且序号重置 |
| 定时间隔 | `RotationInterval`（默认 1h） | 每次写入检查当前时间，不依赖定时器 |
| 单文件大小 | `MaxSize`（默认 1GB） | `当前大小 + 本次写入 > MaxSize` 即滚动 |

进程重启时自动扫描日志目录：当日最新文件未满则**续写**，否则从下一序号开新文件。

## 清理策略（生命周期管理）

| 需求 | 配置 | 语义 |
|---|---|---|
| 6. 文件个数上限 | `MaxBackups` | 本类型文件数超限，删最老 |
| 7. 单类型总大小 | `MaxTotalSize` | 本类型文件总大小超限，删最老 |
| 8. 目录总大小 | `MaxDirSize` | 目录下全部 `*.log` 总大小超限，从本类型最老开始删（不碰其他类型） |
| 9. 保留时长 | `MaxAge` | 修改时间早于 `now-MaxAge` 的文件删除 |

清理触发时机：定时（`CleanupInterval`，默认 10 分钟）+ `Close` 时兜底一次。

## 自定义内容模板

```go
cfg.Encoding = "template"
cfg.TemplateConfig.Template = "date|log_level|service_id|trace_id|url|log_msg"
cfg.TemplateConfig.ServiceID = "playurl"
cfg.TemplateConfig.TimeLayout = "2006-01-02 15:04:05.000"
```

- 字段解析顺序：自定义渲染器 → 内置渲染器（date/log_level/service_id/log_msg 等）→
  结构化字段（`With` 注入与调用点字段，key 与模板字段名一致）→ 未命中输出 `null`；
- 字段内容自动转义：`|`→`\|`、`\n`→`\n`、`\r`→`\r`、`\`→`\\`（`NoEscape` 可关闭）；
- 可用 `encoder.RegisterRenderer(&cfg.TemplateConfig, "log_level", fn)` 覆盖任意字段渲染，
  例如输出老格式 ` INFO [playurl,tid,sid,true]`。

预置模板：

```text
CALL_INFO:    date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
RequestInfo:  date|log_level|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
```

## 接口规范

```go
// 核心引擎接口（slcore）
type Encoder interface { ObjectEncoder; Clone() Encoder; EncodeEntry(Entry, []Field) (*buffer.Buffer, error) }
type Core interface { LevelEnabler; With([]Field) Core; Check(Entry, *CheckedEntry) *CheckedEntry; Write(Entry, []Field) error; Sync() error }
type WriteSyncer interface { io.Writer; Sync() error }

// 文件层接口（writer）
type RollingWriter struct{...}  // Write/Rotate/Sync/Close，实现 slcore.WriteSyncer
type AsyncWriter struct{...}    // Write/Sync/Close + Metrics()

// 业务 API（根包）
type CallInfoLogger interface {
    CallInfo(ctx, CallInfo); RequestInfo(ctx, RequestInfo)
    InfoCtx/WarnCtx/ErrorCtx(ctx, msg, ...Field)
    Sync() error; Close() error
}
```

`*Logger` 与 `*SugaredLogger` 均实现 `CallInfoLogger`。trace_id/span_id 优先通过
`sllogger.SetTraceExtractor(...)` 接入 OpenTelemetry，否则从 `WithTrace` 注入的
context 读取；缺失时输出 `null`。

## 并发与故障语义

### 队列满的分级策略（设计文档 #29）

日志级别不同，队列满时的处理也不同：

| 级别 | 行为 | 理由 |
|---|---|---|
| **ERROR 及以上**（`>= BlockLevel`） | **阻塞等待**直到入队 | 错误日志不能丢，是排查问题的关键证据 |
| **INFO / CALL_INFO**（`< BlockLevel`） | **丢弃并计数** | 保护业务线程，避免被日志 IO 拖垮 |

```go
cfg.Rolling.Async = true
cfg.Rolling.BlockOnFull = false       // 低级别：丢弃
cfg.Rolling.BlockLevel = ErrorLevel   // ERROR 及以上：阻塞（推荐）
```

`BlockLevel` 默认为 0（不启用分级），此时仅由 `BlockOnFull` 决定。
实现上通过 `slcore.LevelWriteSyncer` 接口把级别传给 sink；未实现该接口的
sink 会安全回退到普通 `Write`（不启用分级）。

### 其他语义

- 文件写入异常不会 panic，错误经 `ErrorOutput` 输出并计入 `Metrics.WriteErrors`；
- `Close()` 幂等：停止接收 → 等待在途写入者 → drain 队列 → 批量 flush →
  `File.Sync` → 关闭文件 → 最后一次清理。

### 监控指标（设计文档 #30）

```go
snap := aw.Snapshot()   // AsyncWriter 的快照含队列指标
snap.Enqueued / Written / Dropped / WriteErrors / RotateCount / RemovedFiles
snap.QueueSize / QueueDepth          // 队列容量与当前积压深度
aw.QueueDepth()                      // 也可单独获取即时深度
```

建议对 `Dropped > 0`、`WriteErrors > 0` 以及 `QueueDepth` 长期接近
`QueueSize` 建立告警。

## 示例

| 示例 | 说明 |
|---|---|
| `example/verify` | **功能自检程序**：逐条验证 9 项需求与转义、异步、续写等附加能力，全部做真实断言，失败以非 0 退出码返回 |
| `example/bench` | **写入压测工具**：一致性校验（写入条数 == 落盘条数）+ 吞吐矩阵（多轮取中位数）+ profile 采集 |
| `example/callinfo` | CALL_INFO 调用日志的完整用法，含 9 项配置与自定义模板 |
| `example/basic` | 通用日志能力：JSON 输出、动态级别、With/Named、Sugar、全局日志器、标准库 log 重定向 |

```bash
go run ./example/verify                              # 功能自检，退出码 0 表示全通过
go run ./example/bench -mode check -n 1000000 -g 32  # 一致性校验
go run ./example/bench -mode bench -n 200000 -g 16   # 吞吐矩阵
```

## 测试、压测与性能

```bash
go build ./... && go vet ./...
go test ./... -count=1                 # 单元测试（含并发与边界）
go test -race ./... -count=1           # 竞态检测
go test -bench . -benchtime 100000x ./...   # 全模块基准
go test -run TestStress -v -timeout 600s ./ -count=1   # 压力测试
```

单元测试按模块组织（`buffer` / `internal` / `slcore` / `encoder` / `writer` / 根包），
覆盖命名规则、三种滚动条件、重启续写、四种清理策略、异步批处理与丢弃保护、
模板转义与 null、自定义渲染器、With 上下文隔离、全局日志器等。

### 测试覆盖率

| 包 | 覆盖率 |
|---|---|
| `slcore` / `encoder` / `buffer` / `internal/{pool,exit}` | **100.0%** |
| 根包 | 99.8% |
| `writer` | 98.0% |

所有包均有测试；剩余未覆盖语句为**已证明不可达**的防御性分支
（`panic` 兜底、恒不等式、Format/Parse 对称导致的回退等），
以及竞态防御分支（覆盖率随运行波动 ±0.6%）。
详见 [压测报告](doc/benchmark_report.md#0-测试覆盖率)。

```bash
go test ./... -cover -count=1
go test -race ./... -count=1
```

压力测试位于 `stress_test.go`，可用 `-short` 跳过，或用
`SLLOGGER_STRESS_GOROUTINES` / `SLLOGGER_STRESS_PER_ROUTINE` 调节规模，
覆盖并发无丢失、老化（高频滚动+清理）、队列饱和、多日志器并存与稳态内存。

### 性能速览（CALL_INFO 14 字段，约 200 B/条）

| 指标 | 优化前 | 优化后 | 提升 |
|---|---:|---:|---:|
| **异步端到端峰值** | 558,354 条/s | **1,030,603 条/s** | **+84.6%** |
| 异步常用配置（16 协程/batch=200） | 548,154 条/s | **989,198 条/s** | **+80.5%** |
| 异步耗时 | 1.824 μs/条 | **1.011 μs/条** | -45% |
| 异步入队（不含落盘） | — | 833 ns/条，153.7 MB/s | — |
| 纯编码 | — | 962 ns/条，176 B，1 alloc | — |
| 级别未启用时 | — | 7.4 ns/条，0 alloc | — |
| 稳态单条分配 | — | 486.8 B/op | — |
| 同步模式（未优化） | 98,998 条/s | 99,251 条/s | +0.3% |

数据为 A/B 实测（副本回退法 + 多轮中位数），见
[doc/throughput_tuning.md](doc/throughput_tuning.md#4-优化效果ab-实测对照)。

**数据一致性**：已验证至 200 万条（64 协程 / 最高吞吐配置 / 94 个滚动文件 /
751 MiB），写入条数与落盘行数严格相等，无丢失、重复、损坏与粘连；
并用 `BlockOnFull=false` 做阴性对照，校验能准确检出丢失（证明非假阳性）。

吞吐调优与瓶颈分析见 [doc/throughput_tuning.md](doc/throughput_tuning.md)。

详细基准数据、压测场景，以及压测中发现并修复的 11 个问题（如批量写入突破
`MaxSize`、清理误删活动文件、滚动 fsync 拖垮吞吐等），见
[doc/benchmark_report.md](doc/benchmark_report.md)。

## 使用约束

- `Hooks` 注册的回调**不得持有**接收到的 `[]Field`：`CallInfo`/`RequestInfo`
  路径的字段切片在 `Write` 返回后归还对象池（这是该路径零分配的前提）。
- `SetTraceExtractor` 传入 `nil` 表示取消已安装的提取器。
- 滚动时只 `Close` 不做 fsync；需要强持久化的场景请定期调用 `Sync()`
  （`Close()` 会执行一次 fsync）。
