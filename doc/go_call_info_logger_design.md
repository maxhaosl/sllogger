# Go CALL_INFO 日志库设计文档

**文档名称：** Go CALL_INFO 日志库设计方案  
**版本：** V1.0  
**语言：** Go  
**适用场景：** 微服务 HTTP/RPC 调用日志、接口访问日志、业务调用日志

---

## 1. 背景

当前系统需要在 Go 微服务中实现统一的调用日志能力，日志需要满足既定格式和文件规范。

日志文件目录：

```text
/data/logs/
```

日志文件名称：

```text
LOG_CALL_INFO.%d{yyyy-MM-dd}.{serviceName}{servicePort}.log
```

例如：

```text
/data/logs/LOG_CALL_INFO.2026-09-03.playurl8080.log
```

日志内容采用 `|` 分隔，主要包含两种格式。

### 1.1 CALL_INFO 日志

```text
date|log_type|service_Id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
```

示例：

```text
2026-09-03 11:23:45.376|INFO|playurl|3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|http://play.example.com:443/playurl/v1/play/playurl|GET|123|127.0.0.1|null|^MG.getContent:[690894368]
```

### 1.2 RequestInfo 日志

```text
date|log_type|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
```

示例：

```text
2026-09-03 11:23:45.376|INFO|playurl|3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|xxx|xxx|GET|false|request success
```

---

## 2. 设计目标

日志库需要满足以下目标：

1. 提供统一的 Go 日志 API。
2. 使用结构化数据描述日志，禁止业务代码手动拼接 `|` 字符串。
3. 支持 CALL_INFO 和 RequestInfo 两种固定日志格式。
4. 支持从 `context.Context` 自动获取 `trace_id` 和 `span_id`。
5. 支持异步日志写入。
6. 支持批量写入，减少文件 IO 和系统调用。
7. 支持按小时自动滚动日志文件。
8. 支持单文件超过 1GB 自动滚动。
9. 小时滚动和大小滚动同时生效。
10. 支持多 goroutine 并发写入。
11. 日志 IO 异常不能影响核心业务。
12. 支持日志文件生命周期管理。
13. 统一处理空值、特殊字符和字段转义。
14. 支持优雅关闭和日志 Flush。
15. 支持 Metrics、Benchmark、Race Test 和压力测试。
16. 方便后续扩展新的日志类型和日志输出方式。

---

## 3. 非目标

V1.0 不直接实现以下功能：

- Elasticsearch 直接写入；
- Kafka 直接写入；
- Loki 直接写入；
- 网络日志采集；
- 日志查询；
- 日志搜索；
- 多进程共享同一个日志文件；
- 日志加密；
- 日志压缩传输。

日志库主要负责：

```text
日志生成
   ↓
结构化编码
   ↓
异步队列
   ↓
批量写入
   ↓
本地文件
```

日志采集可以由 Filebeat、Fluent Bit、Vector 等独立组件完成。

---

# 4. 总体架构

```text
┌─────────────────────────────────────────────┐
│                 Business Service            │
│                                             │
│ logger.CallInfo(ctx, data)                  │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│                  Logger API                 │
│                                             │
│ CallInfo()                                  │
│ RequestInfo()                               │
│ Info() / Warn() / Error()                  │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│                  Encoder                    │
│                                             │
│ Struct → | separated byte stream            │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│               Async Queue                   │
│                                             │
│              Channel[10000]                 │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│              Batch Writer                   │
│                                             │
│       BatchSize = 100 / FlushInterval       │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│             RollingFileWriter               │
│                                             │
│  Hour Rotation       Size Rotation          │
│      1h                 1GB                 │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
                 /data/logs/
```

---

# 5. 核心设计原则

## 5.1 业务与日志实现解耦

业务代码只负责提供日志数据：

```go
logger.CallInfo(ctx, CallInfo{
    Mobile:   mobile,
    UserID:   userID,
    ClientID: clientID,
    URL:      req.URL.String(),
    Method:   req.Method,
})
```

业务代码不应该关心：

- 文件路径；
- 文件名称；
- 文件打开；
- 文件大小；
- Rotation；
- Buffer；
- Flush；
- Queue；
- 文件关闭。

---

