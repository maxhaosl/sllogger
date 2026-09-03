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
	"testing"
	"time"
)

// TestCleanupBackupsExcessExceedsRemovable 覆盖 MaxBackups 分支中
// "excess > len(removable)" 的截断逻辑。
//
// 当所有文件都是活动文件时 removable 为空，此时不能删除任何文件，
// 否则会删掉正在写入的文件。
func TestCleanupBackupsExcessExceedsRemovable(t *testing.T) {
	cfg, _ := testConfig(t)
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

	// MaxBackups=1，但唯一的文件是活动文件（removable 为空），
	// excess(1) > len(removable)(0)，必须被截断为 0。
	w.cleaner.cfg.MaxBackups = 1
	w.cleaner.Clean()

	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("活动文件被误删: %v", err)
	}
	if got := len(listFiles(t, cfg.Dir)); got != 1 {
		t.Fatalf("files = %d, want 1", got)
	}
}

// TestCleanupBackupsKeepsActivePlusQuota 覆盖活动文件 + 历史文件混合时，
// 配额只作用于历史文件的分支。
func TestCleanupBackupsKeepsActivePlusQuota(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("active\n"))
	active := filepath.Base(w.path)

	// 4 个历史文件。
	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-01.playurl8080.log",
		"LOG_CALL_INFO.2026-09-01.playurl8080.01.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.01.log",
	} {
		if err := os.WriteFile(filepath.Join(cfg.Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 共 5 个文件，MaxBackups=2 -> excess=3，但 removable 只有 4 个，
	// 因此删掉 3 个最老的历史文件，保留 1 个历史 + 1 个活动。
	w.cleaner.cfg.MaxBackups = 2
	w.cleaner.Clean()

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	// 活动文件必须存活。
	var activeKept bool
	for _, f := range files {
		if f == active {
			activeKept = true
		}
	}
	if !activeKept {
		t.Fatalf("活动文件被误删: %v", files)
	}
}

// TestWriteSizeRotationError 覆盖 Write 中"文件已满触发滚动"时
// rotateLocked 返回错误的分支。
func TestWriteSizeRotationError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 10
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 先写满当前文件（达到 MaxSize）。
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if w.size < w.cfg.MaxSize {
		t.Fatalf("size = %d, want >= MaxSize", w.size)
	}

	// 删除目录后再写：滚动（打开新序号文件）必然失败。
	os.RemoveAll(cfg.Dir)

	if _, err := w.Write([]byte("more\n")); err == nil {
		t.Fatal("expected a rotation error after the directory was removed")
	}
}

// TestWriteRemainNonPositiveError 覆盖 Write 中"剩余空间 <= 0 时滚动"
// 失败并 continue 的分支。
func TestWriteRemainNonPositiveError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 5
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 写一个超过 MaxSize 的块，使 remain 计算为负。
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	// 此时 w.size(10) > MaxSize(5)，remain 为负。
	os.RemoveAll(cfg.Dir)

	if _, err := w.Write([]byte("next\n")); err == nil {
		t.Fatal("expected an error when remain <= 0 and rotation fails")
	}
}

// TestWriteIntervalRotationError 覆盖 Write 中"定时间隔触发滚动"失败
// 的错误返回分支。
func TestWriteIntervalRotationError(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = time.Hour

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("seed\n")); err != nil {
		t.Fatal(err)
	}

	// 推进超过一个间隔，并删除目录，使滚动失败。
	clock.Advance(2 * time.Hour)
	os.RemoveAll(cfg.Dir)

	if _, err := w.Write([]byte("next hour\n")); err == nil {
		t.Fatal("expected a rotation error after the directory was removed")
	}
}
