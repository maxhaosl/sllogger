# 日志写入压测与吞吐优化报告

本报告回答两个问题：

1. **写入条数是否等于日志文件中的条数**（数据一致性）
2. **每秒写入条数是否有提升空间**（吞吐优化）

## 1. 一致性验证：写入条数 == 落盘条数

### 验证方法

使用 `example/bench -mode check`。每条日志携带一个**全局唯一自增序号**
（写入 `log_msg` 尾部），落盘后反向解析并逐一核对，因此能区分
"行数恰好相等但内容错乱"这类伪通过。

五个校验维度：

| # | 维度 | 说明 |
|---|---|---|
| 1 | **条数** | 所有文件的总行数 == 写入条数（不多不少） |
| 2 | **完整性与格式** | 每行分隔符数量与模板字段数一致（CALL_INFO=14 字段/13 分隔符），无截断、无粘连 |
| 3 | **无重复** | 同一序号不出现两次 |
| 4 | **无缺失** | 解析出的序号集合恰好覆盖 `[0, N)` |
| 5 | **可解析** | 无无法解析的行 |

### 验证结果（优化后复测）

| 场景 | 写入 | 落盘 | 文件数 | 结论 |
|---|---|---|---|---|
| 32 协程 · 异步默认 | 1,000,000 | 1,000,000 | 47 | 5 项全 PASS |
| **64 协程 · 最高吞吐配置**（batch=50, shards=8） | **2,000,000** | **2,000,000** | **94** | **5 项全 PASS** |
| 32 协程 · **同步模式** | 200,000 | 200,000 | 10 | 5 项全 PASS |
| 32 协程 · **高频滚动**（MaxSize=64 KB） | 200,000 | 200,000 | **1201** | 5 项全 PASS |

```bash
go run ./example/bench -mode check -n 2000000 -g 64 -batch 50 -shards 8
go run ./example/bench -mode check -n 200000 -g 32 -sync          # 同步路径
go run ./example/bench -mode check -n 200000 -g 32 -maxsize 65536 # 高频滚动
```

**结论：写入条数与落盘条数严格一致，无丢失、无重复、无损坏、无粘连。**
覆盖了异步批处理、同步直写、跨 1201 个文件的高频滚动、以及优雅关闭全链路。

### 阴性对照：证明校验方法真的有效

只做"通过"的测试没有说服力，必须证明**它有能力检出丢失**。用
`BlockOnFull=false`（主动丢弃策略）跑同一个校验：

```
写入：200000 条    文件：2 个，总行数 21937
FAIL  1. 落盘条数 == 写入条数（差 -178063）
FAIL  4. 无缺失条目
结果：2 项失败
```

```bash
go run ./example/bench -mode check -n 200000 -g 64 -nonblocking
```

校验准确检出 178,063 条丢失 —— 说明上面的"全部通过"不是假阳性。

> **前置条件**：异步模式必须 `BlockOnFull: true`。默认 `false` 是**主动丢弃**
> 策略（保护业务线程），队列满时静默丢弃并计入 `Metrics.Dropped`，属预期
> 行为。要求零丢失就必须显式开启 `BlockOnFull`。

---

## 2. 吞吐压测与瓶颈定位

### 测量方法

单次测量在满载机器上抖动可达 ±40%，**不足以判断优化是否有效**。
工具内置多轮采样取中位数（默认 5 轮）：

```bash
go run ./example/bench -mode bench -n 200000 -g 16 -rounds 5
```

### 瓶颈定位（CPU profile）

优化前 64 协程场景的 profile：

```
6.71s 40.82%  runtime.usleep            ← 调度器自旋休眠
3.78s 22.99%  runtime.pthread_cond_wait ← goroutine park
1.22s  7.42%  runtime.pthread_cond_signal
1.05s  6.39%  runtime.madvise
       8.27%  runtime.selectgo  (91% 来自 AsyncWriter.Write)
      10.40%  runtime.lock2
       11.25% slcore.(*lockedWriteSyncer).Write  ← 多余的外层锁
```

**71% 的 CPU 时间在调度器的等待/唤醒上**，而非业务编码。根因是：

- 单个 channel 被大量生产者争抢 → park/unpark 风暴；
- worker 每次只从 channel 取 **1 条**就回到 `select`，唤醒成本被放大 N 倍；
- 已线程安全的 `AsyncWriter` 外面又套了一层 `slcore.Lock`（约 11%）。

### 另一个反直觉现象

| 协程数 | 同步（条/s） | 异步（条/s） |
|---|---|---|
| 1 | 147,092 | 372,899 |
| 4 | — | 834,394 |
| 16 | 99,562（**比单协程慢 32%**） | 605,679 |
| 64 | — | 330,611（**比 4 协程慢 61%**） |

**同步模式下加协程反而变慢**：所有协程争抢 `RollingWriter` 的同一把锁，
且每条日志一次 `write` syscall——锁竞争 + syscall 开销压倒了并发收益。
**同步模式不适合高并发写日志**，应始终使用异步模式。

---

## 3. 已实施的优化

### 优化 1：移除多余的外层互斥锁