## 5.2 多生产者 + 单消费者

采用：

```text
Goroutine 1 ─┐
Goroutine 2 ─┤
Goroutine 3 ─┤
Goroutine 4 ─┤
     ...      ├──→ Channel ──→ Worker ──→ File
Goroutine N ─┘
```

优点：

1. 业务 goroutine 不直接操作文件；
2. 文件写入集中管理；
3. Rotation 实现简单；
4. 减少锁竞争；
5. 保证 Writer 层的顺序性。

---

## 5.3 日志故障不能影响业务

日志系统属于辅助系统，应遵循：

> 业务稳定性优先于日志完整性。

例如磁盘满、文件写入失败、日志队列满时，CALL_INFO 默认允许丢弃，并通过 Metrics 进行统计。

---

# 6. 模块设计

建议工程目录：

```text
calllogger/
│
├── logger.go
├── config.go
├── errors.go
│
├── model/
│   ├── base.go
│   ├── call_info.go
│   └── request_info.go
│
├── encoder/
│   ├── encoder.go
│   ├── call_info_encoder.go
│   └── request_info_encoder.go
│
├── context/
│   └── context.go
│
├── writer/
│   ├── async_writer.go
│   ├── batch_writer.go
│   └── rolling_writer.go
│
├── rotation/
│   └── rotation.go
│
├── cleanup/
│   └── cleanup.go
│
└── test/
    ├── encoder_test.go
    ├── rolling_writer_test.go
    ├── async_writer_test.go
    └── benchmark_test.go
```

模块职责：

| 模块 | 职责 |
|---|---|
| logger | 对外日志 API |
| config | 配置定义与默认值 |
| model | 日志数据结构 |
| encoder | 日志格式编码 |
| context | Trace/Span 信息获取 |
| async writer | 异步队列 |
| batch writer | 批量 Flush |
| rolling writer | 文件写入和 Rotation |
| rotation | Rotation 策略 |
| cleanup | 历史文件清理 |
| test | 单元测试和性能测试 |

---

# 7. 数据模型设计

## 7.1 BaseLog

所有日志公共字段：

```go
type BaseLog struct {
    Date      time.Time
    LogType   string
    ServiceID string
    TraceID   string
    SpanID    string
}
```

字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| Date | time.Time | 日志产生时间 |
| LogType | string | INFO/WARN/ERROR/DEBUG |
| ServiceID | string | 服务 ID |
| TraceID | string | Trace ID |
| SpanID | string | Span ID |

---

# 8. CALL_INFO 数据结构

```go
type CallInfo struct {
    BaseLog

    Mobile   string
    UserID   string
    ClientID string

    URL      string
    Method   string
    UseTime  int64

    ServerIP string
    BussID   string

    LogMsg   string
}
```

对应字段：

```text
date|
log_type|
service_id|
trace_id|
span_id|
mobile|
userId|
clientId|
url|
method|
useTime|
serverIp|
buss_Id|
log_msg
```

字段说明：

| 字段 | 说明 |
|---|---|
| date | 日志产生时间 |
| log_type | 日志级别 |
| service_id | 微服务名称或服务 ID |
| trace_id | 链路 Trace ID |
| span_id | 链路 Span ID |
| mobile | 手机号 |
| userId | 用户 ID |
| clientId | 客户端 ID |
| url | 请求 URL |
| method | HTTP/RPC 方法 |
| useTime | 请求耗时 |
| serverIp | 服务端 IP |
| buss_Id | 业务 ID |
| log_msg | 日志消息 |

---

# 9. RequestInfo 数据结构

```go
type RequestInfo struct {
    BaseLog

    Header      string
    ReqHeader   string
    Method      string
    RateLimiter string

    LogMsg      string
}
```

对应格式：

```text
date|log_type|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
```

---

# 10. Context 设计

业务代码推荐：

```go
logger.CallInfo(ctx, info)
```

而不是：

```go
logger.CallInfo(traceID, spanID, info)
```

日志库从 `context.Context` 中获取：

```text
context.Context
      │
      ├── TraceID
      │
      └── SpanID
```

如果系统已经使用 OpenTelemetry，则优先从 OpenTelemetry `SpanContext` 获取。

没有 Trace/Span 时统一：

