# Changelog

更新按 **更新日期 + 变更内容** 记录；版本号遵循语义化版本（SemVer）。

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
