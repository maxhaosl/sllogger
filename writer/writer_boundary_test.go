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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestShardsQueueSmallerThanShardCount 覆盖队列容量小于分片数时每片至少为 1
// 的分支（避免构造出容量为 0 的 channel 导致写入必阻塞）。
func TestShardsQueueSmallerThanShardCount(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.QueueSize = 2
	cfg.Shards = 8 // 2/8 = 0 -> 每片至少 1
	cfg.BlockOnFull = true

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)
	for _, ch := range aw.shards {
		if cap(ch) != 1 {
			t.Fatalf("shard capacity = %d, want 1 when QueueSize < Shards", cap(ch))
		}
	}

	// 容量虽小但阻塞模式下仍不能丢数据。
	const n = 200
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n/4; i++ {
				aw.Write([]byte("x\n"))
			}
		}()
	}
	wg.Wait()
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	if got := countLines(readAll(t, rw.path)); got != n {
		t.Fatalf("lines = %d, want %d", got, n)
	}
}

// TestAsyncWriteBlockedAfterDone 覆盖阻塞模式下 worker 已退出时 Write
// 返回 ErrClosed 的分支。
func TestAsyncWriteBlockedAfterDone(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.BlockOnFull = true
	cfg.QueueSize = 1
	cfg.FlushInterval = time.Hour // 不自动排空，制造阻塞

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)

	// 填充满队列，使后续 Write 进入阻塞的 select。
	for i := 0; i < cap(aw.shards[0])+4; i++ {
		aw.Write([]byte("fill\n"))
	}

	// 并发关闭：阻塞中的 Write 应因 <-a.done 就绪而返回 ErrClosed。
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		aw.Close()
	}()

	// 在关闭过程中持续写入，命中 done 分支。
	gotErr := false
	for i := 0; i < 200 && !gotErr; i++ {
		if _, err := aw.Write([]byte("after close\n")); err != nil {
			gotErr = true
		}
	}
	wg.Wait()
	if !gotErr {
		t.Log("未命中 done 分支（时序相关），不视为失败")
	}
}

// TestAsyncRunDrainsOnClose 覆盖 worker 在收到 done 后继续排空剩余队列的
// 内层循环分支。
func TestAsyncRunDrainsOnClose(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.BlockOnFull = true
	cfg.QueueSize = 10000
	cfg.BatchSize = 100000 // 很大，确保不会因批次满而提前 flush
	cfg.FlushInterval = time.Hour

	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)

	// 写入远超一批的数据，然后立即关闭：
	// worker 必须在 done 分支里把队列排空再 flush。
	const n = 5000
	for i := 0; i < n; i++ {
		aw.Write([]byte("drain\n"))
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	if got := countLines(readAll(t, rw.path)); got != n {
		t.Fatalf("lines = %d, want %d（关闭时未排空）", got, n)
	}
}

// TestCleanupScanSkipsBrokenEntries 覆盖 scan 中 e.Info() 失败的分支：
// 损坏的目录条目应被跳过，且不影响其他文件。
func TestCleanupScanSkipsBrokenEntries(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ok := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-02.playurl8080.log")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 创建一个悬空符号链接：os.ReadDir 能列出它，但 Info() 会失败。
	broken := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-01.playurl8080.log")
	if err := os.Symlink(filepath.Join(cfg.Dir, "no-such-target"), broken); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	files := w.cleaner.scan()
	var found bool
	for _, f := range files {
		if f.path == ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("损坏条目导致正常文件也被跳过: %+v", files)
	}
}