```text
trace_id = null
span_id  = null
```

---

# 11. Logger API

建议对外提供：

```go
type Logger interface {
    CallInfo(ctx context.Context, info CallInfo)

    RequestInfo(ctx context.Context, info RequestInfo)

    Info(ctx context.Context, msg string)

    Warn(ctx context.Context, msg string)

    Error(ctx context.Context, msg string)

    Sync() error

    Close() error
}
```

初始化：

```go
logger, err := calllogger.New(Config{
    ServiceName: "playurl",
    ServicePort: 8080,
    LogDir:      "/data/logs",
})
```

---

# 12. CALL_INFO 使用示例

```go
func Handle(ctx context.Context, req *http.Request) {
    start := time.Now()

    result := doSomething(ctx)

    logger.CallInfo(ctx, CallInfo{
        Mobile:   "18237438309",
        UserID:   "1071748417",
        ClientID: "c6559e74a24df8d97a2296ff1e23387a",

        URL:    req.URL.String(),
        Method: req.Method,

        UseTime: time.Since(start).Milliseconds(),

        ServerIP: "127.0.0.1",

        BussID: "null",

        LogMsg: "^MG.getContent:[690894368]",
    })

    _ = result
}
```

最终：

```text
2026-09-03 11:23:45.376|INFO|playurl|3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|http://play.example.com:443/playurl/v1/play/playurl|GET|123|127.0.0.1|null|^MG.getContent:[690894368]
```

---

# 13. Date 字段设计

日志格式：

```text
yyyy-MM-dd HH:mm:ss.SSS
```

Go Layout：

```go
const DateLayout = "2006-01-02 15:04:05.000"
```

`date` 表示：

> 日志事件产生时间，而不是日志实际落盘时间。

例如：

```text
业务请求
   ↓
11:29:35.376
   ↓
异步队列
   ↓
11:29:35.390
   ↓
实际落盘
```

最终日志中的时间应该为：

```text
11:29:35.376
```

---

# 14. Encoder 设计

Encoder 负责：

```text
Struct
  ↓
Field
  ↓
String
  ↓
| 分隔
  ↓
[]byte
```

例如：

```go
CallInfo{
    LogType:   "INFO",
    ServiceID: "playurl",
    TraceID:   "xxx",
}
```

转换为：

```text
2026-09-03 11:29:35.376|INFO|playurl|xxx|xxx|...
```

每条日志最后追加：

```text
\n
```

---

# 15. 字段转义

因为使用 `|` 作为字段分隔符，所以字段内容不能直接包含未处理的 `|`。

例如：

```text
log_msg = request|response
```

不能直接输出：

```text
...|request|response
```

否则下游解析会得到错误的字段数量。

建议定义：

```go
func EscapeField(value string) string
```

推荐转义规则：

```text
|   → \|
\r  → \r
\n  → \n
\   → \\
```

例如：

```text
request|response
```

编码为：

```text
request\|response
```

实际转义规则需要与下游日志解析系统保持一致。

---

# 16. 空值处理

统一使用：

```text
null
```

例如：

```go
func normalize(value string) string {
    if value == "" {
        return "null"
    }

    return value
}
```

避免同时出现：

```text
""
NULL
N/A
-
null
```

---

# 17. RollingFileWriter 设计

RollingFileWriter 是整个日志库的核心模块。

职责：

1. 创建日志目录；
2. 创建日志文件；
3. 写入日志；
4. 维护当前文件大小；
5. 检查小时变化；
6. 检查日期变化；
7. 检查文件大小；
8. 执行 Rotate；
9. 创建新文件；
10. 关闭文件；
11. 清理历史日志。

接口：

```go
type RollingWriter interface {
    Write(p []byte) (int, error)

    Rotate() error

    Close() error
}
```

---

# 18. 为什么自研 RollingWriter

本方案不直接依赖通用的日志 Rotation 实现，主要原因：

### 18.1 同时需要小时和大小滚动

要求：

```text
Hour >= 1h
OR
Size >= 1GB
```

### 18.2 文件名规则特殊

需要支持：

```text
LOG_CALL_INFO.{date}.{serviceName}{servicePort}.log
```

### 18.3 后续需要扩展

未来可能增加：

