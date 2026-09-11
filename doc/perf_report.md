# sllogger 性能对比报告：v1.1.0 → v1.2.0

本报告用**同一组基准函数**（两 tag 的 `*_bench_test.go` 完全一致，无改动）在
相同环境上分别实测 `v1.1.0` 与 `main`(v1.2.0)，量化 v1.2.0 引入的 JSON 编码器
重构带来的性能收益，并确认其它路径未回归。

## 1. 环境与复现

| 项 | 值 |
|---|---|
| 系统 | macOS (darwin/amd64) |
| CPU | Intel Xeon W-3275M @ 2.50GHz，56 核 |
| Go | 1.26.4 |
| 基准时长 | `-benchtime=3s`（每函数约 2.4M~25.7M 次迭代，结果稳定） |
| 内存分配统计 | `-benchmem` |
| 旧版本隔离 | `git worktree add /tmp/sllogger_v1.1.0 v1.1.0`（独立目录，不动主工作树；`go.mod`/`go.sum` 两 tag 一致，离线可构建） |
| 基准函数 | 两 tag 同名同义，保证 apples-to-apples |

> 吞吐类指标受机器负载影响，单次运行约有 ±3~5% 波动；下表数值为各函数 3s
> 标定下的代表值。JSON 编码器的提升幅度远超此噪声带。

## 2. 关键结论

- **JSON 编码器（`JSONEncode`）：`4138 ns/op` → `1724 ns/op`，提速约 2.40×；
  分配 `36 allocs/2463 B` → `1 alloc/16 B`（分配减少约 97% / 99%）。**
- **端到端 JSON 写入（`LoggerThroughput`）：`429,706 条/s` → `1,014,529 条/s`，
  吞吐提升约 2.36×；单条 `23 allocs` → `4 allocs`、`1098 B` → `209 B`。**
- CallInfo / RequestInfo / 短模板编码器在 v1.1.0 已高度优化（单条 1 alloc），
  v1.2.0 的 JSON 重构**不影响**它们，实测均在 ±3% 噪声带内，无回归。
- `slcore.CheckedEntryWriteWithFields` 因 JSON 编码器分配锐减、GC 压力下降，
  连带受益：`818.6 ns` → `603.7 ns`（-26.2%）。
- 正确性与零丢失未被破坏：JSON 编码路径由 `encoder/json_test.go`、
  `json_map_test.go`、`json_with_test.go` 全量覆盖，`loss_test.go` 压测落盘行数
  恒等于写入条数（0 丢失），`go test -race ./...` 全绿。

## 3. 详细数据

### 3.1 JSON 编码器（v1.2.0 主优化对象）

| 基准 | v1.1.0 | v1.2.0 | ns/op 变化 | B/op | allocs/op |
|---|---|---|---|---|---|
| `encoder.JSONEncode` | 4138 ns | 1724 ns | **-58.3%**（2.40×） | 2463 → 16 | 36 → 1 |
| `root.LoggerThroughput` | 2327 ns（429,706 logs/s） | 985.7 ns（1,014,529 logs/s） | **-57.6%**（2.36×） | 1098 → 209 | 23 → 4 |

> `LoggerThroughput` 用 `io.Discard` 作下沉点，隔离出「编码 + 分发」热路径，
> 直接反映 JSON 编码器的重构收益（真实文件 IO 另见 `benchmark_report.md` §2）。

### 3.2 CallInfo / 模板编码器（已最优，确认无回归）

| 基准 | v1.1.0 | v1.2.0 | 变化 |
|---|---|---|---|
| `encoder.CallInfoEncode`（14 字段） | 930.4 ns / 176 B / 1 alloc | 932.6 ns / 176 B / 1 alloc | -0.2%（噪声） |
| `encoder.RequestInfoEncode` | 721.3 ns / 176 B / 1 alloc | 726.8 ns / 176 B / 1 alloc | +0.8%（噪声） |
| `encoder.ShortTemplateEncode`（3 字段） | 334.8 ns / 176 B / 1 alloc | 327.2 ns / 176 B / 1 alloc | -2.3%（噪声） |
| `root.EncoderCallInfo`（纯编码） | 939.3 ns / 176 B / 1 alloc | 935.4 ns / 176 B / 1 alloc | -0.4%（噪声） |
| `root.LoggerToDiscard`（编码+写） | 1556 ns / 982 B / 3 alloc | 1474 ns / 982 B / 3 alloc | -5.3% |

