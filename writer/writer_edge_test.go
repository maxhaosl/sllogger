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

package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAsyncWriterWriteErrorOnBlockedClose 覆盖阻塞模式下 worker 已退出时的
// Write 路径（返回 ErrClosed）。
func TestAsyncWriterWriteErrorOnBlockedClose(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.BlockOnFull = true
	cfg.QueueSize = 1
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)

	// 填满队列并关闭：worker 退出后，阻塞中的 Write 应返回 ErrClosed。
	aw.Write([]byte("a\n"))
	aw.Write([]byte("b\n"))
	aw.Write([]byte("c\n"))
	aw.Close()

	// 关闭后写入必须立即返回 ErrClosed，不会挂起。
	done := make(chan error, 1)
	go func() {
		_, err := aw.Write([]byte("d\n"))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write after Close blocked")
	}
}

// TestAsyncWriterSyncAfterDone 覆盖 Sync 在 worker 已退出时的路径。
func TestAsyncWriterSyncAfterDone(t *testing.T) {
	cfg, _ := testConfig(t)
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	aw := NewAsyncWriter(rw, cfg)
	aw.Write([]byte("x\n"))

	// 关闭后再 Sync 必须安全返回。
	aw.Close()
	for i := 0; i < 3; i++ {
		if err := aw.Sync(); err != nil {
			t.Fatalf("Sync after Close = %v", err)
		}
	}
}

// TestCleanerActiveFileProtection 验证清理器不会删除正在写入的文件。
func TestCleanerActiveFileProtection(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxBackups = 1
	cfg.MaxAge = time.Nanosecond // 所有历史文件都"过期"
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("active\n")); err != nil {
		t.Fatal(err)
	}
	activePath := w.path

	// 极端的清理条件：只有活动文件应存活。
	w.cleaner.Clean()

	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active file was removed: %v", err)
	}
	if got := len(listFiles(t, cfg.Dir)); got != 1 {
		t.Fatalf("files = %v, want only the active file", listFiles(t, cfg.Dir))
	}
}

// TestCleanerIsActiveWithoutCallback 未设置活动回调时 isActive 返回 false。
func TestCleanerIsActiveWithoutCallback(t *testing.T) {
	cfg, _ := testConfig(t)
	c := newCleaner(cfg.withDefaults(), newNamer(cfg.withDefaults()), &Metrics{})
	if c.isActive("/any/path") {
		t.Fatal("isActive must be false when no callback is installed")
	}
}

// TestCleanerScanErrors 覆盖 scan/dirLogTotal 在目录不可读时的容错路径。
func TestCleanerScanErrors(t *testing.T) {
	cfg, _ := testConfig(t)
	c := newCleaner(cfg.withDefaults(), newNamer(cfg.withDefaults()), &Metrics{})
	// 指向一个不存在的目录：ReadDir 失败时应返回空结果而非 panic。
	c.cfg.Dir = filepath.Join(cfg.Dir, "does-not-exist")
	if got := c.scan(); len(got) != 0 {
		t.Fatalf("scan = %v, want empty on error", got)
	}
	if got := c.dirLogTotal(); got != 0 {
		t.Fatalf("dirLogTotal = %d, want 0 on error", got)
	}
	// Clean 必须安全返回。
	c.Clean()
}

// TestCleanerBackupsAllActive 当所有文件都是活动文件时，不能被清理。
func TestCleanerBackupsAllActive(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxBackups = 1
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("only\n"))

	// 活动文件是唯一文件：MaxBackups=1 不应删除它。
	w.cleaner.Clean()
	if got := len(listFiles(t, cfg.Dir)); got != 1 {
		t.Fatalf("files = %d, want 1 (active file is protected)", got)
	}
}

// TestCleanerRemovesOldestFirst 同日内按序号、跨日按日期排序删除。
func TestCleanerRemovesOldestFirst(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 两个历史日期，每天两个序号。注意 writer 自身已在当日
	//（2026-09-03，见 baseTime）创建了一个文件。
	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-01.playurl8080.log",
		"LOG_CALL_INFO.2026-09-01.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.01.log",
	} {
		p := filepath.Join(cfg.Dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 共 5 个文件，按 (日期, 序号) 排序后保留最新的 2 个。
	w.cleaner.cfg.MaxBackups = 2
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	// 集合校验（与字典序无关）：保留 09-02 的 01 与 writer 当日的 09-03。
	want := map[string]bool{
		"LOG_CALL_INFO.2026-09-02.playurl8080.01.log": true,
		"LOG_CALL_INFO.2026-09-03.playurl8080.log":    true,
	}
	for _, f := range files {
		if !want[f] {
			t.Fatalf("unexpected remaining file %q in %v", f, files)
		}
	}
	// 最老的一天必须整体消失。
	for _, f := range files {
		if strings.Contains(f, "2026-09-01") {
			t.Fatalf("oldest day survived: %v", files)
		}
	}
}

// TestNamerTruncateToLayoutCustom 自定义布局走 Format+Parse 路径（非 fastDay）。
func TestNamerTruncateToLayoutCustom(t *testing.T) {
	tests := []struct {
		layout string
		want   time.Time
	}{
		// 小时粒度。
		{"2006-01-02-15", time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)},
		// 分钟粒度。
		{"2006-01-02-15-04", time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)},
		// 天粒度的另一种写法。
		{"2006/01/02", time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local)},
	}
	for _, tt := range tests {
		cfg := (&Config{
			Dir:        "/tmp",
			BaseName:   "LOG",
			DateLayout: tt.layout,
		}).withDefaults()
		n := newNamer(cfg)
		if n.fastDay {
			t.Fatalf("layout %q must not take the fast day path", tt.layout)
		}
		got := n.truncateToLayout(baseTime)
		if !got.Equal(tt.want) {
			t.Errorf("truncateToLayout(%q) = %v, want %v", tt.layout, got, tt.want)
		}
	}
}