- 日期滚动；
- 小时滚动；
- Size 滚动；
- PID；
- 实例 ID；
- 自定义文件名；
- 日志类型。

自研 RollingWriter 更容易满足这些需求。

---

# 19. 文件命名设计

推荐的基础文件名称：

```text
LOG_CALL_INFO.{date}.{serviceName}{servicePort}.log
```

例如：

```text
LOG_CALL_INFO.2026-09-03.playurl8080.log
```

由于同时存在小时 Rotation 和 Size Rotation，建议实际内部文件名增加序号。

推荐：

```text
LOG_CALL_INFO.2026-09-03.playurl8080.11.001.log
```

含义：

```text
2026-09-03     日期
playurl        ServiceName
8080           ServicePort
11             小时
001            当前小时内文件序号
```

例如：

```text
/data/logs/
├── LOG_CALL_INFO.2026-09-03.playurl8080.09.001.log
├── LOG_CALL_INFO.2026-09-03.playurl8080.09.002.log
├── LOG_CALL_INFO.2026-09-03.playurl8080.10.001.log
├── LOG_CALL_INFO.2026-09-03.playurl8080.11.001.log
└── LOG_CALL_INFO.2026-09-03.playurl8080.11.002.log
```

---

# 20. 文件名策略抽象

文件命名建议独立成接口：

```go
type FileNameStrategy interface {
    CurrentFileName(t time.Time) string
    RotatedFileName(t time.Time, seq int) string
}
```

这样后续修改文件名不会影响 RollingWriter。

---

# 21. Rotation 触发规则

配置：

```text
MaxSize = 1GB
RotationInterval = 1h
```

每次真正写入之前检查：

```text
                   Write Request
                        │
                        ▼
                 Check Rotation
                        │
             ┌──────────┴──────────┐
             │                     │
        Hour Changed?          Size Enough?
             │                     │
            YES                   YES
             │                     │
             └──────────┬──────────┘
                        │
                       Rotate
                        │
                        ▼
                   New File
                        │
                        ▼
                     Write
```

只要满足一个条件，就执行 Rotate。

---

# 22. Size Rotation

不能只判断：

```go
if currentSize >= MaxSize {
    Rotate()
}
```

应该判断：

```go
if currentSize+int64(len(p)) > MaxSize {
    Rotate()
}
```

例如：

```text
MaxSize = 1GB

当前文件 = 1023.99MB

新日志 = 20KB
```

如果：

```text
1023.99MB + 20KB > 1GB
```

则：

```text
Rotate()
   ↓
创建新文件
   ↓
写入日志
```

目标：

> 单个日志文件尽量不超过 1GB。

---

# 23. Hour Rotation

不建议仅依赖：

```go
time.NewTicker(time.Hour)
```

而应该：

> 每次 Write 时检查当前时间。

例如：

```text
10:30 启动
      ↓
无日志
      ↓
12:35 第一条日志
```

此时 Writer 应根据当前时间判断是否需要切换。

推荐保存：

```go
fileHour time.Time
```

每次写入：

```go
currentHour := now.Truncate(time.Hour)

if !currentHour.Equal(w.fileHour) {
    w.Rotate()
}
```

---

# 24. Date Rotation

日期变化也必须触发 Rotation。

例如：

```text
2026-09-03 23:59:59
        ↓
2026-09-04 00:00:00
```

必须从：

```text
LOG_CALL_INFO.2026-09-03.playurl8080...
```

切换到：

```text
LOG_CALL_INFO.2026-09-04.playurl8080...
```

---

# 25. Rotation 原子性

RollingWriter 内部必须保证：

```text
Write
Rotate
Close
```

之间不会发生并发冲突。

推荐：

```go
type RollingWriter struct {
    mu sync.Mutex

    file *os.File

    size int64

    fileHour time.Time

    sequence uint64
}
```

Write：

```go
func (w *RollingWriter) Write(p []byte) (int, error) {
    w.mu.Lock()
    defer w.mu.Unlock()

    // Check rotation
    // Rotate if necessary
    // Write data

    return n, nil
}
```

---

# 26. AsyncWriter 设计

业务线程不直接写文件：