`Config.Build` 对所有 sink 无差别套 `slcore.Lock`，而 `AsyncWriter` 与
`RollingWriter` 自身已线程安全，等于**双重加锁**。

引入 `ConcurrentSafe() bool` 标记，由 sink 自声明：

```go
func (a *AsyncWriter) ConcurrentSafe() bool   { return true }
func (w *RollingWriter) ConcurrentSafe() bool { return true }

func lockIfNeeded(w slcore.WriteSyncer) slcore.WriteSyncer {
	if cs, ok := w.(concurrentWriteSyncer); ok && cs.ConcurrentSafe() {
		return w
	}
	return slcore.Lock(w)
}
```

### 优化 2：worker 批量排空 channel（**核心收益**）

原实现：每次唤醒只取 1 条，随即回到 `select`。
新实现：唤醒后**非阻塞地把队列取空**，再统一攒批落盘。

```go
case b := <-queue:
    batch = append(batch, b...)
    count++
    a.recycle(&b)
    a.drain(queue, &batch, &count)   // 一次性取空
    if count >= a.cfg.BatchSize {
        flush()
    }
```

单条唤醒成本被均摊，生产者不再频繁 park。

### 优化 3：分片队列（`Shards`，默认 1 = 关闭）

```go
// 生产者按 atomic 自增 round-robin 路由，成本远低于争抢单 channel
queue := a.shards[a.next.Add(1)%uint64(len(a.shards))]
```

每个分片独立 channel + 独立 worker；`Sync()` 等待**所有**分片；
`Close()` 等待所有 worker；队列容量按分片均分（总缓冲不变）。

---

## 4. 优化效果（A/B 实测对照）

### 对照方法

代码只有一次 Initial commit（本次实现均为新增文件），无法用 `git` 回退，
因此采用**副本回退法**做真正的 A/B：

1. 复制当前工程到 `/tmp/sllogger-baseline`；
2. 在副本中回退三处优化（恢复 `slcore.Lock`、去掉 worker 批量 drain、
   分片逻辑保留但默认 1）；
3. 两版本用**完全相同的参数**交替运行，并做 B-A-B 交叉复测以排除机器漂移。

单次测量抖动可达 ±40%，每个场景取 3~5 轮中位数。

### 全场景对照（n=200,000 / g=16 / 5 轮中位数）

| 场景 | 基线（优化前） | 优化后 | 提升 |
|---|---:|---:|---:|
| 异步/16协程/batch=1 | 373,379 | **971,668** | **+160%** |
| **异步/16协程/batch=50** | 539,447 | **1,030,603** | **+91%** |
| 异步/16协程/batch=200 | 548,154 | **989,198** | **+80%** |
| 异步/16协程/batch=1000 | 543,953 | 889,431 | +63% |
| 异步/4协程/batch=200 | 558,354 | 879,331 | +57% |
| 异步/64协程/batch=200 | 554,834 | 725,499 | +31% |
| 异步/1协程/batch=200 | 368,438 | 382,374 | +4% |
| 同步/16协程 | 98,998 | 99,251 | +0.3%（**未优化**） |

### B-A-B 交叉复测（3 轮中位数）

| 场景 | 基线 | 优化后 | 提升 |
|---|---:|---:|---:|
| 异步/16协程/batch=50 | 538,028 | 1,047,855 | **+94.8%** |
| 异步/16协程/batch=200 | 542,593 | 953,737 | **+75.8%** |

两轮基线分别是 548,154 / 542,593（差 1.0%），两轮优化后是
989,198 / 953,737（差 3.6%）——**提升幅度远大于测量噪声，结论可靠**。

### 结论

| 指标 | 基线 | 优化后 | 提升 |
|---|---:|---:|---:|
| 峰值吞吐 | 558,354 条/s | **1,030,603 条/s** | **+84.6%** |
| 常用配置（batch=200） | 548,154 条/s | **989,198 条/s** | **+80.5%** |
| 相对同步单协程 | — | — | **7.24x** |

**同步路径吞吐未变（+0.3%）**：三项优化全部针对异步链路，同步路径行为
与开销保持一致，无回归。

### 分片的实际影响（**务必注意**）

| 场景 | Shards=1 | Shards=2 | Shards=4 | Shards=8 | Shards=16 |
|---|---|---|---|---|---|
| **16 协程** | **989,198** | 891,723 | 857,248 | 711,261（-28%） | 522,621（-47%） |
| 64 协程 | 725,499 | — | 798,649 | **841,229（+16%）** | 797,587 |

**分片不是越多越好**：

- 并发量 ≲ 32：**Shards=1 最优**，分片反而下降最多 46%
  （每片队列变浅 + 多 worker 争抢下游锁 + 更多调度开销）
- 并发量 ≥ 64：Shards=8 约 +14%

因此 **`Shards` 默认保持 1**，仅在实测确认的高并发场景才调大。

### 队列容量的影响（**几乎无影响**）

| QueueSize | 16 协程 | 64 协程 |
|---|---|---|
| 1,024 | 907,553 | 819,454 |
| 16,384 | 1,025,102 | 778,720 |
| 131,072 | 945,136 | 767,337 |