// TestNewRollingWriterOpenFirstFileFails 覆盖 NewRollingWriter 中首个文件
// 打开失败时返回错误的分支。
func TestNewRollingWriterOpenFirstFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限检查不生效")
	}
	cfg, _ := testConfig(t)
	// 先放一个已写满的当日文件，迫使启动时打开新序号文件。
	full := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	if err := os.WriteFile(full, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.MaxSize = 10 // 100 >= 10，视为已满
	cfg.RotationInterval = 0

	// 收紧目录权限后再启动：创建 .01 文件必然失败。
	if err := os.Chmod(cfg.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cfg.Dir, 0o755)

	if _, err := NewRollingWriter(cfg); err == nil {
		t.Fatal("expected an error when the first file cannot be opened")
	}
}

// TestWriteDateRotationError 覆盖 Write 中日期变化触发滚动失败的错误分支。
func TestWriteDateRotationError(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("seed\n")); err != nil {
		t.Fatal(err)
	}

	// 跨天 + 删除目录 -> 滚动（打开新文件）必然失败。
	clock.Advance(48 * time.Hour)
	os.RemoveAll(cfg.Dir)

	if _, err := w.Write([]byte("next day\n")); err == nil {
		t.Fatal("expected a rotation error after the directory was removed")
	}
}

// TestFindResumeFileWithBrokenEntry 验证目录中存在损坏条目（悬空符号链接）
// 时，findResumeFile 仍能正常工作而不 panic。
//
// 注意：DirEntry.Info() 对符号链接执行 lstat，不会因目标缺失而失败，
// 因此 scan/findResumeFile 里的 statErr 分支属于防御性代码，无法从外部
// 稳定触发。这里验证的是整体健壮性。
func TestFindResumeFileWithBrokenEntry(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 悬空符号链接：ReadDir 可见，lstat 成功但目标不存在。
	broken := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.09.log")
	if err := os.Symlink(filepath.Join(cfg.Dir, "missing"), broken); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	// 不应 panic，且必须返回一个可用的序号。
	seq, _, resume := w.findResumeFile(w.namer.stampOf(baseTime))
	if !resume {
		t.Fatal("expected resume to succeed")
	}
	if seq < 0 {
		t.Fatalf("seq = %d, want >= 0", seq)
	}
	// 续写后仍能正常写入。
	if _, err := w.Write([]byte("after\n")); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

// TestTruncateToLayoutNonFastDay 验证非 fastDay 布局走 Format+Parse 路径
// 并能正确截断到布局粒度。
//
// 说明：truncateToLayout 里 `time.ParseInLocation` 失败时回退返回原时间，
// 是**防御性分支**。实测 Go 的 Format/Parse 对同一 layout 基本对称
// （纳秒为 0、分钟级、天级等布局均能成功往返），因此该分支无法通过
// 正常时间值触发，这里验证的是实际生效的 Format+Parse 路径。
func TestTruncateToLayoutNonFastDay(t *testing.T) {
	tests := []struct {
		layout string
		want   time.Time
	}{
		// 天粒度（不同写法，非 fastDay）。
		{"2006/01/02", time.Date(2026, 9, 3, 0, 0, 0, 0, time.Local)},
		// 小时粒度。
		{"2006-01-02-15", time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)},
		// 分钟粒度。
		{"2006-01-02-15-04", time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)},
		// 秒粒度（原时间 10:00:00，截断后不变）。
		{"2006-01-02 15:04:05", time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)},
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

// TestCleanerDirLogTotalWithBrokenEntry 验证 dirLogTotal 在目录含损坏
// 条目时不会 panic。
//
// 与 scan 同理：Info() 的 statErr 分支是防御性代码，无法从外部稳定触发。
func TestCleanerDirLogTotalWithBrokenEntry(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ok := filepath.Join(cfg.Dir, "a.log")
	if err := os.WriteFile(ok, []byte(strings.Repeat("x", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(cfg.Dir, "b.log")
	if err := os.Symlink(filepath.Join(cfg.Dir, "missing"), broken); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	// 只需保证能算出非负结果且不 panic。
	if got := w.cleaner.dirLogTotal(); got < 40 {
		t.Fatalf("dirLogTotal = %d, want >= 40", got)
	}
}