```text
Business
   │
   ▼
Encoder
   │
   ▼
Async Queue
   │
   ▼
Worker
   │
   ▼
RollingWriter
   │
   ▼
File
```

建议：

```go
type AsyncWriter struct {
    queue chan []byte

    writer *RollingWriter

    batchSize     int
    flushInterval time.Duration
}
```

---

# 27. Queue 设计

默认：

```text
QueueSize = 10000
```

实现：

```go
queue := make(chan []byte, 10000)
```

业务线程：

```text
Encode
  ↓
Queue
```

后台 Worker：

```text
Queue
  ↓
Dequeue
  ↓
Batch
  ↓
RollingWriter
```

---

# 28. Batch Writer

避免：

```text
一条日志
 ↓
write()
```

建议：

```text
100 条日志
 ↓
bytes.Buffer
 ↓
一次 write()
```

推荐配置：

```yaml
batch_size: 100
flush_interval: 100ms
```

满足任一条件即 Flush：

```text
BatchSize >= 100
```

或者：

```text
FlushInterval >= 100ms
```

---

# 29. Queue 满处理策略

建议分级处理。

### CALL_INFO / INFO

队列满：

```text
Queue Full
   ↓
Drop
   ↓
Dropped++
```

### ERROR

队列满：

```text
Queue Full
   ↓
Fallback / Block
```

但同步 fallback 必须避免因为磁盘故障导致业务请求长时间阻塞。

---

# 30. 日志丢弃策略

建议维护：

```go
type Metrics struct {
    Enqueued uint64
    Written  uint64
    Dropped  uint64

    WriteErrors uint64
    RotateCount uint64

    QueueSize uint64
}
```

重点监控：

```text
calllogger_dropped_total
calllogger_write_errors_total
calllogger_queue_size
calllogger_rotate_total
```

---

# 31. 日志写入错误处理

## 31.1 文件打开失败

```text
Open File
   ↓
Error
   ↓
内部错误计数
   ↓
业务继续
```

不应该因为日志文件打开失败直接导致服务 Panic。

---

## 31.2 文件写入失败

```text
Write Error
   ↓
WriteErrors++
   ↓
尝试重新打开
   ↓
失败
   ↓
丢弃低优先级日志
```

---

## 31.3 磁盘满

当收到 `ENOSPC`：

```text
ENOSPC
 ↓
WriteErrors++
 ↓
触发告警
 ↓
Drop INFO/CALL_INFO
```

避免日志系统拖垮业务服务。

---

# 32. 日志文件生命周期

建议支持：

```yaml
retention:
  max_age: 7d
```

例如当前日期：

```text
2026-09-03
```

保留：

```text
2026-09-02
2026-09-01
...
2026-08-27
```

超过保留时间的文件自动删除。

实际 retention 时间应根据日志采集系统要求确定。

---

# 33. Graceful Shutdown

服务收到：

```text
SIGTERM
```

执行：

```text
SIGTERM
  ↓
停止接收新请求
  ↓
logger.Close()
  ↓
停止接收新日志
  ↓
Drain Queue
  ↓
Flush Batch
  ↓
File.Sync()
  ↓
Close File
  ↓
Process Exit
```

目标：

> 尽可能保证服务正常退出时已经进入队列的日志全部落盘。

---

# 34. Sync

提供：

```go
func (l *Logger) Sync() error
```

处理：

```text
Queue
 ↓
Flush
 ↓
File.Sync()
```

主要用于：

- 服务关闭；
- 测试；
- 特殊业务场景；
- 需要确保日志落盘的场景。

---

# 35. 多进程限制

V1.0 默认：

> 一个日志文件只能由一个进程负责写入。

不支持：

```text
Process A ──→ playurl8080.log
Process B ──→ playurl8080.log
```

如果同一机器存在多个进程实例，建议文件名增加 PID 或实例 ID：

```text
LOG_CALL_INFO.2026-09-03.playurl8080.pid1234.log
```

或者：

```text
LOG_CALL_INFO.2026-09-03.playurl8080.instance01.log
```

---

# 36. 配置设计

```go
type Config struct {
    LogDir      string
    ServiceName string
    ServicePort int

    MaxSize int64

    RotationInterval time.Duration

    QueueSize int
    BatchSize int

    FlushInterval time.Duration

    MaxAge time.Duration

    EnableAsync bool
}
```

