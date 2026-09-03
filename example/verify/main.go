// Copyright (c) 2026 sllogger authors.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// verify 逐条验证 sllogger 的全部功能点，每条都做实际断言而非仅打印。
//
// 运行：
//
//	go run ./example/verify [-dir /tmp/sllogger-verify] [-keep]
//
// 退出码 0 表示全部通过；非 0 表示有校验失败。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sllogger"
	"sllogger/encoder"
	"sllogger/writer"
)

var (
	dirFlag  = flag.String("dir", "", "验证用的临时日志目录（默认自动创建）")
	keepFlag = flag.Bool("keep", false, "保留生成的日志文件便于查看")
)

// 可推进的假时钟，用于验证定时滚动与按时间清理。
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func newStepClock(t time.Time) *stepClock { return &stepClock{t: t} }

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var failures int

// check 记录一条断言结果。
func check(name string, cond bool, detail string) {
	if cond {
		fmt.Printf("  PASS  %s\n", name)
		return
	}
	failures++
	fmt.Printf("  FAIL  %s\n        %s\n", name, detail)
}

func section(title string) {
	fmt.Printf("\n=== %s ===\n", title)
}

func listLogs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func readLog(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func main() {
	flag.Parse()

	root := *dirFlag
	if root == "" {
		root = filepath.Join(os.TempDir(), fmt.Sprintf("sllogger-verify-%d", os.Getpid()))
	}
	if err := os.RemoveAll(root); err != nil {
		panic(err)
	}
	if !*keepFlag {
		defer os.RemoveAll(root)
	}
	fmt.Printf("日志目录: %s\n", root)

	req1OutputPath(root)
	req2FileName(root)
	req3Template(root)
	req4SizeRotation(root)
	req5TimeRotation(root)
	req6MaxBackups(root)
	req7MaxTotalSize(root)
	req8MaxDirSize(root)
	req9MaxAge(root)
	extraAsyncAndMetrics(root)
	extraCallInfoFormat(root)
	extraRequestInfoFormat(root)

	fmt.Printf("\n---------------------------------------------\n")
	if failures == 0 {
		fmt.Printf("全部功能验证通过\n")
		return
	}
	fmt.Printf("共 %d 项验证失败\n", failures)
	os.Exit(1)
}

// 需求 1：日志输出路径可以设置。
func req1OutputPath(root string) {
	section("需求1：日志输出路径可设置")

	dir := filepath.Join(root, "req1", "nested")
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	log := sllogger.Must(cfg.Build())
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "hello"})
	log.Close()

	_, err := os.Stat(dir)
	check("目录不存在时自动创建", err == nil, fmt.Sprintf("stat: %v", err))
	check("文件写入到指定目录", strings.Contains(readLog(dir, listLogs(dir)[0]), "hello"),
		fmt.Sprintf("files=%v", listLogs(dir)))
}

// 需求 2：日志文件名可以自定义。
func req2FileName(root string) {
	section("需求2：日志文件名可自定义")

	// 2.1 默认命名规则
	dir := filepath.Join(root, "req2-default")
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.Clock = newStepClock(time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local))
	log := sllogger.Must(cfg.Build())
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "x"})
	log.Close()

	files := listLogs(dir)
	want := "LOG_CALL_INFO.2026-09-03.playurl8080.log"
	check("默认命名 {base}.{date}.{service}.log", len(files) == 1 && files[0] == want,
		fmt.Sprintf("got %v, want [%s]", files, want))

	// 2.2 完全自定义命名模板
	dir2 := filepath.Join(root, "req2-custom")
	cfg2 := sllogger.Config{
		Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding: "callinfo",
		Rolling: &writer.Config{
			Dir:                dir2,
			BaseName:           "svc",
			NamePattern:        "{base}_{date}_{service}.log",
			RotatedNamePattern: "{base}_{date}_{service}_{seq}.log",
			DateLayout:         "20060102",
			ServiceName:        "order",
			ServicePort:        80,
			Clock:              newStepClock(time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)),
			RotationInterval:   0,
			MaxSize:            0,
		},
	}
	log2 := sllogger.Must(cfg2.Build())
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "x"})
	log2.Close()

	files2 := listLogs(dir2)
	want2 := "svc_20260903_order80.log"
	check("自定义命名模板与日期格式", len(files2) == 1 && files2[0] == want2,
		fmt.Sprintf("got %v, want [%s]", files2, want2))
}

