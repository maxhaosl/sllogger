# Changelog

更新按 **更新日期 + 变更内容** 记录；版本号遵循语义化版本（SemVer）。

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
