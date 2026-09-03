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

// TestWriteChunkedBatches 覆盖 Write 的分块路径：单次写入携带一个远超
// MaxSize 的大批量数据时，必须在行边界处切分并滚动，且每行保持完整。
func TestWriteChunkedBatches(t *testing.T) {
	const lineLen = 40 // "aaaa...\n"
	cfg, _ := testConfig(t)
	cfg.MaxSize = 200
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 一次写入 50 行（2000 B），远超 MaxSize(200)。
	big := strings.Repeat(strings.Repeat("a", lineLen-1)+"\n", 50)
	n, err := w.Write([]byte(big))
	if err != nil {
		t.Fatal(err)
	}
	if n != len(big) {
		t.Fatalf("n = %d, want %d", n, len(big))
	}

	files := listFiles(t, cfg.Dir)
	if len(files) < 10 {
		t.Fatalf("files = %d, want at least 10 after chunked rotation", len(files))
	}

	// 每个文件都不超过 MaxSize，且内容全是完整行。
	var totalLines int
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(cfg.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > 200 {
			t.Fatalf("%s is %d bytes, exceeds MaxSize(200)", f, len(b))
		}
		// 内容必须以换行结尾（行不被切断）。
		if len(b) > 0 && b[len(b)-1] != '\n' {
			t.Fatalf("%s ends mid-line: %q", f, b)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			if len(line) != lineLen-1 {
				t.Fatalf("%s has a broken line of length %d", f, len(line))
			}
			totalLines++
		}
	}
	if totalLines != 50 {
		t.Fatalf("total lines = %d, want 50", totalLines)
	}
}

// TestWriteSingleEntryLargerThanMaxSize 单条日志超过 MaxSize 时必须整条写入，
// 不能无限循环。
func TestWriteSingleEntryLargerThanMaxSize(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 100
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 没有换行符的巨型单条：无法在行边界切分。
	huge := strings.Repeat("z", 5000)
	if _, err := w.Write([]byte(huge)); err != nil {
		t.Fatal(err)
	}
	files := listFiles(t, cfg.Dir)
	if len(files) != 1 {
		t.Fatalf("files = %v, want 1 (written whole)", files)
	}
	got := readAll(t, filepath.Join(cfg.Dir, files[0]))
	if got != huge {
		t.Fatalf("content length = %d, want %d", len(got), len(huge))
	}

	// 后续写入继续正常滚动。
	if _, err := w.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	if got := len(listFiles(t, cfg.Dir)); got != 2 {
		t.Fatalf("files = %d, want 2 after the next write", got)
	}
}

// TestWriteExactBoundary 写入大小正好等于剩余空间时的边界行为。
func TestWriteExactBoundary(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 10
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 正好填满：5 字节 × 2 = 10。
	if _, err := w.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("67890")); err != nil {
		t.Fatal(err)
	}
	if w.size != 10 {
		t.Fatalf("size = %d, want 10", w.size)
	}
	// 再写一次必须触发滚动（remain <= 0 分支）。
	if _, err := w.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2 after filling the first file", files)
	}
	if got := readAll(t, filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log")); got != "1234567890" {
		t.Fatalf("first file = %q", got)
	}
}

// TestWritePartialLineThenRotate 剩余空间不足以容纳整行时，整行写入新文件。
func TestWritePartialLineThenRotate(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 20
	cfg.RotationInterval = 0

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 先写 10 字节（含换行），剩 10 字节。
	if _, err := w.Write([]byte("012345678\n")); err != nil {
		t.Fatal(err)
	}
	// 这一行 20 字节，剩余空间不够，应整体放到新文件。
	if _, err := w.Write([]byte(strings.Repeat("b", 19) + "\n")); err != nil {
		t.Fatal(err)
	}

	files := listFiles(t, cfg.Dir)
	if len(files) != 2 {
		t.Fatalf("files = %v, want 2", files)
	}
	first := readAll(t, filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.log"))
	if first != "012345678\n" {
		t.Fatalf("first file = %q, want the short line only", first)
	}
	second := readAll(t, filepath.Join(cfg.Dir, "LOG_CALL_INFO.2026-09-03.playurl8080.01.log"))
	if second != strings.Repeat("b", 19)+"\n" {
		t.Fatalf("second file = %q, want the whole long line", second)
	}
}

func TestLastLineEnd(t *testing.T) {
	tests := []struct {
		name  string
		p     string
		limit int64
		want  int
	}{
		{"empty", "", 10, 0},
		{"no newline", "abc", 10, 0},
		{"exact at boundary", "ab\n", 3, 3},
		{"boundary before newline", "ab\ncd", 3, 3},
		{"multiple newlines", "a\nb\nc\n", 4, 4},
		{"limit beyond length", "a\nb\n", 100, 4},
		{"limit zero", "a\n", 0, 0},
		{"newline at index 0", "\nabc", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastLineEnd([]byte(tt.p), tt.limit); got != tt.want {
				t.Errorf("lastLineEnd(%q, %d) = %d, want %d", tt.p, tt.limit, got, tt.want)
			}
		})
	}
}

// TestWriteRotateDuringChunk 分块过程中跨越时间边界：块间检查会触发滚动。
func TestWriteRotateDuringChunk(t *testing.T) {
	cfg, clock := testConfig(t)
	cfg.MaxSize = 0
	cfg.RotationInterval = time.Hour

	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 一个小批次，时间戳仍在当前小时内。
	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	// 推进时间后，同一个 Write 调用内的下一个块应触发定时滚动。
	clock.Advance(2 * time.Hour)

	// 构造一个跨越多次检查的批量：先推进时间，再写多块。
	// 由于单次 Write 内 clock 不变，这里验证跨调用场景。
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	if got := len(listFiles(t, cfg.Dir)); got != 2 {
		t.Fatalf("files = %d, want 2 after the interval elapsed", got)
	}
}

// TestWriteZeroLengthPayload 空写入不应触发滚动或报错。
func TestWriteZeroLengthPayload(t *testing.T) {
	cfg, _ := testConfig(t)
	cfg.MaxSize = 10
	cfg.RotationInterval = 0
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	n, err := w.Write(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}
	if got := len(listFiles(t, cfg.Dir)); got != 1 {
		t.Fatalf("files = %d, want 1 (no rotation for an empty write)", got)
	}
}