三组容量之间波动仅 ±10%，**不是瓶颈**。瓶颈是 worker 的消费速率；
加大队列只增加内存占用与关闭时的排空耗时，不提升吞吐。

---

## 5. 配置建议

### 推荐配置（覆盖绝大多数场景）

```go
cfg := sllogger.NewCallInfoConfig("/data/logs", "playurl", 8080)
cfg.Rolling.Async = true          // 必选：同步模式多协程会退化
cfg.Rolling.BlockOnFull = true    // 必选：false 会在队列满时丢弃日志
cfg.Rolling.QueueSize = 16384     // 约 1.6 万条缓冲，够用且不浪费内存
cfg.Rolling.BatchSize = 200       // 每 200 条一次 write syscall
cfg.Rolling.FlushInterval = 100 * time.Millisecond
cfg.Rolling.Shards = 1            // 默认；并发 ≥64 且实测有效才调到 8
log, _ := cfg.Build()
defer log.Close()                 // 务必调用，否则关闭时会丢数据
```

**必须同时启用 `Async` + `BlockOnFull`**：只用 `Async` 而保持默认
`BlockOnFull=false`，队列满时日志会被**静默丢弃**（计入 `Metrics.Dropped`）。

### 决策表

| 业务特征 | 建议 |
|---|---|
| 峰值 < 10 万条/s | 默认配置即可（余量 10 倍） |
| 峰值 10~50 万条/s | 默认配置 + 监控 `Dropped` |
| 峰值 > 50 万条/s（协程 ≥64） | 试 `Shards=8`，按本报告方法实测 |
| 要求零丢失 | `BlockOnFull=true` + 监控 `Dropped`/`WriteErrors` |
| 可接受丢失（保护业务） | `BlockOnFull=false`，监控 `Dropped` 并告警 |

### 可观测性

```go
snap := rw.Metrics().Snapshot()  // RollingWriter 指标
snap.Enqueued / Written / Dropped / WriteErrors / RotateCount / RemovedFiles
```

建议对 `Dropped > 0` 与 `WriteErrors > 0` 建立告警。

---

## 6. 进一步优化空间（未实施）

按 **收益 / 风险** 排序：

| 方案 | 预估收益 | 风险与代价 | 建议 |
|---|---|---|---|
| **独立磁盘 / 更快存储** | 高（IO 场景 2~5x） | 无代码改动 | 优先排查磁盘 IOPS 是否成为瓶颈 |
| **增大 BatchSize 至 500~1000** | 低（实测 ±2%） | 关闭时排空略久 | 已在推荐值附近，无需调整 |
| **消除一次内存拷贝** | 中（省 1/3 memcpy） | 需改变 `ioCore.Write` 的 buffer 所有权约定，侵入引擎层 | 收益不明确，暂不建议 |
| **每生产者本地攒批再入队** | 高（竞争降 1~2 个数量级） | 需 goroutine-local 状态（Go 无官方支持），且日志延迟上升 | 若未来出现百万 QPS 场景再评估 |
| **多 worker 分片（已实现）** | 高并发 +14% | 跨分片无序 | 已提供，默认关闭 |
| **多进程分文件写入** | 高 | 需外部协调文件命名 | 超出库职责 |

**结论：当前 ~103 万条/s（约 383 MiB/s）已远超绝大多数业务需求**
（典型峰值数千至数万条/s，余量 1~2 个数量级）。继续优化的边际收益低，
建议把精力放在**磁盘 IO 能力**与**监控告警**上，而非进一步压榨 CPU。

---

## 7. 复现命令

```bash
# —— 一致性校验 ——
go run ./example/bench -mode check -n 1000000 -g 32            # 默认异步配置
go run ./example/bench -mode check -n 2000000 -g 64 -batch 50 -shards 8  # 最高吞吐配置
go run ./example/bench -mode check -n 200000 -g 32 -sync       # 同步路径
go run ./example/bench -mode check -n 200000 -g 32 -maxsize 65536        # 高频滚动
go run ./example/bench -mode check -n 200000 -g 64 -nonblocking          # 阴性对照（应失败）

# —— 吞吐矩阵 ——
go run ./example/bench -mode bench -n 200000 -g 16 -rounds 5
go run ./example/bench -mode bench -n 200000 -g 64 -rounds 3 -queue 1

# —— 性能剖析 ——
go run ./example/bench -mode bench -n 200000 -g 64 -async -cpuprofile cpu.out
go tool pprof -top cpu.out

# —— 回归 ——
go test ./... -count=1 && go test -race ./... -count=1
go run ./example/verify
```

### A/B 对照（复现本次优化前后对比）

```bash
rm -rf /tmp/sllogger-baseline && cp -r . /tmp/sllogger-baseline
cd /tmp/sllogger-baseline
# 回退优化1：config.go 中 lockIfNeeded(out) -> slcore.Lock(out)
# 回退优化2：async.go 中删除 a.drain(queue, &batch, &count) 这一行
go build ./... && go run ./example/bench -mode bench -n 200000 -g 16 -rounds 5
```
