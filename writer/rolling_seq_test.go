package writer

import (
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

// seqFakeClock 可控时钟：用于在测试中模拟跨小时，而不必真实等待一小时。
type seqFakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *seqFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set 调整当前时间（仅测试用）。
func (c *seqFakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *seqFakeClock) NewTicker(d time.Duration) *time.Ticker {
	return time.NewTicker(d)
}

var _ slcore.Clock = (*seqFakeClock)(nil)

// newSeqTestWriter 构造一个使用 {base}.{date}.{seq}.{service}.log 命名、禁用定时滚动、
// 单文件上限 maxSize 字节、时钟为 clk 的 RollingWriter，用于验证序号/滚动逻辑。
func newSeqTestWriter(t *testing.T, dir string, clk *seqFakeClock, maxSize int64) *RollingWriter {
	t.Helper()
	cfg := &Config{
		Dir:                dir,
		BaseName:           "LOG_CALL_INFO",
		NamePattern:        "{base}.{date}.{seq}.{service}.log",
		RotatedNamePattern: "{base}.{date}.{seq}.{service}.log",
		DateLayout:         "2006-01-02-15", // 按小时切分
		ServiceName:        "aegis",
		ServicePort:        9092,
		MaxSize:            maxSize,
		RotationInterval:   0, // 0 = 禁用定时滚动，仅按小时+大小
		MaxAge:             24 * time.Hour,
		CleanupInterval:    10 * time.Minute,
		Clock:              clk,
	}
	w, err := NewRollingWriter(cfg)
	if err != nil {
		t.Fatalf("NewRollingWriter: %v", err)
	}
	return w
}

func listSeqFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func writeSeqLine(w *RollingWriter, body string) {
	// 每行以 \n 结尾，便于库按行边界切分；单行均小于 MaxSize，避免触发“超大单行整体写”分支。
	_, _ = w.Write([]byte(body + "\n"))
}

func hasSeqFile(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

// TestRollingWriterSeqAlwaysPresent 验证序号始终存在且符合规则：
//  1. 首文件序号为 .01.（而非 .00.）
//  2. 同小时内文件超过 MaxSize 自动切到 .02.
//  3. 服务重启后追加到当前小时最大序号文件（如 .02.），若其已满则新开 .03.
//  4. 跨整点切到新小时的首文件 .01.（如 19:00 的 .01.）
func TestRollingWriterSeqAlwaysPresent(t *testing.T) {
	dir := t.TempDir()
	clk := &seqFakeClock{now: time.Date(2026, 9, 12, 18, 0, 0, 0, time.Local)}
	const maxSize int64 = 300

	// ── 场景 1：全新启动，首文件应为 .01. ──
	w1 := newSeqTestWriter(t, dir, clk, maxSize)
	files := listSeqFiles(t, dir)
	if !hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-18.01.aegis9092.log") {
		t.Fatalf("首文件应为 .01.，实际: %v", files)
	}
	if hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-18.00.aegis9092.log") {
		t.Fatalf("不应出现 .00. 文件: %v", files)
	}

	// 写三行（每行 101 字节），第三行触发按大小滚动 -> .02.
	writeSeqLine(w1, strings.Repeat("a", 100))
	writeSeqLine(w1, strings.Repeat("b", 100))
	writeSeqLine(w1, strings.Repeat("c", 100))
	files = listSeqFiles(t, dir)
	if !hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-18.02.aegis9092.log") {
		t.Fatalf("超大小后应生成 .02.，实际: %v", files)
	}

	// ── 场景 3：模拟服务重启（同一目录、同小时新建 writer）──
	w1.Close()
	w2 := newSeqTestWriter(t, dir, clk, maxSize) // 应续接到当前小时最大序号 .02.
	// 续接后写两行：第一行追加到 .02.（未满），第二行超大小 -> 新开 .03.
	writeSeqLine(w2, strings.Repeat("d", 100))
	writeSeqLine(w2, strings.Repeat("e", 100))
	files = listSeqFiles(t, dir)
	if !hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-18.03.aegis9092.log") {
		t.Fatalf("重启后继续超大小应新开 .03.，实际: %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, ".00.") {
			t.Fatalf("出现非法 .00. 序号文件: %v", files)
		}
	}

	// ── 场景 4：跨整点到 19:00，应切到新小时首文件 .01. ──
	clk.Set(time.Date(2026, 9, 12, 19, 0, 0, 0, time.Local))
	writeSeqLine(w2, strings.Repeat("f", 100))
	files = listSeqFiles(t, dir)
	if !hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-19.01.aegis9092.log") {
		t.Fatalf("跨小时后应生成 19:00 的 .01.，实际: %v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "LOG_CALL_INFO.2026-09-12-19.") && !strings.Contains(f, ".01.aegis9092.log") {
			t.Fatalf("19:00 仅应出现 .01.，实际: %v", files)
		}
	}
	w2.Close()
}

// TestRollingWriterRotationIntervalZeroDisabled 验证 RotationInterval=0 时禁用定时滚动，
// 在不足 MaxSize 且未跨小时的情况下不触发任何滚动。
func TestRollingWriterRotationIntervalZeroDisabled(t *testing.T) {
	dir := t.TempDir()
	clk := &seqFakeClock{now: time.Date(2026, 9, 12, 18, 0, 0, 0, time.Local)}
	w := newSeqTestWriter(t, dir, clk, 1<<30) // 1G 上限，测试期间不会超大小
	// 推进时钟 5 小时（远超任意定时周期），但仅在同小时写入；由于 RotationInterval=0，
	// 不应产生任何滚动文件。
	for i := 0; i < 5; i++ {
		writeSeqLine(w, "keep-alive")
		clk.Set(clk.Now().Add(1 * time.Hour))
	}
	files := listSeqFiles(t, dir)
	if !hasSeqFile(files, "LOG_CALL_INFO.2026-09-12-18.01.aegis9092.log") {
		t.Fatalf("应存在 18:00 的 .01.，实际: %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, ".02.") {
			t.Fatalf("RotationInterval=0 不应触发定时滚动，却出现 .02.：%v", files)
		}
	}
	w.Close()
}