---

# 37. 推荐默认配置

```yaml
log:
  directory: /data/logs

  service_name: playurl
  service_port: 8080

  max_size: 1GB
  rotation_interval: 1h

  async:
    enabled: true
    queue_size: 10000
    batch_size: 100
    flush_interval: 100ms

  retention:
    max_age: 168h
```

---

# 38. RollingWriter 核心逻辑

伪代码：

```go
func (w *RollingWriter) Write(p []byte) (int, error) {
    w.mu.Lock()
    defer w.mu.Unlock()

    now := w.clock.Now()

    // 1. 日期/小时变化
    if !sameHour(now, w.fileHour) {
        if err := w.rotate(); err != nil {
            return 0, err
        }
    }

    // 2. 文件大小检查
    if w.size+int64(len(p)) > w.maxSize {
        if err := w.rotate(); err != nil {
            return 0, err
        }
    }

    // 3. 写入
    n, err := w.file.Write(p)
    w.size += int64(n)

    return n, err
}
```

---

# 39. Clock 抽象

为了方便测试，建议不要直接在 RollingWriter 中大量调用：

```go
time.Now()
```

而设计：

```go
type Clock interface {
    Now() time.Time
}
```

生产环境：

```go
type RealClock struct{}

func (RealClock) Now() time.Time {
    return time.Now()
}
```

测试环境使用：

```go
type MockClock struct {
    current time.Time
}
```

这样可以模拟：

```text
10:59:59
 ↓
11:00:00
```

而不需要真实等待一个小时。

---

# 40. 并发模型

整体采用：

```text
                    ┌──────────────┐
Goroutine 1 ───────>│              │
Goroutine 2 ───────>│ Async Queue  │
Goroutine 3 ───────>│              │
Goroutine N ───────>│              │
                    └──────┬───────┘
                           │
                           ▼
                        Worker
                           │
                           ▼
                       Batch Writer
                           │
                           ▼
                    RollingFileWriter
                           │
                           ▼
                         File
```

Worker 数量建议 V1.0 设置为：

```text
1
```

因为日志最终写入单个文件，单 Worker 可以简化顺序控制和 Rotation。

---

# 41. 日志顺序

单进程内：

> Queue 入队顺序 = Writer 消费顺序。

但多个 goroutine 同时调用日志接口时，不保证业务事件的绝对时间顺序。

如果业务需要严格排序，应依赖：

```text
date
trace_id
span_id
```

等字段。

---

# 42. Metrics

建议提供：

```go
type Metrics struct {
    Enqueued    uint64
    Written     uint64
    Dropped     uint64
    WriteErrors uint64
    RotateCount uint64
    QueueSize   uint64
}
```

后续可以接 Prometheus。

推荐指标：

```text
calllogger_enqueued_total
calllogger_written_total
calllogger_dropped_total
calllogger_write_errors_total
calllogger_rotate_total
calllogger_queue_size
```

---

# 43. 性能设计目标

V1.0 可以设置以下测试目标：

| 指标 | 目标 |
|---|---:|
| Encoder 单条编码 | < 5 μs |
| Queue 入队 | < 5 μs |
| Batch Size | 100 |
| Queue Size | 10000 |
| Flush Interval | 100ms |
| Max File Size | 1GB |
| Rotation Interval | 1h |

以上作为工程测试目标，不作为固定 SLA。最终性能需要通过实际机器和业务负载 Benchmark 验证。

---

# 44. 单元测试

## 44.1 Encoder

测试：

- 正常字段；
- 空字段；
- 中文；
- 特殊字符；
- `|`；
- `\n`；
- `\r`；
- `\`；
- 超长字段。

---

## 44.2 RollingWriter

测试：

- 正常写入；
- 小文件；
- Hour Rotation；
- Date Rotation；
- Size Rotation；
- Hour + Size 同时触发；
- Rotate 后文件创建；
- Close；
- 并发写入。

---

## 44.3 AsyncWriter

测试：

- 正常入队；
- Batch；
- Flush；
- Queue Full；
- Drop；
- Close；
- Drain；
- Worker 异常；
- Writer 异常。

---

# 45. Rotation 测试

测试时不要真的创建 1GB 文件。

将：

```go
MaxSize = 1GB
```

替换成：

```go
MaxSize = 1KB
```

例如：

```text
写入 900 bytes
 ↓
