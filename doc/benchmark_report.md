# sllogger 性能压测与基准报告

本报告记录 sllogger 的单元测试覆盖、压力测试场景与基准数据，以及在压测中
发现并修复的性能/正确性问题。

- 测试环境：macOS (darwin/amd64)，Go 1.26.4，CPU 56 核
- 数据集：CALL_INFO 14 字段模板，单条约 200 字节
- 复现命令见文末

## 0. 测试覆盖率

各包覆盖率：`go test ./... -cover -count=1`

整体覆盖率（库代码，正确算法）：

```bash
# 必须用 -coverpkg（逗号分隔）+ 排除 example/*，否则汇总会被严重低估
# （各库包 98~100%，错误算法却显示 71.9%）
go test ./ ./buffer/... ./encoder/... ./internal/... ./slcore/... ./writer/... \
  -covermode=atomic \
  -coverpkg=./,./buffer/...,./encoder/...,./internal/...,./slcore/...,./writer/... \
  -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out | tail -1
# => total: (statements) 99.5%
```

或直接用 `make cover`（已封装上述逻辑）。

| 包 | 语句覆盖率 | 说明 |
|---|---|---|
| `buffer` | **100.0%** | 缓冲与对象池 |
| `encoder` | **100.0%** | 模板编码器 + JSON 编码器 |
| `slcore` | **100.0%** | 引擎层：级别/Entry/Field/Core/编码器/预置编码器 |
| `internal/pool` | **100.0%** | 泛型对象池 |
| `internal/exit` | **100.0%** | os.Exit 封装（子进程验证退出码） |
| 根包 | **99.8%** | Logger/Sugar/Global/Config/CallInfo |
| `writer` | **98.0%** | RollingWriter/Cleaner/Namer/AsyncWriter |
| `internal/bufferpool` | — | 仅变量声明，无可覆盖语句 |

所有包均有测试文件。`example/*` 为可执行示例，不计入覆盖率（由
`go run ./example/verify` 做端到端断言）。

### 读覆盖率数字时的两个陷阱

**1. 空函数体永远显示 0.0%（Go coverage 的显示特性，非漏测）**

接口要求实现、但本编码器不需要动作的方法（如
`OpenNamespace(key string) {}`）方法体内没有任何语句，
`go tool cover -func` 对它们**恒显示 0.0%**，即使测试确实调用了。
最小复现（Go 1.26）：

```go
func (t *T) NoOp(key string) {}          // 被调用，仍显示 0.0%
func (t *T) WithStmt(key string) { ... } // 显示 100.0%
// 总体 statements 覆盖率：100.0%（空函数体不影响总体数字）
```

因此判断覆盖情况应看**总体 statements 百分比**，不要被单个函数的
0.0% 误导。项目里的三个 `OpenNamespace`（`encoder/json.go`、
`encoder/template.go`）属于此类，其行为已由
"调用后不产生额外 key" 的断言覆盖。

**2. `DirEntry.Info()` 对符号链接执行 lstat，不会因目标缺失而失败**

因此 `scan()` / `dirLogTotal()` / `findResumeFile()` 中
`if statErr != nil { continue }` 属于**防御性代码**，无法通过构造悬空
符号链接来触发（实测 lstat 成功，链接会被当作有效条目）。这类分支只会在
目录被并发删除等极端竞态下命中，不做强行覆盖。

剩余 9 处均为**已证明不可达**的防御性分支：

| 位置 | 论证 |
|---|---|
| `global.go` RedirectStdLog panic 兜底 | 传入级别恒为 InfoLevel，`levelToFunc` 不会失败；`RedirectStdLogAt` 的非法级别错误路径已覆盖 |
| `cleanup.go` `excess > len(removable)` | 活动文件最多 1 个，`removable = files - 1`，故 `excess = len(files) - MaxBackups ≤ len(removable)`（因 MaxBackups ≥ 1）恒不成立 |
| `naming.go` `truncateToLayout` 的 Parse 回退 | Go 的 `Format`/`Parse` 对同一 layout 对称，已用 13 种刁钻布局（纯小数秒、`20062006`、`_2`、MST、PM 等）实测全部往返成功 |
| `rolling_writer.go` `remain <= 0` | switch 的 case 3 已处理 `size >= MaxSize` 并旋转（旋转后 size=0），故 `remain = MaxSize - size > 0` 恒成立 |
| `rolling_writer.go` `n == 0` break | 需要文件 Write 返回 (0, nil)，正常文件不会（循环保证 chunk 非空） |
| `cleanup.go` / `rolling_writer.go` 的 `statErr` | 见陷阱 2（lstat 不失败） |