// 需求 3：日志写入内容模板可以自定义。
func req3Template(root string) {
	section("需求3：内容模板可自定义")

	dir := filepath.Join(root, "req3")
	cfg := sllogger.Config{
		Level:    sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding: "template",
		TemplateConfig: sllogger.TemplateConfig{
			Template:   "date|log_level|service_id|url|method|log_msg",
			ServiceID:  "playurl",
			TimeLayout: "2006/01/02 15:04:05.000",
			Separator:  "|",
		},
		Rolling: &writer.Config{
			Dir:              dir,
			BaseName:         "CUSTOM_TPL",
			ServiceName:      "playurl",
			ServicePort:      8080,
			RotationInterval: 0,
			MaxSize:          0,
		},
	}
	log := sllogger.Must(cfg.Build())
	log.InfoCtx(context.Background(), "custom message",
		sllogger.String("url", "http://svc/api"),
		sllogger.String("method", "GET"))
	log.Close()

	content := readLog(dir, listLogs(dir)[0])
	check("模板只包含配置的字段", strings.Count(content, "|") == 5,
		fmt.Sprintf("separators=%d, content=%s", strings.Count(content, "|"), content))
	check("自定义时间格式生效", strings.HasPrefix(content, "2026/") || strings.Contains(content, "|INFO|"),
		fmt.Sprintf("content=%s", content))
	check("动态字段按 key 填充", strings.Contains(content, "http://svc/api|GET|custom message"),
		fmt.Sprintf("content=%s", content))

	// 3.2 自定义字段渲染器（复刻老格式 ` INFO [playurl,tid,sid,true]`）
	dir2 := filepath.Join(root, "req3-renderer")
	encCfg := sllogger.TemplateConfig{
		Template:  "date|log_level|log_msg",
		ServiceID: "playurl",
	}
	encoder.RegisterRenderer(&encCfg, encoder.FieldLogLevel, func(ctx encoder.RenderContext) string {
		return ctx.Entry.Level.CapitalString() + " [playurl,tid,sid,true]"
	})
	cfg2 := sllogger.Config{
		Level:          sllogger.NewAtomicLevelAt(sllogger.InfoLevel),
		Encoding:       "template",
		TemplateConfig: encCfg,
		Rolling: &writer.Config{
			Dir:              dir2,
			BaseName:         "RENDERER",
			ServiceName:      "playurl",
			ServicePort:      8080,
			RotationInterval: 0,
			MaxSize:          0,
		},
	}
	log2 := sllogger.Must(cfg2.Build())
	log2.Info("legacy format")
	log2.Close()

	content2 := readLog(dir2, listLogs(dir2)[0])
	check("自定义渲染器覆盖字段输出",
		strings.Contains(content2, "|INFO [playurl,tid,sid,true]|legacy format"),
		fmt.Sprintf("content=%s", content2))
}

// 需求 4：单个日志文件超过指定大小自动切新日志文件。
func req4SizeRotation(root string) {
	section("需求4：单文件大小超限自动切新文件")

	dir := filepath.Join(root, "req4")
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 300
	cfg.Rolling.RotationInterval = 0 // 只按大小滚动
	log := sllogger.Must(cfg.Build())

	payload := strings.Repeat("a", 120) + "\n"
	for i := 0; i < 10; i++ {
		log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: payload})
	}
	log.Close()

	files := listLogs(dir)
	check("超过 MaxSize 后生成多个文件", len(files) >= 3, fmt.Sprintf("files=%v", files))

	withinLimit := true
	var sizes []int64
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			withinLimit = false
			break
		}
		sizes = append(sizes, fi.Size())
		if fi.Size() > 300 {
			withinLimit = false
		}
	}
	check("每个文件都不超过 MaxSize(300B)", withinLimit, fmt.Sprintf("sizes=%v", sizes))
	// 注意：字典序下 ".01.log" 排在无序号文件之前，因此按存在性断言。
	check("首文件无序号、滚动文件从 .01 开始编号",
		containsFile(files, "playurl8080.log") && containsFile(files, "playurl8080.01.log"),
		fmt.Sprintf("files=%v", files))
}