不 Rotation

再写入 200 bytes
 ↓
900 + 200 > 1024
 ↓
Rotation
 ↓
新文件
```

---

# 46. Benchmark

至少提供：

```go
func BenchmarkCallInfo(b *testing.B)
```

测试：

```text
CallInfo
 ↓
Encoder
```

以及：

```go
func BenchmarkAsyncLogger(b *testing.B)
```

测试：

```text
CallInfo
 ↓
Queue
```

以及：

```go
func BenchmarkRollingWriter(b *testing.B)
```

测试：

```text
[]byte
 ↓
RollingWriter
 ↓
File
```

---

# 47. Race Test

必须支持：

```bash
go test -race ./...
```

重点检查：

- Logger；
- AsyncWriter；
- RollingWriter；
- Metrics；
- Close；
- Rotate；
- Queue。

---

# 48. 工程依赖

V1.0 建议尽量减少第三方依赖。

核心使用 Go 标准库：

```text
context
time
sync
bytes
bufio
os
io
path/filepath
atomic
```

如果项目已经有统一的通用日志体系，可以额外集成 Zap；但 CALL_INFO 的核心格式仍由自定义 Encoder 控制。

---

# 49. 日志采集建议

日志库只负责本地文件：

```text
Go Service
    ↓
CallLogger
    ↓
