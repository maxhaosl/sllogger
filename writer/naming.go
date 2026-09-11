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
	"strconv"
	"strings"
	"sync"
	"time"
)

// Namer renders log file names from the configured patterns.
//
// 命名规则（默认）：
//
//	NamePattern        = "{base}.{date}.{service}.log"          第一个文件
//	RotatedNamePattern = "{base}.{date}.{service}.{seq}.log"    滚动文件
//
// 支持的占位符：{base} {date} {service} {app} {seq} {pid}
//
//   - {service} 渲染为 "<ServiceName><ServicePort>"，如 playurl8080
//   - {app}     仅渲染 ServiceName（不含端口），如 playurl
//
// 渲染示例：
//
//	LOG_CALL_INFO.2026-09-03.playurl8080.log
//	LOG_CALL_INFO.2026-09-03.playurl8080.01.log
//	LOG_CALL_INFO.2026-09-03-10.playurl.log          （{app}，按小时）
type Namer struct {
	cfg     *Config
	service string
	pid     string

	// fastDay reports whether DateLayout is the common day-only layout, in
	// which case truncation can be done with time.Date instead of
	// Format+ParseInLocation (which is significantly more expensive).
	fastDay bool
	// granularity is how long one truncated interval lasts, derived from
	// DateLayout. It bounds the validity window of the stamp cache.
	granularity time.Duration

	// Stamp cache: Write is on the hot path, so avoid re-computing the
	// truncated time on every single write.
	cacheMu    sync.Mutex
	cacheValid bool
	cacheStart time.Time
	cacheEnd   time.Time
}

func newNamer(cfg *Config) *Namer {
	n := &Namer{
		cfg:         cfg,
		service:     cfg.service(),
		pid:         strconv.Itoa(os.Getpid()),
		fastDay:     cfg.DateLayout == DefaultDateLayout,
		granularity: layoutGranularity(cfg.DateLayout),
	}
	return n
}

// layoutGranularity derives how long one DateLayout interval lasts, so the
// stamp cache can be invalidated correctly at interval boundaries.
func layoutGranularity(layout string) time.Duration {
	has := func(tokens ...string) bool {
		for _, tk := range tokens {
			if strings.Contains(layout, tk) {
				return true
			}
		}
		return false
	}

	// Two-digit forms first: they are unambiguous. Single-digit forms are
	// only considered afterwards, because tokens like "15" contain "5".
	if has("05") {
		return time.Second
	}
	if has("04") {
		return time.Minute
	}
	if has("15", "03", "PM", "pm") {
		return time.Hour
	}
	if has("5") {
		return time.Second
	}
	if has("4") {
		return time.Minute
	}
	if has("3") {
		return time.Hour
	}
	return 24 * time.Hour
}

// stampOf returns the DateLayout-truncated time for now. Consecutive calls
// within the same interval are served from a cache, which keeps the per-write
// cost near zero.
func (n *Namer) stampOf(now time.Time) time.Time {
	n.cacheMu.Lock()
	if n.cacheValid && !now.Before(n.cacheStart) && now.Before(n.cacheEnd) {
		stamp := n.cacheStart
		n.cacheMu.Unlock()
		return stamp
	}
	n.cacheMu.Unlock()

	stamp := n.truncateToLayout(now)

	n.cacheMu.Lock()
	n.cacheStart = stamp
	n.cacheEnd = stamp.Add(n.granularity)
	n.cacheValid = true
	n.cacheMu.Unlock()
	return stamp
}

// FileName renders the file name (without Dir) for the given time and
// in-day sequence number. seq <= 0 selects NamePattern; seq > 0 selects
// RotatedNamePattern.
func (n *Namer) FileName(t time.Time, seq int) string {
	if seq <= 0 {
		return n.render(n.cfg.NamePattern, t, 0)
	}
	return n.render(n.cfg.RotatedNamePattern, t, seq)
}

// FilePath renders the full path of a log file.
func (n *Namer) FilePath(t time.Time, seq int) string {
	return n.cfg.Dir + string(os.PathSeparator) + n.FileName(t, seq)
}

// dateKey renders the date part used both in file names and for detecting
// date-based rotation.
func (n *Namer) dateKey(t time.Time) string {
	return t.Format(n.cfg.DateLayout)
}

// truncateToLayout truncates t to the granularity of DateLayout, so that
// rotation can detect day (or period) changes reliably.
func (n *Namer) truncateToLayout(t time.Time) time.Time {
	if n.fastDay {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	}
	s := n.dateKey(t)
	if tt, err := time.ParseInLocation(n.cfg.DateLayout, s, t.Location()); err == nil {
		return tt
	}
	return t
}