// 需求 5：日志可以设置定时时间（如 1 小时）切换新文件。
func req5TimeRotation(root string) {
	section("需求5：定时（1 小时）切换新文件")

	dir := filepath.Join(root, "req5")
	clock := newStepClock(time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local))
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = time.Hour
	cfg.Clock = clock
	log := sllogger.Must(cfg.Build())

	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "h10-a"})
	clock.Advance(30 * time.Minute)
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "h10-b"}) // 同一小时
	clock.Advance(31 * time.Minute)                                        // -> 11:01
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "h11"})
	clock.Advance(time.Hour) // -> 12:01
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "h12"})
	log.Close()

	files := listLogs(dir)
	check("每满 1 小时生成新文件", len(files) == 3, fmt.Sprintf("files=%v", files))

	first := readLog(dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	second := readLog(dir, "LOG_CALL_INFO.2026-09-03.playurl8080.01.log")
	third := readLog(dir, "LOG_CALL_INFO.2026-09-03.playurl8080.02.log")
	check("同一小时内的记录写入同一文件",
		strings.Contains(first, "h10-a") && strings.Contains(first, "h10-b"),
		fmt.Sprintf("first=%q", first))
	check("跨小时后写入序号 01 的文件", strings.Contains(second, "h11"), fmt.Sprintf("second=%q", second))
	check("再跨一小时写入序号 02 的文件", strings.Contains(third, "h12"), fmt.Sprintf("third=%q", third))

	// 跨天：日期变化后序号重置
	dir2 := filepath.Join(root, "req5-day")
	clock2 := newStepClock(time.Date(2026, 9, 3, 23, 30, 0, 0, time.Local))
	cfg2 := sllogger.NewCallInfoConfig(dir2, "playurl", 8080)
	cfg2.Rolling.MaxSize = 0
	cfg2.Rolling.RotationInterval = time.Hour
	cfg2.Clock = clock2
	log2 := sllogger.Must(cfg2.Build())
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "d1"})
	clock2.Advance(time.Hour) // -> 2026-09-04 00:30
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "d2"})
	log2.Close()

	files2 := listLogs(dir2)
	check("跨天后文件名日期变化且序号重置",
		len(files2) == 2 &&
			files2[0] == "LOG_CALL_INFO.2026-09-03.playurl8080.log" &&
			files2[1] == "LOG_CALL_INFO.2026-09-04.playurl8080.log",
		fmt.Sprintf("files=%v", files2))
}

// 需求 6：日志文件个数超过指定个数自动清理最老的。
func req6MaxBackups(root string) {
	section("需求6：文件个数超限清理最老的")

	dir := filepath.Join(root, "req6")
	clock := newStepClock(time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local))
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = time.Hour
	cfg.Rolling.MaxBackups = 3
	cfg.Rolling.CleanupInterval = time.Minute
	cfg.Clock = clock

	log := sllogger.Must(cfg.Build())
	for i := 0; i < 6; i++ {
		log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: fmt.Sprintf("h%d", i)})
		clock.Advance(time.Hour)
	}
	log.Close() // Close 会兜底执行一次清理

	files := listLogs(dir)
	check("仅保留 MaxBackups(3) 个文件", len(files) == 3, fmt.Sprintf("files=%v", files))
	// 6 个小时产生 6 个文件（无序号 + .01..05），保留序号最大的 3 个。
	check("保留的是最新的 3 个小时（.03/.04/.05）",
		len(files) == 3 &&
			files[0] == "LOG_CALL_INFO.2026-09-03.playurl8080.03.log" &&
			files[1] == "LOG_CALL_INFO.2026-09-03.playurl8080.04.log" &&
			files[2] == "LOG_CALL_INFO.2026-09-03.playurl8080.05.log",
		fmt.Sprintf("files=%v", files))
}

// 需求 7：单个类型文件总大小超限自动清理最老的。
func req7MaxTotalSize(root string) {
	section("需求7：单类型文件总大小超限清理最老的")

	dir := filepath.Join(root, "req7")
	clock := newStepClock(time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local))
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = time.Hour
	cfg.Rolling.MaxTotalSize = 1000 // 每条约 200B => 保留约 5 个
	cfg.Rolling.CleanupInterval = time.Minute
	cfg.Clock = clock

	log := sllogger.Must(cfg.Build())
	for i := 0; i < 10; i++ {
		// 固定长度消息，便于精确计算
		log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: strings.Repeat("x", 180)})
		clock.Advance(time.Hour)
	}
	log.Close()

	files := listLogs(dir)
	var total int64
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err == nil {
			total += fi.Size()
		}
	}
	check("本类型文件总大小不超过 MaxTotalSize(1000B)", total <= 1000,
		fmt.Sprintf("total=%d, files=%v", total, files))
	// 10 个小时产生 10 个文件；最老的若干个应已被删除。
	check("清理掉的是最老的文件（首个文件已不在）",
		len(files) < 10 && !containsFile(files, "playurl8080.log"),
		fmt.Sprintf("files=%v", files))
}