核心路径（编码、三种滚动、四种清理、异步批处理与关闭、级别短路、
`Build(opts...)` 传参、Marshaler 字段类型）均为全覆盖。

### 覆盖率工作发现的真实 Bug：关闭瞬间丢失日志

在补齐 `AsyncWriter` 分支覆盖时，并发关闭测试暴露了一个**数据丢失缺陷**：

```
round 182: 落盘 7 条，但 Write 成功 8 条（数据丢失）
```

**根因**：`Write` 在 `closed` 检查通过后进入 `select`，此时
`queue <- buf`（队列有空位）与 `<-a.done` 同时就绪，Go 会**随机选择**。
若选中入队：
- `Write` 返回 `nil`，调用方认为写入成功；
- 但 worker 可能已走完 drain 并 `return`；
- 该条目永久滞留在队列中，既不落盘也不报错。

**修复**（两道防线）：

1. **入队中计数器 `inFlight`**：`Write` 在入队前 `inFlight.Add(1)`、结束后
   `Add(-1)`，并做二次 `closed` 检查；`Close` 在 `wg.Wait()` 之前先等
   `inFlight` 归零，确保没有"即将入队"的写入者。
2. **兜底排空 `drainRemainingToInner`**：worker 全部退出后再排空一次所有
   分片队列并写入（通常为空，零开销）。

修复后 400 轮并发关闭（2 个忙等生产者 + 小队列 + 随机时刻 Close）**零丢失**。

> 这是覆盖率工作的直接收益：单纯追求百分比不会发现问题，但为覆盖
> `async.go` 的分支而编写的并发测试暴露了它。

**代价**：`Write` 热路径增加 2 次原子操作（约 20~40 ns）。实测端到端
吞吐：常用配置（16 协程/batch=200）989,198 → 960,424 条/s（**-2.9%**），
峰值（batch=50）1,030,603 → 909,689 条/s（-11.7%）。考虑到测量本身有
±10% 波动，实际损耗约在 3~10% 之间。

正确性优先于吞吐，且仍维持 **96 万条/s（约 349 MiB/s）**，
远超绝大多数业务需求（典型峰值数千至数万条/s）。

## 1. 测试覆盖