// render substitutes the supported placeholders into pattern.
func (n *Namer) render(pattern string, t time.Time, seq int) string {
	var sb strings.Builder
	for {
		i := strings.IndexByte(pattern, '{')
		if i < 0 {
			sb.WriteString(pattern)
			break
		}
		j := strings.IndexByte(pattern[i:], '}')
		if j < 0 {
			sb.WriteString(pattern)
			break
		}
		sb.WriteString(pattern[:i])
		switch name := pattern[i+1 : i+j]; name {
		case "base":
			sb.WriteString(n.cfg.BaseName)
		case "date":
			sb.WriteString(n.dateKey(t))
		case "service":
			sb.WriteString(n.service)
		case "app":
			// {app} is the bare application name, without the port. It lets
			// the file name carry just the AppName (e.g. playurl) instead of
			// the "<name><port>" form rendered by {service}.
			sb.WriteString(n.cfg.ServiceName)
		case "seq":
			sb.WriteString(seqName(seq))
		case "pid":
			sb.WriteString(n.pid)
		default:
			sb.WriteString(pattern[i : i+j+1])
		}
		pattern = pattern[i+j+1:]
	}
	return cleanupName(sb.String())
}

// cleanupName removes artifacts (like "..") produced by empty placeholders,
// e.g. when {service} is empty.
func cleanupName(name string) string {
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	name = strings.Trim(name, ".")
	if name == "" {
		name = "sllogger.log"
	}
	return name
}

// seqName formats the sequence number with at least two digits: 1 -> "01".
func seqName(seq int) string {
	s := strconv.Itoa(seq)
	if len(s) < 2 {
		s = "0" + s
	}
	return s
}

// trailingSeq reports whether the last segment is a positive in-day sequence
// number. At least two segments are required so that a purely numeric date
// segment (e.g. DateLayout "20060102") is never mistaken for a sequence.
func trailingSeq(segments []string) (int, bool) {
	if len(segments) < 2 {
		return 0, false
	}
	if seq, err := strconv.Atoi(segments[len(segments)-1]); err == nil && seq > 0 {
		return seq, true
	}
	return 0, false
}

// parsedFile is the result of parsing a managed log file name.
type parsedFile struct {
	path  string
	date  time.Time
	seq   int
	size  int64
	mtime time.Time
}

// parseFileName parses a file name that follows this writer's naming rules:
//
//	{base}.{date}[.{service}|.{app}][.{seq}].log
//
// It tolerates custom NamePattern/RotatedNamePattern as long as base, date,
// service/app and seq are separate dot-separated segments. When ServiceName is
// set, both "<ServiceName><ServicePort>" ({service}) and "<ServiceName>"
// ({app}) are accepted as the anchor segment, so either naming pattern can be
// resumed and cleaned up. The returned seq is 0 for the unnumbered file.
func (n *Namer) parseFileName(name string, fi os.FileInfo) (parsedFile, bool) {
	var pf parsedFile
	if !strings.HasPrefix(name, n.cfg.BaseName+".") || !strings.HasSuffix(name, ".log") {
		return pf, false
	}
	middle := name[len(n.cfg.BaseName)+1 : len(name)-len(".log")]
	if middle == "" {
		return pf, false
	}

	segments := strings.Split(middle, ".")

	// Optional trailing seq, for patterns with seq last
	// (...{base}.{date}.{service}.{seq}.log).
	if seq, ok := trailingSeq(segments); ok {
		pf.seq = seq
		segments = segments[:len(segments)-1]
	}

	// Optional service/app anchor. Both the "<name><port>" form ({service})
	// and the bare "<name>" form ({app}) are accepted.
	if n.service != "" || n.cfg.ServiceName != "" {
		if len(segments) < 2 {
			return pf, false
		}
		last := segments[len(segments)-1]
		if last != n.service && last != n.cfg.ServiceName {
			return pf, false
		}
		segments = segments[:len(segments)-1]

		// Patterns with seq before the anchor
		// (...{base}.{date}.{seq}.{app}.log) leave seq trailing now.
		if seq, ok := trailingSeq(segments); ok {
			pf.seq = seq
			segments = segments[:len(segments)-1]
		}
	}

	if len(segments) != 1 {
		return pf, false
	}

	date, err := time.ParseInLocation(n.cfg.DateLayout, segments[0], time.Local)
	if err != nil {
		return pf, false
	}
	pf.date = date
	pf.path = n.cfg.Dir + string(os.PathSeparator) + name
	if fi != nil {
		pf.size = fi.Size()
		pf.mtime = fi.ModTime()
	}
	return pf, true
}