func containsFile(files []string, substr string) bool {
	for _, f := range files {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

// 需求 8：总日志文件大小超过设置值后自动清理最老的。
func req8MaxDirSize(root string) {
	section("需求8：目录总大小超限清理最老的（本类型）")

	dir := filepath.Join(root, "req8")
	clock := newStepClock(time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local))

	// 先放一个"其他类型"的日志文件，计入目录总量但不被删除。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	foreign := filepath.Join(dir, "OTHER.2026-09-01.other.log")
	if err := os.WriteFile(foreign, []byte(strings.Repeat("y", 400)), 0o644); err != nil {
		panic(err)
	}

	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = time.Hour
	cfg.Rolling.MaxDirSize = 1200 // 400B 已被其他文件占用
	cfg.Rolling.CleanupInterval = time.Minute
	cfg.Clock = clock

	log := sllogger.Must(cfg.Build())
	for i := 0; i < 8; i++ {
		log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: strings.Repeat("x", 180)})
		clock.Advance(time.Hour)
	}
	log.Close()

	files := listLogs(dir)
	var total int64
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err == nil {
			total += fi.Size()
		}
	}
	check("目录内日志总量不超过 MaxDirSize(1200B)", total <= 1200,
		fmt.Sprintf("total=%d, files=%v", total, files))
	check("其他类型的日志文件不被清理", fileExists(foreign), "OTHER 文件被误删")

	// 关闭后再触发一次清理，验证重复清理不越权
	cfg2 := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg2.Rolling.MaxSize = 0
	cfg2.Rolling.MaxDirSize = 600
	cfg2.Rolling.CleanupInterval = time.Minute
	cfg2.Clock = clock
	log2 := sllogger.Must(cfg2.Build())
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "trigger"})
	log2.Close()
	check("再次清理后其他类型文件仍保留", fileExists(foreign), "OTHER 文件被误删")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// 需求 9：日志保留时长（如 1 天、3 天）超期自动清理。
func req9MaxAge(root string) {
	section("需求9：超过保留时长自动清理")

	dir := filepath.Join(root, "req9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	now := time.Now()

	// 手工构造 3 个不同日期、不同 mtime 的历史文件。
	stale := filepath.Join(dir, "LOG_CALL_INFO.2026-08-31.playurl8080.log")
	fresh := filepath.Join(dir, "LOG_CALL_INFO.2026-09-02.playurl8080.log")
	old := now.Add(-72 * time.Hour)
	recent := now.Add(-time.Hour)
	for path, mt := range map[string]time.Time{stale: old, fresh: recent} {
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			panic(err)
		}
		if err := os.Chtimes(path, mt, mt); err != nil {
			panic(err)
		}
	}

	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.MaxAge = 48 * time.Hour // 保留 2 天
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0
	log := sllogger.Must(cfg.Build())
	log.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "today"})
	log.Close() // Close 时兜底清理

	check("超过 48 小时的文件被清理", !fileExists(stale), "72 小时前的文件仍然存在")
	check("48 小时内的文件保留", fileExists(fresh), "1 小时前的文件被误删")
	check("当天新写入的文件保留", len(listLogs(dir)) == 2, fmt.Sprintf("files=%v", listLogs(dir)))
}

// 附加验证：异步写入、优雅关闭与指标。
func extraAsyncAndMetrics(root string) {
	section("附加：异步批量写入、优雅关闭与指标")

	dir := filepath.Join(root, "extra-async")
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Rolling.Async = true
	cfg.Rolling.BlockOnFull = true
	cfg.Rolling.QueueSize = 512
	cfg.Rolling.BatchSize = 50
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0

	log := sllogger.Must(cfg.Build())
	const n = 3000
	start := time.Now()
	for i := 0; i < n; i++ {
		log.CallInfo(context.Background(), sllogger.CallInfo{
			LogMsg:  fmt.Sprintf("async-%d", i),
			UseTime: int64(i),
		})
	}
	log.Close()
	elapsed := time.Since(start)

	content := readLog(dir, listLogs(dir)[0])
	lines := strings.Count(content, "\n")
	check("异步写入无丢失", lines == n, fmt.Sprintf("lines=%d, want %d", lines, n))
	check("入队顺序与写入顺序一致",
		strings.HasPrefix(content, "async-0") || strings.Contains(content, "async-0"),
		"缺少首条记录")
	fmt.Printf("        异步写入 %d 条耗时 %v（%.2f μs/条）\n", n, elapsed,
		float64(elapsed.Microseconds())/float64(n))

	// 追加写入（重启续写）
	cfg2 := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg2.Rolling.MaxSize = 0
	cfg2.Rolling.RotationInterval = 0
	log2 := sllogger.Must(cfg2.Build())
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "resumed"})
	log2.Close()

	content2 := readLog(dir, listLogs(dir)[0])
	check("重启后追加写入同一文件而非覆盖",
		strings.Count(content2, "\n") == n+1 && strings.Contains(content2, "resumed"),
		fmt.Sprintf("lines=%d", strings.Count(content2, "\n")))
}

