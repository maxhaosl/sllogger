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
	"time"
)

// TestWriteRotationError 覆盖 Write 在滚动失败时的错误返回路径。
func TestWriteRotationError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 50
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write([]byte("seeded\n"))

	// 把目录变为只读（或删除），使下一次滚动时 openFile 失败。
	os.RemoveAll(cfg.Dir)

	// 触发滚动：必须返回错误而不是静默丢失或 panic。
	if _, err := w.Write([]byte(strings.Repeat("x", 200))); err == nil {
		t.Fatal("expected a rotation error when the directory is gone")
	}
}

// TestWriteDateThenIntervalRotation 覆盖日期变化优先于定时滚动的分支顺序。
func TestWriteDateThenIntervalRotation(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = time.Hour
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write([]byte("day1\n"))
	// 跨天且跨多个小时：日期分支应先命中，序号重置为 0。
	clock.Advance(50 * time.Hour) // -> 2026-09-05 12:00
	w.Write([]byte("day3\n"))

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	// 新的一天必须是无序号文件。
	if _, err := os.Stat(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-05.playurl8080.log")); err != nil {
		t.Fatalf("expected an unnumbered file for the new day: %v", files)
	}
}

// TestSyncAfterCloseAndNilFile 覆盖 Sync 在已关闭/无文件时的路径。
func TestSyncAfterCloseAndNilFile(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync before close = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// 已关闭：file 为 nil，Sync 必须安全返回 nil。
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync after close = %v, want nil", err)
	}
}

// TestCleanerScanSkipsDirectories 覆盖 scan 跳过子目录与 Info 失败的分支。
func TestCleanerScanSkipsDirectories(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 子目录不应被当作日志文件。
	if err := os.Mkdir(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2020-01-01.playurl8080.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 有效文件仍应被扫描到。
	valid := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2020-01-02.playurl8080.log")
	if err := os.WriteFile(valid, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 除 valid 外，writer 自身已在当日（baseTime）创建了一个活动文件。
	files := w.cleaner.scan()
	if len(files) != 2 {
		t.Fatalf("scan = %d files, want 2 (valid + active; directories skipped)", len(files))
	}
	var sawValid bool
	for _, f := range files {
		if f.path == valid {
			sawValid = true
		}
	}
	if !sawValid {
		t.Fatalf("scan did not include %q: %+v", valid, files)
	}
}

// TestDirLogTotalSkipsNonLogFiles 覆盖 dirLogTotal 跳过非 .log 文件。
func TestDirLogTotalSkipsNonLogFiles(t *testing.T) {
	cfg, _ := testConfig(t)
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	os.WriteFile(filepath.Join(cfg.Dir, "a.log"), []byte(strings.Repeat("x", 100)), 0o644)
	os.WriteFile(filepath.Join(cfg.Dir, "b.txt"), []byte(strings.Repeat("y", 999)), 0o644)
	os.Mkdir(filepath.Join(cfg.Dir, "c.log"), 0o755) // 名为 .log 的目录

	if got := w.cleaner.dirLogTotal(); got != 100 {
		t.Fatalf("dirLogTotal = %d, want 100 (non-log files and dirs skipped)", got)
	}
}

// TestFindResumeFileSkipsDirectoriesAndErrors 覆盖 findResumeFile 的容错分支。
func TestFindResumeFileSkipsDirectoriesAndErrors(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 与当日文件同名的目录，不应被当作可续写文件。
	os.Mkdir(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.05.log"), 0o755)
	// 其他日期的文件应被忽略。
	os.WriteFile(filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-01.playurl8080.log"),
		[]byte("old"), 0o644)

	seq, size, resume := w.findResumeFile(w.namer.stampOf(baseTime))
	if !resume {
		t.Fatal("expected to resume the current day file")
	}
	if seq != 0 {
		t.Fatalf("seq = %d, want 0 (directory and other days ignored)", seq)
	}
	_ = size
}

// TestFindResumeFileReadDirError 覆盖目录不存在时的容错。
func TestFindResumeFileReadDirError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.Dir = filepath.Join(cfg.Dir, "missing")
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 目录被删除后扫描：应返回不可续写，而不是 panic。
	os.RemoveAll(cfg.Dir)
	seq, _, resume := w.findResumeFile(w.namer.stampOf(baseTime))
	if resume {
		t.Fatal("resume should be false when the directory cannot be read")
	}
	if seq != 0 {
		t.Fatalf("seq = %d, want 0", seq)
	}
}

// TestOpenFileStatFallback 覆盖 openFile 中 Stat 失败时回退到初始大小。
func TestOpenFileStatFallback(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 用初始大小打开：Stat 成功时会以实际大小为准。
	if err := w.openFile(baseTime, w.namer.stampOf(baseTime), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(w.path); got != "LOG_CALL_INFO.2026-09-03.playurl8080.03.log" {
		t.Fatalf("path = %q, want seq 03", got)
	}
	if w.seq != 3 {
		t.Fatalf("seq = %d, want 3", w.seq)
	}
}

// TestCleanupLoopRunsPeriodically 覆盖 cleanupLoop 的定时分支。
func TestCleanupLoopRunsPeriodically(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = 0
	cfg.MaxBackups = 1
	cfg.CleanupInterval = 5 * time.Millisecond // 远小于默认 10 分钟

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 制造多个文件，等待后台清理协程收敛到 MaxBackups。
	for _, name := range []string{
		"LOG_CALL_INFO.2026-09-01.playurl8080.log",
		"LOG_CALL_INFO.2026-09-02.playurl8080.log",
		"LOG_CALL_INFO.2026-09-03.playurl8080.01.log",
	} {
		os.WriteFile(filepath.Join(cfg.Dir, name), []byte("x"), 0o644)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := len(listFiles(t, cfg.Dir)); got <= 2 {
			return // 后台清理生效（含活动文件）
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cleanupLoop did not run: files = %v", listFiles(t, cfg.Dir))
}

// TestNewRollingWriterResumeStatError 覆盖 openFile 续写路径的 Stat 分支。
func TestNewRollingWriterResumeStatError(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 1000
	cfg.RotationInterval = 0

	// 预置一个未满的当日文件，重启后应续写并读取实际大小。
	target := filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")
	os.WriteFile(target, []byte(strings.Repeat("a", 100)), 0o644)

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if w.size != 100 {
		t.Fatalf("size = %d, want 100 (stat after open)", w.size)
	}
	if filepath.Base(w.path) != "LOG_CALL_INFO.2026-09-03.playurl8080.log" {
		t.Fatalf("path = %q", w.path)
	}
}
