# sllogger — 构建与验证入口
#
# 常用命令：
#   make            # 等同于 make all（格式化检查 + 构建 + vet + 单元测试）
#   make test       # 单元测试
#   make race       # 竞态检测
#   make cover      # 覆盖率报告（终端 + HTML）
#   make bench      # 全模块基准
#   make stress     # 压力测试（较慢）
#   make verify     # 功能自检（逐条验证 9 项需求）
#   make check      # 端到端一致性校验：写入条数 vs 落盘条数
#   make ci         # CI 全量流水线（本机复现 GitHub Actions）
#   make clean      # 清理构建与覆盖率产物

.PHONY: all fmt fmt-check build vet test race cover cover-html \
        bench stress verify check profile ci clean help

# 可通过环境变量覆盖，例如：
#   make check N=1000000 G=64
#   make bench N=200000 G=16 ROUNDS=5
N      ?= 300000
G      ?= 32
ROUNDS ?= 5

COVERPROFILE ?= coverage.out

# 库包（不含 example/*）。
#
# example 下的 4 个程序是 main 包（可执行演示），没有单元测试，
# 由 `make verify` / `make check` 做端到端验证。若把它们计入覆盖率，
# 整体数字会被它们的 0% 拉低（各库包 98~100%，汇总却显示 71.9%）。
# 注意两种写法的区别：
#   LIB_PKGS       空格分隔，作为 `go test` 的包参数
#   COVER_PKGS     逗号分隔，作为 -coverpkg 的值
# -coverpkg 只接受逗号分隔的单一参数；若用空格分隔，后面的路径会被
# 当成包参数，导致覆盖率统计范围错误（表现为 "0.0% of statements"）。
LIB_PKGS   := ./ ./buffer/... ./encoder/... ./internal/... ./slcore/... ./writer/...
COVER_PKGS := ./,./buffer/...,./encoder/...,./internal/...,./slcore/...,./writer/...

all: fmt-check build vet test

help:
	@echo "sllogger 构建与验证入口"
	@echo ""
	@echo "  make           格式化检查 + 构建 + vet + 单元测试"
	@echo "  make test      单元测试"
	@echo "  make race      竞态检测"
	@echo "  make cover     覆盖率报告"
	@echo "  make bench     全模块基准"
	@echo "  make stress    压力测试（较慢）"
	@echo "  make verify    功能自检（9 项需求逐条验证）"
	@echo "  make check     一致性校验：写入条数 vs 落盘条数"
	@echo "  make ci        CI 全量流水线"
	@echo "  make clean     清理产物"
	@echo ""
	@echo "可调参数（环境变量）：N（条数）G（协程数）ROUNDS（轮数）"
	@echo "示例：make check N=1000000 G=64"

## 格式化 ###############################################################

fmt:
	@echo ">> 格式化代码"
	@gofmt -w .

fmt-check:
	@echo ">> 检查代码格式"
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "以下文件未格式化："; echo "$$unformatted"; \
		echo "请运行 make fmt"; \
		exit 1; \
	fi
	@echo "格式检查通过"

## 构建与静态检查 #######################################################

build:
	@echo ">> 构建"
	@go build ./...

vet:
	@echo ">> go vet"
	@go vet ./...

lint: vet
	@echo ">> 静态分析"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	elif command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "未安装 golangci-lint / staticcheck，跳过（已执行 go vet）"; \
	fi

## 测试 #################################################################

test:
	@echo ">> 单元测试"
	@go test ./... -count=1

# 快速测试：跳过压力测试
test-short:
	@echo ">> 单元测试（跳过压测）"
	@go test ./... -short -count=1

race:
	@echo ">> 竞态检测"
	@go test -race ./... -count=1

# 注意两点，否则汇总数字会失真：
#  1. 必须用 -coverpkg 指定全部库包。不加时每个包的测试只统计该包自身
#     的语句，汇总后会被严重低估。
#  2. 必须排除 example/*（main 包、无单元测试），否则它们的 0% 会
#     拉低整体数字。
cover:
	@echo ">> 覆盖率（库代码）"
	@go test $(LIB_PKGS) -covermode=atomic -coverpkg=$(COVER_PKGS) \
		-coverprofile=$(COVERPROFILE) -count=1
	@echo ""
	@echo "各包覆盖率："
	@go test $(LIB_PKGS) -cover -count=1 | sed 's/^/  /'
	@echo ""
	@echo "整体覆盖率："
	@go tool cover -func=$(COVERPROFILE) | tail -1 | sed 's/^/  /'
	@echo "详细报告：make cover-html"

cover-html: cover
	@go tool cover -html=$(COVERPROFILE) -o coverage.html
	@echo "已生成 coverage.html"

bench:
	@echo ">> 全模块基准"
	@go test -bench . -benchtime 50000x -run '^$$' ./...

## 压测与端到端校验 #####################################################

stress:
	@echo ">> 压力测试（N=$(N) G=$(G)）"
	@go test -run TestStress -v -timeout 900s ./ -count=1

verify:
	@echo ">> 功能自检"
	@go run ./example/verify

check:
	@echo ">> 一致性校验：写入 $(N) 条 / $(G) 协程"
	@go run ./example/bench -mode check -n $(N) -g $(G)

# 阴性对照：验证校验方法能检出丢失（预期失败并输出丢失条数）
check-negative:
	@echo ">> 阴性对照（应检出丢失，用于证明校验有效）"
	@go run ./example/bench -mode check -n 200000 -g 64 -nonblocking || true

throughput:
	@echo ">> 吞吐矩阵（N=$(N) G=$(G) ROUNDS=$(ROUNDS)）"
	@go run ./example/bench -mode bench -n $(N) -g $(G) -rounds $(ROUNDS)

profile:
	@echo ">> 性能剖析（生成 cpu.out / mem.out）"
	@go run ./example/bench -mode bench -n 200000 -g 64 -async \
		-cpuprofile cpu.out -memprofile mem.out
	@echo "查看：go tool pprof -top cpu.out"

## CI ###################################################################

ci: fmt-check build vet race cover verify check
	@echo ""
	@echo ">> CI 流水线全部通过"

## 清理 #################################################################

clean:
	@echo ">> 清理产物"
	@rm -f $(COVERPROFILE) coverage.html cpu.out mem.out
	@go clean -testcache 2>/dev/null || true
	@echo "已清理"
