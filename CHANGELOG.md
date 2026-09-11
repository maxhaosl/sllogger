# Changelog

更新按 **更新日期 + 变更内容** 记录；版本号遵循语义化版本（SemVer）。

## v1.2.0 — 2026-09-12

缺陷修复、安全性与性能优化。

缺陷 / 安全修复：

- **[崩溃修复] 旋转失败时 writer 被打坏**：`RollingWriter.rotateLocked` 原先先关闭旧文件再把 `w.file` 置为 `nil`，再打开新文件；一旦新文件打开失败（如磁盘满、目录只读），`w.file` 会永久为 `nil`，之后每次 `Write` 对 nil `*os.File` 解引用直接 panic，把整个 logger 打挂。改为**先打开新文件成功后再关闭旧文件并原子替换**，失败时保留旧文件（仍可用），`Write` 返回错误而非 panic；并加 `ErrWriteUnavailable` 兜底（任何情况下 `w.file==nil` 也只报错不崩溃）。新增 `writer/rotation_fail_test.go` 回归测试。
- **[安全] 日志文件默认权限收紧为 `0o600`**：原先新建日志文件（`RollingWriter` 与 `Open` 路径）统一用 `0o644`，世界/组可读，敏感结构化日志存在泄露风险。新增 `FileMode` 配置（写入 `writer.Config.FileMode` 与根 `Config.FileMode`，向 `RollingWriter` 透传），默认 `0o600`（仅属主可读写）；需其他账号读取时显式设 `0o640`/`0o644`。`Open` 公共 API 保持 `0o644` 向后兼容，新增 `OpenWithMode` 供配置驱动路径按 `FileMode` 创建。
- **[健壮性] `Levels`/`ExcludeLevels` 全为无效名时静默放行所有级别**：`parseLevels` 遇到全拼写错误（如 `["infox","warnz"]`）会生成空 map，原逻辑 `len(allow)==0` 退化为"白名单为空=全部放行"，使级别路由被悄悄关闭。`buildOutputCore` 现在在 `Build` 阶段**失败返回错误**（fail-fast），避免误配置静默失效。新增 `TestBuildOutputInvalidLevels`/`TestBuildOutputInvalidExcludeLevels`，并确认 `trace`（映射 `DebugLevel`）仍被接受。

性能优化：

- **[性能] JSON 编码器改为零反射流式写入**：原 `JSONEncoder` 每一条日志都构造 `map[string]interface{}` 再经 `encoding/json` 反射序列化，单条日志分配 23~29 次、约 1.5KB。新版 `JSONEncoder` 直接流式写入复用的 buffer（与 `TemplateEncoder` 同思路，零第三方依赖），彻底去掉每行的 map 分配与反射。实测（`BenchmarkLoggerThroughput`，单条 JSON 日志）：
  - 吞吐 **2335 → 984 ns/op（约 2.4×）**，约 **42.8 万 → 102 万 条/秒**；
  - 分配 **23 → 4 次/op**（写入文件路径 29 → 7 次/op）；
  - 多输出 / 模板等场景同步提升。With 上下文在 `With` 时序列化一次并缓存，热路径不再重复反射。

可观测性 / 正确性验证：

- 新增日志丢失校验（压测 + 一致性）：`writer/loss_test.go`（同步 / 异步多协程）与根包 `loss_test.go`（整链路 Logger→JSON→异步 RollingWriter）写入 **100 万条**并逐行解析校验，断言落盘行数 == 写入条数、`Dropped == 0`、且每行均为合法 JSON。
  - 实测 writer 层异步：**100 万条，0 丢失，约 75 万条/秒**；
  - 整链路：**100 万条，0 丢失，约 38 万条/秒**。
  - 压测基准 `BenchmarkRollingWriterNoLoss` / `BenchmarkLoggerToFileNoLoss` 同时充当压力测试与丢失校验，失败即 `b.Fatalf`。

## v1.1.0 — 2026-09-12

新增多输出能力与按级别分流，并对多输出热路径做了零分配优化。

新增功能：

- **多输出 `Config.Outputs []Output`**：单个 logger 扇出到多个独立输出，每个输出拥有各自的编码（`Encoding` / `TemplateConfig`）、滚动文件（`Rolling`）或路径（`OutputPaths`）。`Outputs` 非空时优先生效，覆盖单输出字段。
- **按级别分流到不同文件**：每个 `Output` 支持 `Levels`（白名单）、`ExcludeLevels`（黑名单）、`Level`（最低级别）三类路由，且先经过全局 `Config.Level` 闸门。典型场景：
  - ERROR / WARN 单独写错误文件：`Levels: ["error","warn"]`；
  - 上线只输出 ERROR / WARN：只配置该 Output（不配置 INFO / DEBUG / TRACE 的 Output）即可；
  - 丢弃某些级别：`ExcludeLevels: ["info","debug","trace"]`。
  - 约定：`trace` 不在 zap 级别阶梯内，解析时映射为 `DebugLevel`。