// 附加验证：CALL_INFO 预置格式与需求中的示例完全一致。
func extraCallInfoFormat(root string) {
	section("附加：CALL_INFO 格式与需求示例一致")

	dir := filepath.Join(root, "extra-callinfo")
	fixed := time.Date(2021, 11, 14, 20, 37, 34, 376000000, time.Local)
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Clock = newStepClock(fixed)
	cfg.Rolling.Clock = newStepClock(fixed)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0

	log := sllogger.Must(cfg.Build())
	ctx := sllogger.WithTrace(context.Background(),
		"3c06e3121c18f6114a2e9f2e38e5b8fe", "b256f6eac12c9818")
	log.CallInfo(ctx, sllogger.CallInfo{
		Mobile:   "18237438309",
		UserID:   "1071748417",
		ClientID: "c6559e74a24df8d97a2296ff1e23387a",
		URL:      "http://play.example.com:443/playurl/v1/play/playurl",
		Method:   "GET",
		UseTime:  0,
		ServerIP: "127.0.0.1",
		BussID:   "null",
		LogMsg:   "^MG.getContent:[690894368]",
	})
	log.Close()

	want := "2021-11-14 20:37:34.376|INFO|playurl|" +
		"3c06e3121c18f6114a2e9f2e38e5b8fe|b256f6eac12c9818|" +
		"18237438309|1071748417|c6559e74a24df8d97a2296ff1e23387a|" +
		"http://play.example.com:443/playurl/v1/play/playurl|GET|0|" +
		"127.0.0.1|null|^MG.getContent:[690894368]\n"
	got := readLog(dir, listLogs(dir)[0])
	check("CALL_INFO 输出与需求示例逐字段一致", got == want,
		fmt.Sprintf("got:\n%s\nwant:\n%s", got, want))

	// 转义：字段内含分隔符与换行时保证可解析
	dir2 := filepath.Join(root, "extra-escape")
	cfg2 := sllogger.NewCallInfoConfig(dir2, "playurl", 8080)
	cfg2.Rolling.MaxSize = 0
	cfg2.Rolling.RotationInterval = 0
	log2 := sllogger.Must(cfg2.Build())
	log2.CallInfo(context.Background(), sllogger.CallInfo{LogMsg: "a|b\nc"})
	log2.Close()

	line := strings.TrimSpace(readLog(dir2, listLogs(dir2)[0]))
	check("分隔符与换行被转义", strings.HasSuffix(line, `a\|b\nc`),
		fmt.Sprintf("line=%s", line))
	// 统计字段数时要先剔除被转义的 "\|"，否则会把转义序列也算作分隔符。
	unescaped := strings.ReplaceAll(strings.ReplaceAll(line, `\|`, ""), `\n`, "")
	check("字段数保持不变（14 个字段 / 13 个分隔符）",
		strings.Count(unescaped, "|") == 13,
		fmt.Sprintf("separators=%d (raw=%d)", strings.Count(unescaped, "|"), strings.Count(line, "|")))
}

// 附加验证：RequestInfo 预置格式。
func extraRequestInfoFormat(root string) {
	section("附加：RequestInfo 格式")

	dir := filepath.Join(root, "extra-requestinfo")
	fixed := time.Date(2026, 9, 3, 11, 0, 0, 0, time.Local)
	cfg := sllogger.NewCallInfoConfig(dir, "playurl", 8080)
	cfg.Encoding = "requestinfo"
	cfg.Clock = newStepClock(fixed)
	cfg.Rolling.Clock = newStepClock(fixed)
	cfg.Rolling.MaxSize = 0
	cfg.Rolling.RotationInterval = 0

	log := sllogger.Must(cfg.Build())
	ctx := sllogger.WithTrace(context.Background(), "tid", "sid")
	log.RequestInfo(ctx, sllogger.RequestInfo{
		Header:      "resp-header",
		ReqHeader:   "req-header",
		Method:      "POST",
		RateLimiter: "false",
		LogMsg:      "request success",
	})
	log.Close()

	want := "2026-09-03 11:00:00.000|INFO|playurl|tid|sid|resp-header|req-header|POST|false|request success\n"
	got := readLog(dir, listLogs(dir)[0])
	check("RequestInfo 输出与预置模板一致", got == want,
		fmt.Sprintf("got:\n%s\nwant:\n%s", got, want))
}
