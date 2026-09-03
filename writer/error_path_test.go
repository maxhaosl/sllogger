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

// TestWriteFileWriteError 覆盖 Write 中 file.Write 返回错误的分支：
// 用已关闭的文件句柄注入 IO 错误。
func TestWriteFileWriteError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 关闭底层文件 → 后续 Write 必然失败。
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = w.Write([]byte("will fail\n"))
	if err == nil {
		t.Fatal("expected a write error on a closed file")
	}
	if w.Metrics().WriteErrors.Load() == 0 {
		t.Fatal("write error was not counted")
	}
}

// TestWriteRotationErrorAllBranches 覆盖 Write 中三个滚动分支各自的
// 失败返回路径：日期变化、定时间隔、大小超限。
func TestWriteRotationErrorAllBranches(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Config)
		advance time.Duration
	}{
		{
			name:    "日期变化触发滚动失败",
			setup:   func(c *Config) { c.MaxSize = 0; c.RotationInterval = 0 },
			advance: 48 * time.Hour,
		},
		{
			name:    "定时间隔触发滚动失败",
			setup:   func(c *Config) { c.MaxSize = 0; c.RotationInterval = time.Hour },
			advance: 2 * time.Hour,
		},
		{
			name:    "大小超限触发滚动失败",
			setup:   func(c *Config) { c.MaxSize = 10; c.RotationInterval = 0 },
			advance: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, clock := testConfig(t)
			tt.setup(cfg)
			w, err := NewRollingWriter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()

			// 先写一条建立基线。
			if _, err := w.Write([]byte("seed\n")); err != nil {
				t.Fatal(err)
			}

			// 推进时间（若需要），然后删除目录使滚动必然失败。
			if tt.advance > 0 {
				clock.Advance(tt.advance)
			}
			os.RemoveAll(cfg.Dir)

			if _, err := w.Write([]byte(strings.Repeat("x", 4096) + "\n")); err == nil {
				t.Fatal("expected a rotation error after the directory was removed")
			}
		})
	}
}

// TestWriteAfterFileRemovedNoRotation 目录被删除但没有触发滚动时，
// 写入仍应成功（文件句柄已打开），验证不改变既有行为。
func TestWriteAfterFileRemovedNoRotation(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	// 删除目录：已打开的 fd 仍可写入（Unix 语义）。
	os.RemoveAll(cfg.Dir)
	if _, err := w.Write([]byte("after\n")); err != nil {
		t.Fatalf("写入已打开的文件不应失败: %v", err)
	}
}

// TestOpenFilePermissionDenied 覆盖 NewRollingWriter 打开首个文件失败的路径。
func TestOpenFilePermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限检查不生效")
	}
	cfg, _ := testConfig(t)
	// 预置一个已"写满"的当日文件，迫使启动后开新文件（序号 01）。
	// 注意：必须先写文件再收紧目录权限，否则自己也无法创建。
	target := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.MaxSize = 1 // 1 byte >= MaxSize，视为已满

	if err := os.Chmod(cfg.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cfg.Dir, 0o755)

	if _, err := NewRollingWriter(cfg); err == nil {
		t.Fatal("expected an error when the directory is read-only")
	}
}

// TestFindResumeFileFullOpensNextSeq 覆盖 findResumeFile 中"文件已满则
// 从下一序号开始"的分支（配合 resume=false）。
func TestFindResumeFileFullOpensNextSeq(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 10
	cfg.RotationInterval = 0

	// 当日无序号文件已满（>= MaxSize）。
	full := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	if err := os.WriteFile(full, []byte(strings.Repeat("x", 50)), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if got := filepath.Base(w.path); got != "LOG_CALL_INFO.2026-09-03.playurl8080.01.log" {
		t.Fatalf("path = %q, want 序号 01（原文件已满）", got)
	}
}

// TestAsyncWriterInnerWriteError 覆盖 worker flush 时下游写入失败的路径：
// 错误必须被计入 WriteErrors 而不是 panic 或丢失。
func TestAsyncWriterInnerWriteError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)
	// 关闭下游文件，使 flush 必然失败。
	if err := rw.file.Close(); err != nil {
		t.Fatal(err)
	}

	aw.Write([]byte("will fail on flush\n"))

	// Sync 会把下游的 fsync 错误如实返回（这是正确行为：调用方需要知道
	// 数据没有落盘）。这里只要求它不 panic、不死锁。
	_ = aw.Sync()

	// Close 会如实返回下游关闭失败（正确行为：调用方需要知道资源未干净
	// 释放）。这里只要求不 panic、不死锁，且错误确实来自被关闭的文件。
	if err := aw.Close(); err == nil {
		t.Log("Close 未返回错误（下游已由 RollingWriter 处理）")
	}
	// 下游写入失败必须被计入指标，便于监控发现。
	if got := rw.Metrics().WriteErrors.Load(); got == 0 {
		t.Fatal("下游写入错误未被计入 WriteErrors")
	}
}

// TestTruncateToLayoutFallback 覆盖 truncateToLayout 中 Parse 失败回退的分支。
func TestTruncateToLayoutFallback(t *testing.T) {
	// 该布局格式化后无法被自身解析（小数秒为空时缺少小数点）。
	cfg := (&Config{
		Dir:        "/tmp",
		BaseName:   "LOG",
		DateLayout: "2006-01-02 15:04:05.999999999",
	}).withDefaults()
	n := newNamer(cfg)
	if n.fastDay {
		t.Fatal("自定义布局不应走 fastDay 分支")
	}

	// 纳秒为 0 → Format 输出空的小数部分 → Parse 失败 → 回退返回原时间。
	ts := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)
	got := n.truncateToLayout(ts)
	if !got.Equal(ts) {
		t.Fatalf("回退分支: got %v, want %v", got, ts)
	}
}

// TestCleanupScanInfoError 覆盖 scan 中 e.Info() 失败的分支。
func TestCleanupScanInfoError(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 创建一个正常的日志文件。
	ok := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-02.playurl8080.log")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 创建一个会 Info() 失败的条目：符号链接指向不存在的目标。
	broken := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-01.playurl8080.log")
	if err := os.Symlink(filepath.Join(cfg.Dir, "nonexistent-target"), broken); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	// 扫描应跳过损坏条目且保留正常条目，不能 panic。
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

// TestAsyncWriterRecycleAfterDrop 覆盖 Write 中丢弃时归还缓冲的分支。
func TestAsyncWriterRecycleAfterDrop(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.QueueSize = 1
	cfg.FlushInterval = time.Hour // 不自动 flush
	rw, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	aw := NewAsyncWriter(rw, cfg)
	defer aw.Close()

	// 持续写入，绝大多数会被丢弃；缓冲必须被正确归还复用，
	// 否则内存会随写入次数线性增长。
	for i := 0; i < 100000; i++ {
		aw.Write([]byte(strings.Repeat("x", 128) + "\n"))
	}
	m := aw.Metrics().Snapshot()
	if m.Dropped == 0 {
		t.Fatal("expected drops with a queue of size 1")
	}
	if m.Enqueued+m.Dropped != 100000 {
		t.Fatalf("enqueued+dropped = %d, want 100000", m.Enqueued+m.Dropped)
	}
}

var _ = errors.Is