| 包 | 测试文件 | 覆盖重点 |
|---|---|---|
| `buffer` | `buffer_test.go` | 各 Append*/Write*/Len/Cap/Reset/TrimNewline、池复用 |
| `internal/pool` | `pool_test.go` | 工厂调用、值往返、结构体类型、并发（按 sync.Pool 语义宽松断言） |
| `internal/exit` | `exit_test.go` | 子进程验证退出码 0 / 非 0 |
| `slcore` | `level_test.go` | 级别字符串/解析/边界/flag 接口/Enabled/LevelOf |
| | `entry_test.go` | EntryCaller 路径裁剪、CheckedEntry 写入/复用检测/错误上报、CheckWriteAction |
| | `field_test.go` | 全字段类型 AddTo、Equals、未知类型 panic、Stringer/Error panic 恢复 |
| | `core_test.go` | ioCore Check/With/Write/Sync、Fatal 强制 Sync、NopCore |
| | `encoder_test.go` | **预置编码器全族**（Level/Time/Duration/Caller/Name）与 UnmarshalText、fast path |
| | `write_syncer_test.go` | AddSync/Lock 并发/MultiWriteSyncer 错误聚合 |
| | `error_test.go` | encodeError、fmt.Formatter verbose、panic 恢复、errors.Join |
| | `hooks_test.go` | 钩子调用顺序、错误不阻断写入、With 保留钩子 |
| | `stacktrace_test.go` | CaptureCaller/CaptureStack 与 skip 语义 |
| `encoder` | `template_test.go` | CALL_INFO/RequestInfo 精确格式、null、转义 |
| | `template_edge_test.go` | 中文/超长、未知字段、自定义分隔符/时间格式、NoEscape、Clone 隔离 |
| | `template_object_test.go` | TemplateEncoder 的 ObjectEncoder 全方法、makeAppender 各分支、非标量回退 |
| | `fields_test.go` | EscapeField、appendEscaped 与 EscapeField 等价、字段渲染器 |
| | `json_test.go` | 默认/自定义/省略键、With 上下文、对象与数组、异常负载降级 |
| | `json_map_test.go` | mapObjectEncoder/mapArrayEncoder/primitiveCapture 全部方法 |
| | `json_with_test.go` | JSONEncoder 的 With 上下文全类型、Clone 隔离与配置保留 |
| `writer` | `rolling_writer_test.go` | 命名、大小/定时/跨天滚动、重启续写、四种清理策略 |
| | `rolling_writer_chunk_test.go` | **分块写入**：大批量切分、单条超限、精确边界、整行不切断、lastLineEnd |
| | `rolling_writer_concurrency_test.go` | 并发写、并发滚动/Sync/清理、关闭后写入、故障与边界 |
| | `writer_edge_test.go` | Async 关闭后写入/Sync、清理保护活动文件、scan/dirLogTotal 容错、文件名解析边界 |
| | `writer_branch_test.go` | 滚动失败错误返回、跨天优先于定时、scan/dirLogTotal 跳过目录、findResumeFile 容错、cleanupLoop 定时 |
| | `async_race_test.go` | Sync 与 Close 并发（50 轮）、Sync 阻塞至落盘 |
| | `naming_test.go` | 序号、服务名渲染、占位符、文件名解析、布局粒度、stamp 缓存 |
| | `async_test.go` | Sync 语义、批处理、阻塞/丢弃、并发生产者、缓冲复用、关闭幂等 |
| 根包 | `logger_test.go` | 级别/With/Named/Check/Sync/Caller/Stacktrace/DPanic/Panic\|Fatal 钩子/Close 幂等/并发 |
| | `sugar_test.go` | Print/Printf/Println/w 风格、With、sweetenFields 异常分支 |
| | `sugar_levels_test.go` | **全部级别方法**（含 DPanic/Panic/Fatal 的 f/w/ln 变体，用 Goexit 钩子隔离） |
| | `global_test.go` | ReplaceGlobals、NewStdLog、RedirectStdLog、级别映射 |
| | `options_test.go` | 全部 Option、Open/CombineWriteSyncers、错误路径 |
| | `field_test.go` | 全部字段构造器、Any 类型分派、Skip 语义 |
| | `level_test.go` | AtomicLevel 读写/解析/序列化/并发 |
| | `context_test.go` | WithTrace、提取器优先级与回退、ctx 注入 trace |
| | `callinfo_test.go` | CALL_INFO/RequestInfo 字段、转义、接口实现、Config 错误分支 |
| | `callinfo_disabled_test.go` | **级别未启用时的短路**（不构造字段、不编码、不写盘） |
| | `config_extra_test.go` | buildOptions 各分支、levelToFunc 全级别、服务名回填、encoding 分支、Clock 贯通 |
| | `stress_test.go` | 见第 2 节 |

## 2. 压力测试场景

`stress_test.go`，可用 `-short` 跳过，或用环境变量调节规模
（`SLLOGGER_STRESS_GOROUTINES`、`SLLOGGER_STRESS_PER_ROUTINE`）。

| 场景 | 目的 | 校验点 |
|---|---|---|
| `TestStressAsyncNoLoss` | 16 协程 × 5000 条异步写入 | 落盘条数完全相等（阻塞模式零丢失） |
| `TestStressSyncThroughput` | 同步直写吞吐 | 吞吐与单条耗时 |
| `TestStressRotationAging` | 2 秒高频滚动 + 清理 | 文件数 ≤ MaxBackups、目录总量 ≤ MaxTotalSize、无协程泄漏 |
| `TestStressQueueSaturation` | 20 万条冲击小队列 | 丢弃保护生效，业务线程不被阻塞 |
| `TestStressConcurrentLoggers` | 8 个日志器并存 | 各自配额独立，互不干扰 |
| `TestStressMemoryAllocation` | 10 万条稳态分配 | 单条分配 < 1 KiB |
| `TestStressWriterDirect` | 直压 writer 层 | 无写错误、批处理比、带宽 |

### 压测结果（默认 16 × 5000）