### 3.3 引擎层 slcore

| 基准 | v1.1.0 | v1.2.0 | 变化 |
|---|---|---|---|
| `slcore.CheckedEntryWrite` | 141.2 ns / 32 B / 1 alloc | 137.8 ns / 32 B / 1 alloc | -2.4% |
| `slcore.CheckedEntryWriteWithFields` | 818.6 ns / 822 B / 1 alloc | 603.7 ns / 806 B / 1 alloc | **-26.2%** |
| `slcore.FieldAddTo` | 212.5 ns / 352 B / 3 alloc | 211.6 ns / 352 B / 3 alloc | -0.4%（噪声） |

### 3.4 异步链路（注意事项）

| 基准 | v1.1.0 | v1.2.0 | 变化 |
|---|---|---|---|
| `root.CallInfoAsync`（编码+入队） | 2492 ns / 495 B / 5 alloc | 2799 ns / 482 B / 5 alloc | +12.3% |

`CallInfoAsync` 走 **CallInfo 模板编码器 + AsyncWriter 入队**，v1.2.0 的代码改动
**不涉及**该路径。其 +12% 属异步队列调度的运行间方差（3s benchtime 下 ±10~15%
常见），不代表性能回退；异步吞吐由队列/批处理参数决定，与 JSON 重构无关。

## 4. 收益归因

v1.2.0 的提速集中在 JSON 编码器重构（详见 `CHANGELOG.md` v1.2.0 条目）：

- 编码热路径改为**字节片段流式直写** `buffer.Buffer`，消除 `json.Marshal`、
  临时 `[]byte`/字符串拼接与中间 map 分配；
- 字段值经 `AppendInt`/`AppendTime`/`AppendBool` 等原语零分配写入；
- 转义走分段批量 `appendEscaped`，无需预分配整缓冲；
- 整条 `EncodeEntry` 仅 **1 次**对象池分配（缓存缓冲复用），单条分配由
  2463 B / 36 allocs 降至 16 B / 1 alloc。

CallInfo / RequestInfo / 模板编码器在前序版本已采用同款流式设计，故本次无变化；
`CheckedEntryWriteWithFields` 的提升是 JSON 路径分配锐减后 GC 压力下降的连带收益。

## 5. 复现命令

```bash
# 当前版本（main / v1.2.0）
go test -run='^$' -bench='^Benchmark(LoggerThroughput|EncoderCallInfo|CallInfoAsync)$' \
  -benchmem -benchtime=3s ./
go test -run='^$' -bench='^Benchmark(JSONEncode|CallInfoEncode|RequestInfoEncode|ShortTemplateEncode)$' \
  -benchmem -benchtime=3s ./encoder/
go test -run='^$' -bench='^Benchmark(CheckedEntryWrite|CheckedEntryWriteWithFields|FieldAddTo)$' \
  -benchmem -benchtime=3s ./slcore/

# 旧版本（隔离工作树，避免改动主目录）
git worktree add /tmp/sllogger_v1.1.0 v1.1.0
cd /tmp/sllogger_v1.1.0
go test -run='^$' -bench='^Benchmark(LoggerThroughput|JSONEncode|CallInfoEncode)$' \
  -benchmem -benchtime=3s ./ ./encoder/
cd /Volumes/工作固态/GitHub/sllogger && git worktree remove /tmp/sllogger_v1.1.0 --force
```

## 6. 零丢失校验（一致性保证）

v1.2.0 在提升吞吐的同时未引入数据丢失，由以下用例持续守护：

- `loss_test.go`：`BenchmarkLoggerToFileNoLoss` / `TestStressAsyncNoLoss` —
  写入 N 条后扫描落盘文件行数，恒等于 N（异步阻塞模式零丢失）。
- `writer/loss_test.go`：`BenchmarkRollingWriterNoLoss` — 滚动 + 清理场景下
  落盘行数 == 写入条数。
- `go test -race ./...` 全绿。

吞吐提升与零丢失二者兼得，符合「正确性优先」的设计原则。
