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
	"testing"
)

// TestNewRollingWriterMkdirFails 覆盖 NewRollingWriter 中 os.MkdirAll
// 失败的错误分支：用一个普通文件充当父目录。
func TestNewRollingWriterMkdirFails(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Dir 的父路径是普通文件 -> MkdirAll 失败。
	_, err := NewRollingWriter(&Config{
		Dir:      filepath.Join(blocker, "logs"),
		BaseName: "app",
	})
	if err == nil {
		t.Fatal("expected an error when the log directory cannot be created")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("err = %v, want a mkdir error", err)
	}
}

// TestCleanupKeepsAllWhenUnderQuota 覆盖清理中 MaxBackups 足够大时不删除
// 任何文件、但仍走完重排序逻辑的分支（含同日按 seq 比较）。
//
// 注意：磁盘目录列表（os.ReadDir）是字典序，与清理器内部按 (date, seq)
// 的排序不一致是正常的 —— 例如 "xxx.01.log" 字典序在 "xxx.log" 之前。
// 因此排序正确性要通过"删除了谁"来验证，而不是目录序。
func TestCleanupKeepsAllWhenUnderQuota(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("active\n"))

	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-01.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.02.log",
	} {
		if err := os.WriteFile(filepath.Join(cfg.Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// MaxBackups 足够大：不删除，但重排序逻辑会被执行。
	w.cleaner.cfg.MaxBackups = 100
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 5 { // 4 个历史 + 1 个活动
		t.Fatalf("files = %v, want 5", files)
	}
	if got := w.Metrics().RemovedFiles.Load(); got != 0 {
		t.Fatalf("removed = %d, want 0", got)
	}
}

// TestCleanupRemovesOldestWithinSameDay 验证同日内序号较小（更老）的文件
// 会先被删除，确认 seq 排序确实影响删除顺序。
func TestCleanupRemovesOldestWithinSameDay(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("active\n"))

	// 只有同一天的三个序号文件（无跨日），考验 seq 排序。
	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.02.log",
	} {
		if err := os.WriteFile(filepath.Join(cfg.Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 4 个文件（3 历史 + 1 活动），保留 2 个 -> 删掉序号最小的两个。
	w.cleaner.cfg.MaxBackups = 2
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	// 序号最小的（无序号）应已被删除。
	for _, f := range files {
		if f == "LOG_CALL_INFO.2026-09-02.playurl8080.log" {
			t.Fatalf("最老的同日文件未被删除: %v", files)
		}
	}
}