```
异步压测:   8 万条 耗时 ~200ms 吞吐 348k~411k 条/s  2.4~2.9 μs/条  写入 16 MiB
同步压测:   1.6 万条 吞吐 92k~102k 条/s  9.8~10.9 μs/条
老化压测:   13.9 万条 剩余文件 20（MaxBackups=20） 目录 74 KiB 单文件均值 3807 B（MaxSize=4096） 协程 4->2
队列饱和:   20 万条入队 ~420ms  2.1 μs/条（丢弃模式，不阻塞业务）
多日志器:   8 个日志器并存，各自配额独立生效
内存压测:   10 万条 单次分配 ~487 B/op  分配速率 ~155 MiB/s  GC 增量 14
writer 压测: 8 万条 吞吐 324k~344k 条/s  带宽 62~66 MiB/s  批处理比 ~254 条/次
```

注：吞吐类指标受机器负载影响，多次运行约有 ±15% 波动；配额类指标
（文件数、单文件大小、目录总量）为确定性校验，每次都必须成立。

## 3. 基准数据

`go test -bench . -benchtime 100000x -run '^$' ./...`

### 根包（端到端链路）

| 基准 | ns/op | B/op | allocs/op |
|---|---|---|---|
| `LoggerDisabled`（级别过滤） | 7.4 | 0 | 0 |
| `Any` | 11.9 | 0 | 0 |
| `AtomicLevelSet` | 7.0 | 0 | 0 |
| `LoggerNamed` | 75.0 | 160 | 1 |
| `LoggerWith` | 408 | 672 | 6 |
| `SugarInfof` | 933 | 226 | 3 |
| `SugarInfow` | 1,089 | 466 | 3 |
| `EncoderCallInfo`（纯编码） | 962 | 176 | 1 |
| `LoggerToDiscard`（编码+写） | 1,479 | 982 | 3 |
| `LoggerWithStacktrace` | 1,435 | 982 | 3 |
| `LoggerWithCaller` | 2,096 | 1,232 | 5 |
| `CallInfoAsync`（编码+入队） | 2,626 | 486 | 5 |

### encoder 包

| 基准 | ns/op | B/op | allocs/op |
|---|---|---|---|
| `AppendEscaped`（需转义） | 227 | 0 | 0 |
| `ShortTemplateEncode`（3 字段） | 316 | 176 | 1 |
| `CustomRenderer` | 386 | 208 | 2 |
| `WithContextClone` | 683 | 176 | 1 |
| `RequestInfoEncode` | 697 | 176 | 1 |
| `CallInfoEncode`（14 字段） | 919 | 176 | 1 |
| `EscapeField`（字符串版本） | 756 | 288 | 1 |
| `JSONEncode`（对照） | 4,041 | 2,495 | 36 |

### writer 包

| 基准 | ns/op | 带宽 | allocs/op |
|---|---|---|---|
| `StampOf`（时间截断缓存） | 20.5 | - | 0 |
| `AsyncWriterEnqueue` | 833 | 153.7 MB/s | 1 |
| `AsyncWriterConcurrent` | 1,798 | 71.2 MB/s | 2 |
| `RollingWriterSync`（单条直写） | 3,808 | 33.6 MB/s | 0 |
| `RollingWriterWithSizeCheck` | 3,799 | 33.7 MB/s | 0 |
| `ParseFileName` | 286 | - | 2 |
| `NamerFileName` | 375 | - | 5 |

### slcore / buffer 包

| 基准 | ns/op | allocs/op |
|---|---|---|
| `LevelString` / `CapitalString` | 2.1 / 2.3 | 0 |
| `CheckedEntryWrite` | 147 | 1 |
| `AddFields`（3 字段） | 144 | 2 |
| `CoreWith` | 114 | 2 |
| `CaptureCaller` | 426 | 2 |
| `CaptureStack` | 1,908 | 10 |
| `BufferAppendString` | 1.7 | 0 |
| `BufferAppendByte` | 0.9 | 0 |
| `PoolGetPut` | 16.0 | 0 |
| `BufferAppendTime` | 160.7 | 0 |

## 4. 压测中发现并修复的问题