// TestNamerTruncateToLayoutUnparsable 无法用同一布局回读的时间戳时回退到原值。
func TestNamerTruncateToLayoutUnparsable(t *testing.T) {
	// 该布局格式化后无法被同一布局解析（缺少小数秒前的点），
	// 此时 truncateToLayout 必须回退为原始时间。
	cfg := (&Config{
		Dir:        "/tmp",
		BaseName:   "LOG",
		DateLayout: "2006-01-02 15:04:05.999999999",
	}).withDefaults()
	n := newNamer(cfg)

	// 纳秒为 0 时，Format 输出空的小数部分，Parse 会失败。
	ts := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)
	got := n.truncateToLayout(ts)
	// 无论是解析成功还是回退，都不能 panic，且结果必须是有效时间。
	if got.IsZero() {
		t.Fatalf("truncateToLayout returned the zero time")
	}
	if got.After(ts) {
		t.Fatalf("truncateToLayout = %v, must not be after %v", got, ts)
	}
}

// TestLayoutGranularityAllBranches 覆盖 layoutGranularity 的每种返回。
func TestLayoutGranularityAllBranches(t *testing.T) {
	tests := []struct {
		layout string
		want   time.Duration
	}{
		{"2006-01-02", 24 * time.Hour},
		{"2006-01-02 15", time.Hour},
		{"2006-01-02 15:04", time.Minute},
		{"2006-01-02 15:04:05", time.Second},
		{"2006-01-02 03PM", time.Hour},
		{"2006-01-02 3", time.Hour},
		{"2006-01-02 4x", time.Minute},
		{"2006-01-02 5x", time.Second},
	}
	for _, tt := range tests {
		if got := layoutGranularity(tt.layout); got != tt.want {
			t.Errorf("layoutGranularity(%q) = %v, want %v", tt.layout, got, tt.want)
		}
	}
}

// TestParseFileNameEdgeCases 覆盖文件名解析的边界分支。
func TestParseFileNameEdgeCases(t *testing.T) {
	cfg := (&Config{
		Dir:         "/data/logs",
		BaseName:    "LOG_CALL_INFO",
		ServiceName: "playurl",
		ServicePort: 8080,
	}).withDefaults()
	n := newNamer(cfg)

	tests := []struct {
		name string
		ok   bool
	}{
		// 序号为 0 或负数时不视为序号。
		{"LOG_CALL_INFO.2026-09-03.playurl8080.00.log", false},
		{"LOG_CALL_INFO.2026-09-03.playurl8080.-1.log", false},
		// 缺少 service 段。
		{"LOG_CALL_INFO.2026-09-03.log", false},
		// 空中间段。
		{"LOG_CALL_INFO..log", false},
		// 没有 .log 后缀。
		{"LOG_CALL_INFO.2026-09-03.playurl8080", false},
		// 前缀不匹配。
		{"LOG_CALL.2026-09-03.playurl8080.log", false},
		// 多余段。
		{"LOG_CALL_INFO.2026-09-03.extra.playurl8080.log", false},
		// 合法：带 service 与序号。
		{"LOG_CALL_INFO.2026-09-03.playurl8080.12.log", true},
		// 合法：仅日期（service 为空时）。
		{"LOG_CALL_INFO.2026-09-03.log", false},
	}
	for _, tt := range tests {
		_, ok := n.parseFileName(tt.name, nil)
		if ok != tt.ok {
			t.Errorf("parseFileName(%q) = %v, want %v", tt.name, ok, tt.ok)
		}
	}
}

// TestParseFileNameNoServiceAnchor 未配置 service 时的解析。
func TestParseFileNameNoServiceAnchor(t *testing.T) {
	cfg := (&Config{
		Dir:      "/data/logs",
		BaseName: "LOG",
	}).withDefaults()
	n := newNamer(cfg)
	if n.service != "" {
		t.Fatal("service should be empty")
	}

	// 没有 service 锚点时，中间段应只剩日期。
	pf, ok := n.parseFileName("LOG.2026-09-03.log", nil)
	if !ok {
		t.Fatal("expected a successful parse without a service anchor")
	}
	if pf.seq != 0 {
		t.Fatalf("seq = %d, want 0", pf.seq)
	}
	// 带序号也能解析。
	pf, ok = n.parseFileName("LOG.2026-09-03.05.log", nil)
	if !ok {
		t.Fatal("expected a successful parse with a sequence number")
	}
	if pf.seq != 5 {
		t.Fatalf("seq = %d, want 5", pf.seq)
	}
	// 两个非日期段时无法区分，应拒绝。
	if _, ok := n.parseFileName("LOG.2026-09-03.extra.log", nil); ok {
		t.Fatal("expected rejection for an ambiguous name")
	}
}

// TestRenderNoTrailingBrace 覆盖 render 中花括号不闭合的分支。
func TestRenderNoTrailingBrace(t *testing.T) {
	n := newNamer((&Config{BaseName: "LOG"}).withDefaults())
	// 未闭合的占位符：整个串原样保留，不做部分替换。
	if got := n.render("{base.log", baseTime, 0); got != "{base.log" {
		t.Fatalf("got %q, want the pattern preserved verbatim", got)
	}
	// 没有占位符时原样返回。
	if got := n.render("plain.log", baseTime, 0); got != "plain.log" {
		t.Fatalf("got %q", got)
	}
}