- **每文件不同格式**：每个 `Output` 用各自 `Encoding` / `TemplateConfig`；`^` 分隔的自定义埋点模板示例：`TemplateConfig{Separator:"^", Template:"log_level^date^dataType^operatorId^serviceId^useTime^result"}`，自定义字段名走动态查找（调用侧 `sllogger.String("字段名", ...)` 注入）。
- **示例 `example/multitype`**：演示 feign（JSON + ERROR/WARN 分流）与 mgmonitor（`^` 模板）两类日志，`-mode erroronly` 演示只输出 ERROR/WARN；`make example-multitype` 构建。

性能优化：

- **多输出级别路由零分配**：原先每条日志都会对每个带 `Levels`/`ExcludeLevels` 的 Output 现场 `make(map)` 解析级别，压测中三输出场景每写一条分配约 26 次 map（57 allocs/op）。改为在 `Build` 时预编译路由（`outputLevelEnabler`，持有全局 `AtomicLevel` + 预解析的 `allow`/`deny` map + 最低级别），热路径 `Enabled` 仅做 map 查询，零分配（三输出场景 57 → 31 allocs/op，ns/op 降约 15%）。单输出（向后兼容）路径未触碰，`Level` 判断方式不变。

修复：

- **按小时命名文件清理/续写失效**：`writer/naming.go` 的 `parseFileName` 原按 `.` 切分并假定日期为单段，导致 `DateLayout="2006-01-02.15"`（生成 `2026-09-12.01`）解析失败，清理扫描与启动续写完全识别不了按小时文件，实际永不清理。改为按 `DateLayout` 逐前缀 `time.Parse` 定位日期边界（新增 `writer/hourly_test.go` 含跨类型隔离清理断言）。注：之前的 calllog 示例用连字符布局 `2006-01-02-15`（`2026-09-11-15`，无点）未暴露此问题。

## v1.0.0 — 2026-09-12

首个公开发布版本，已作为 Go 公共模块 `github.com/maxhaosl/sllogger` 发布（可被 `go get` 直接引用）。

实现功能：

- **结构化、高吞吐日志库**：`Logger` 基于 `slcore.Core`，提供层级日志、采样、可插拔 Sink / Encoder。
- **模板编码（content-template encoder）**：以 `|` 分隔的字段模板落盘；内置 CALL_INFO / RequestInfo 预置模板（`encoder.CallInfoTemplate` / `encoder.RequestInfoTemplate`），也支持完全自定义模板（`Encoding="template"`）。
- **字段按 key 动态解析**：字段从 `With` / 调用点 `Field` 中查找；未注入回退 `null`（可配 `NullValue`）；支持 `RegisterRenderer` 注册自定义渲染器。
- **三类调用日志 API**：
  - `CallInfo`：入参 + 返回报文 + 总耗时，每个请求一条（无论成功 / 失败）。
  - `RequestInfo`：请求 / 响应头、方法、限流值等出错详细日志。
  - 通用 `InfoCtx` / `WarnCtx` / `ErrorCtx`：业务逻辑普通日志。
- **trace / span 自动注入**：`WithTrace(ctx, traceID, spanID)`，调用 `*Ctx` 方法自动带上 `trace_id` / `span_id`（兼容 OpenTelemetry `SpanContext` 抽取）。
- **自研 RollingWriter**：支持按大小 / 小时 / 天滚动；命名占位符 `{base} {date} {service} {app} {seq} {pid}`；自动续写已有文件，并按 `MaxBackups` / `MaxTotalSize` / `MaxAge` 清理。
- **异步批量落盘**：多生产者单消费者模型；`LevelWriteSyncer` + `BlockLevel` 策略，ERROR 队列满时阻塞不丢、INFO / CALL_INFO 满时丢弃以保护业务。
- **字段转义**：自动转义 `| \n \r \`，保证下游按 `|` 切分可靠。
- **示例**：`example/calllog` 演示三类日志同时落盘、滚动命名与自定义字段（`log_type` / `service_Id`）注入。
- **文档**：`doc/go_call_info_logger_design.md` 设计文档、`README.md` 集成说明。

约定与备注：

- module 路径为 `github.com/maxhaosl/sllogger`，但各子包 Go 包名仍为 `sllogger`。
- 设计规范字段名为 `log_type` / `service_Id`（预置模板归一化为 `log_level` / `service_id`）；库中无常量时，按同名 key 注入 `Field` 即可使用。
