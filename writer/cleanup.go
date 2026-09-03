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
	"sort"
	"sync/atomic"
)

// Cleaner implements the lifecycle management of historical log files
// (design doc #32):
//
//	MaxAge         按保留时长清理（需求9，如 1 天 / 3 天）
//	MaxBackups     按文件个数清理（需求6）
//	MaxTotalSize   按本类型总大小清理（需求7）
//	MaxDirSize     按目录总大小清理（需求8，只删除本类型文件）
//
// 清理触发时机：定时（CleanupInterval）、RollingWriter.Close 时。
type Cleaner struct {
	cfg      *Config
	namer    *Namer
	metrics  *Metrics
	inFlight atomic.Bool

	// active reports the file currently being written to. It must never be
	// removed by cleanup, otherwise the writer would keep appending to a
	// deleted inode.
	active func() string
}

func newCleaner(cfg *Config, namer *Namer, metrics *Metrics) *Cleaner {
	return &Cleaner{cfg: cfg, namer: namer, metrics: metrics}
}

// isActive reports whether path is the file currently being written to.
func (c *Cleaner) isActive(path string) bool {
	if c.active == nil {
		return false
	}
	return c.active() == path
}

// Clean runs one cleanup pass. Concurrent calls are coalesced: only one pass
// runs at a time, others are skipped (they will happen on the next tick).
func (c *Cleaner) Clean() {
	if !c.inFlight.CompareAndSwap(false, true) {
		return
	}
	defer c.inFlight.Store(false)

	files := c.scan()
	if len(files) == 0 {
		return
	}
	now := c.cfg.Clock.Now()

	// Requirement 9: age-based removal.
	if c.cfg.MaxAge > 0 {
		deadline := now.Add(-c.cfg.MaxAge)
		var kept []parsedFile
		for _, f := range files {
			if f.mtime.Before(deadline) && !c.isActive(f.path) {
				c.remove(f.path)
				continue
			}
			kept = append(kept, f)
		}
		files = kept
	}

	// Oldest first: date ascending, then seq ascending.
	sort.Slice(files, func(i, j int) bool {
		if !files[i].date.Equal(files[j].date) {
			return files[i].date.Before(files[j].date)
		}
		return files[i].seq < files[j].seq
	})

	// Requirement 6: count-based removal. The active file always survives, so
	// the effective number of kept files is MaxBackups plus the active one.
	if c.cfg.MaxBackups > 0 {
		var removable, keep []parsedFile
		for _, f := range files {
			if c.isActive(f.path) {
				keep = append(keep, f)
			} else {
				removable = append(removable, f)
			}
		}
		if len(files) > c.cfg.MaxBackups {
			excess := len(files) - c.cfg.MaxBackups
			if excess > len(removable) {
				excess = len(removable)
			}
			for _, f := range removable[:excess] {
				c.remove(f.path)
			}
			removable = removable[excess:]
		}
		files = append(removable, keep...)
		sort.Slice(files, func(i, j int) bool {
			if !files[i].date.Equal(files[j].date) {
				return files[i].date.Before(files[j].date)
			}
			return files[i].seq < files[j].seq
		})
	}

	// Requirement 7: total size of this log type.
	if c.cfg.MaxTotalSize > 0 {
		files = c.removeUntilTotal(files, c.cfg.MaxTotalSize)
	}

	// Requirement 8: total size of all log files under Dir. Only files of
	// this log type are ever removed (from the oldest), so other log types
	// are never touched.
	if c.cfg.MaxDirSize > 0 {
		excess := c.dirLogTotal() - c.cfg.MaxDirSize
		if excess > 0 {
			var kept []parsedFile
			removed := int64(0)
			for _, f := range files {
				if removed < excess && !c.isActive(f.path) {
					removed += f.size
					c.remove(f.path)
					continue
				}
				kept = append(kept, f)
			}
			files = kept
		}
	}
}

// removeUntilTotal removes the oldest files until the total size of the
// remaining files fits the limit.
func (c *Cleaner) removeUntilTotal(files []parsedFile, limit int64) []parsedFile {
	var total int64
	for _, f := range files {
		total += f.size
	}
	if total <= limit {
		return files
	}
	idx := 0
	kept := files[:0]
	for _, f := range files {
		if total > limit && !c.isActive(f.path) {
			total -= f.size
			c.remove(f.path)
			continue
		}
		kept = append(kept, f)
	}
	for i := len(kept); i < len(files); i++ {
		files[i] = parsedFile{}
	}
	_ = idx
	return kept
}

// dirLogTotal sums the sizes of all *.log files under Dir.
func (c *Cleaner) dirLogTotal() int64 {
	entries, err := os.ReadDir(c.cfg.Dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !hasSuffix(e.Name(), ".log") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// scan lists the files of this log type under Dir.
func (c *Cleaner) scan() []parsedFile {
	entries, err := os.ReadDir(c.cfg.Dir)
	if err != nil {
		return nil
	}
	var files []parsedFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, statErr := e.Info()
		if statErr != nil {
			continue
		}
		if pf, ok := c.namer.parseFileName(e.Name(), fi); ok {
			files = append(files, pf)
		}
	}
	return files
}

func (c *Cleaner) remove(path string) {
	if err := os.Remove(path); err == nil {
		c.metrics.RemovedFiles.Add(1)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