/data/logs/*.log
    ↓
Filebeat / Fluent Bit / Vector
    ↓
Kafka / Elasticsearch / Loki / 日志平台
```

这样日志 SDK 与日志平台解耦。

---

# 50. 安全与隐私

CALL_INFO 中包含：

```text
mobile
userId
clientId
```

这些字段可能属于敏感业务数据，因此日志库本身应提供可扩展的脱敏能力。

建议后续支持：

```go
type FieldSanitizer interface {
    Sanitize(field string, value string) string
}
```

例如：

```text
18237438309
```

根据业务要求转换为：

```text
182****8309
```

是否脱敏、脱敏规则和范围由业务安全规范确定。

---

# 51. 日志大小保护

除了单文件 1GB 限制，还建议考虑单条日志大小限制：

```yaml
max_entry_size: 1MB
```

避免异常的：

```text
header
request body
response body
log_msg
```

导致单条日志达到几十 MB。

超过限制可以：

```text
截断
+
记录 truncated 标志
```

例如：

```text
log_msg = xxx...
```

---

# 52. 推荐开发阶段

## Phase 1：基础模型

实现：

```text
BaseLog
CallInfo
RequestInfo
Config
Logger API
```

---

## Phase 2：Encoder

实现：

```text
CallInfoEncoder
RequestInfoEncoder
EscapeField
normalize
```

完成：

```text
Struct
 ↓
| separated string
```

---

## Phase 3：RollingWriter

实现：

```text
File Open
Write
Hour Rotation
Date Rotation
Size Rotation
Close
```

此阶段先不加入 Async。

---

## Phase 4：AsyncWriter

实现：

```text
Channel
Worker
Batch
Flush
Queue Full
Drop
```

---

## Phase 5：Context

实现：

```text
TraceID
SpanID
OpenTelemetry
```

---

## Phase 6：生产能力

增加：

```text
Metrics
Retry
Disk Full
Cleanup
Graceful Shutdown
```

---

## Phase 7：测试

完成：

```text
Unit Test
Race Test
Benchmark
Stress Test
```

---

# 53. 最终架构

```text
                     ┌─────────────────┐
                     │    Go Service   │
                     └────────┬────────┘
                              │
                              ▼
                     ┌─────────────────┐
                     │    CallLogger   │
                     └────────┬────────┘
                              │
                       Structured Data
                              │
                              ▼
                     ┌─────────────────┐
                     │     Encoder     │
                     └────────┬────────┘
                              │
                         []byte + \n
                              │
                              ▼
                     ┌─────────────────┐
                     │  Async Queue    │
                     │    10000        │
                     └────────┬────────┘
                              │
                              ▼
                     ┌─────────────────┐
                     │  Batch Writer   │
                     │      100        │
                     └────────┬────────┘
                              │
                              ▼
               ┌──────────────────────────────┐
               │      RollingFileWriter       │
               │                              │
               │   Hour       Size            │
               │   1h         1GB             │
               │     \       /                │
               │       Rotate                 │
               └──────────────┬───────────────┘
                              │
                              ▼
                       /data/logs/
                              │
                              ▼
             LOG_CALL_INFO.2026-09-03.playurl8080...
```

---

# 54. 最终设计决策

V1.0 的核心设计决策如下：

1. **业务只提交结构化 `CallInfo/RequestInfo`，不允许业务代码自行拼接日志字符串。**
2. **CALL_INFO 使用自定义 Encoder，最终输出固定的 `|` 分隔格式。**
3. **通过 `context.Context` 获取 Trace ID 和 Span ID。**
4. **采用多生产者 + 单消费者模型。**
5. **使用异步 Queue，避免业务线程直接执行文件 IO。**
6. **采用 Batch Writer 降低系统调用次数。**
7. **RollingFileWriter 自研，实现 Hour + Date + Size 三种 Rotation 条件。**
8. **单文件最大 1GB，写入前检查 `currentSize + incomingSize`。**
9. **每次 Write 检查当前小时，而不是单纯依赖定时器。**
10. **Queue 满时 CALL_INFO/INFO 默认允许丢弃。**
11. **日志写入异常不能导致业务服务 Panic。**
12. **服务退出时执行 Queue Drain + Flush + Sync + Close。**
13. **V1.0 默认单进程独占日志文件。**
14. **通过 Metrics 统计写入、丢弃、错误和 Rotation。**
15. **通过 Clock 抽象保证 Hour Rotation 可以进行快速、可靠的单元测试。**
16. **通过 FileNameStrategy 解耦日志文件命名规则。**
17. **通过 retention 机制控制历史日志占用的磁盘空间。**

---

# 55. V1.0 推荐配置

```yaml
log:
  directory: /data/logs

  service_name: playurl
  service_port: 8080

  max_size: 1GB
  rotation_interval: 1h

  async:
    enabled: true
    queue_size: 10000
    batch_size: 100
    flush_interval: 100ms

  retention:
    max_age: 168h
```

---

# 56. V1.0 预期使用方式

最终业务侧只需要：

```go
logger, err := calllogger.New(Config{
    LogDir:       "/data/logs",
    ServiceName:  "playurl",
    ServicePort:  8080,
    MaxSize:      1 << 30,
    QueueSize:    10000,
    BatchSize:    100,
    FlushInterval: 100 * time.Millisecond,
    RotationInterval: time.Hour,
})
if err != nil {
    // handle initialization error
}

defer logger.Close()

logger.CallInfo(ctx, CallInfo{
    Mobile:   mobile,
    UserID:   userID,
    ClientID: clientID,
    URL:      req.URL.String(),
    Method:   req.Method,
    UseTime:  time.Since(start).Milliseconds(),
    ServerIP: serverIP,
    BussID:   bussID,
    LogMsg:   "^MG.getContent:[690894368]",
})
```

业务代码无需感知：

```text
Async Queue
Batch
Flush
Rotation
File Size
File Name
Trace ID
Span ID
File Close
```

---

# 57. 结论

本方案将日志库划分为：

```text
Logger
  ↓
Model
  ↓
Encoder
  ↓
Async Queue
  ↓
Batch Writer
  ↓
RollingFileWriter
  ↓
Local File
```

核心能力为：

```text
                    CALL_INFO
                        │
                        ▼
                 Structured Model
                        │
                        ▼
                     Encoder
                        │
                        ▼
                  Async Queue
                   10000 entries
                        │
                        ▼
                  Batch Writer
                   100 entries
                        │
                        ▼
              RollingFileWriter
                 ┌──────┴──────┐
                 │             │
              Hourly         Size
                1h           1GB
                 │             │
                 └──────┬──────┘
                        ▼
                   Local File
```

该设计能够满足当前 CALL_INFO 日志格式要求，同时为后续增加更多日志类型、日志采集方式、监控指标、脱敏策略以及不同 Rotation 策略保留扩展空间。