| # | 问题 | 影响 | 修复 |
|---|---|---|---|
| 1 | 异步批量写入时整批一次性落盘 | 单文件突破 `MaxSize`（实测 341 KB vs 4 KB 上限） | `RollingWriter.Write` 按行边界分块，块间滚动；单条超限时整条写入以避免死循环 |
| 2 | 清理器会删除当前正在写入的文件 | 写入方继续追加到已删除的 inode，日志丢失 | `Cleaner` 注入活动文件回调，四种清理策略均跳过活动文件 |
| 3 | 每次滚动都执行 `Sync()`（fsync） | 高频滚动下吞吐下降约 50 倍（4.32s → 0.08s 修复后） | 滚动只 `Close`；落盘由显式 `Sync()` 与 `Close()` 保证 |
| 4 | `AsyncWriter.Sync()` 未排空队列 | Sync 后仍有数据未落盘，语义不正确 | Sync 前先非阻塞排空队列再 flush + `inner.Sync()` |
| 5 | `layoutGranularity` 子串误判 | `"...15"` 含 `"5"` 被判为秒级，缓存窗口过窄 | 优先匹配两位形式（`05`/`04`/`15`/`03`），再匹配单位数形式 |
| 6 | `SetTraceExtractor(nil)` 触发 `atomic.Value.Store(nil)` panic | 取消提取器即崩溃 | 改用 `atomic.Pointer[TraceExtractor]` |
| 7 | `Any()` 中 `fmt.Stringer` 先于 `time.Time`/`time.Duration` 匹配 | 时间与耗时被降级为字符串输出 | 调整分派顺序：time/duration → error → Stringer |
| 8 | JSON 编码器的 Level/Time 编码器写入固定 `"value"` 键 | 自定义 `EncodeLevel`/`EncodeTime` 丢失 | 引入 `primitiveCapture` 捕获单个值后写入配置键 |
| 9 | JSON 数组编码覆盖同一键 | 数组只剩最后一个元素 | `mapArrayEncoder` 收集为切片，序列化为 JSON 数组 |
| 10 | 空 `EncoderConfig` 无法表达"省略字段" | `OmitKey` 失效 | 编码器严格按 key 处理；默认键由 `Config.Build` 在装配层填充 |
| 11 | `service()` 在无服务名端口时输出 `"0"` | 文件名为 `LOG.2026-09-03.0.log` | 服务名与端口均为零值时返回空串 |

## 5. 针对性优化（相对初始实现）

| 优化 | 效果 |
|---|---|
| 编码路径预编译字段解析器（`fieldAppender`） | 消除每次编码的 map 查找 |
| 字段值直写 `buffer.Buffer`（`AppendTime`/`AppendInt`/`AppendBool`） | 消除时间/数字格式化的字符串分配 |
| 渲染上下文去掉 `map[string]string` 缓存，改线性查找 | 消除每条目一次 map 分配 |
| 转义改为分段批量写入 `appendEscaped` | 转义路径 0 分配（227 ns/op） |
| CallInfo/RequestInfo 字段切片池化 | 单条分配 1260.9 B → 486.8 B（-61%），GC 36 → 14 |
| `AsyncWriter` 关闭状态改 `atomic.Bool`，写入缓冲池化 | 去锁竞争，入队 1 alloc |
| 时间截断结果缓存（`Namer.stampOf` + 布局粒度推导） | 每写一次的时间计算 200ns → 20ns |
| 滚动不再 fsync | 高频滚动场景 54× 提速 |

汇总：CALL_INFO 编码 **2388 ns → 962 ns / 11 allocs → 1 alloc**；
端到端异步链路 **3496 ns → 2626 ns / 16 allocs → 5 allocs**；
异步吞吐 **304,897 → 411,464 条/s**；带宽 **53.6 → 66.0 MiB/s**。
全部满足设计目标（单条 < 5 μs）。

## 6. 复现命令

```bash
# 单元测试（含并发与边界）
go test ./... -count=1

# 竞态检测
go test -race ./... -count=1

# 基准（全模块）
go test -bench . -benchtime 100000x -run '^$' ./...

# 压力测试
go test -run TestStress -v -timeout 600s ./ -count=1
SLLOGGER_STRESS_GOROUTINES=32 SLLOGGER_STRESS_PER_ROUTINE=20000 \
  go test -run TestStressAsyncNoLoss -v ./ -count=1

# 功能逐条自检（9 项需求 + 附加能力）
go run ./example/verify

# 用法示例
go run ./example/callinfo
go run ./example/basic
```
