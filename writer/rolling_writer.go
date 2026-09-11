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
	"sync"
	"time"
)

// ErrClosed is returned when writing to a closed writer.
var ErrClosed = errors.New("github.com/maxhaosl/sllogger/writer: writer is closed")

// RollingWriter writes logs to local files and rotates them by date (per
// DateLayout), by configured interval (e.g. hourly) and by size.
//
// 并发模型：Write/Rotate/Sync/Close 之间通过互斥锁保证原子性（设计文档第
// 25 节）。当 RollingWriter 作为 AsyncWriter 的下游时，只有单个 worker
// goroutine 调用 Write，锁竞争可以忽略。
type RollingWriter struct {
	mu      sync.Mutex
	cfg     *Config
	namer   *Namer
	cleaner *Cleaner
	metrics Metrics

	file     *os.File
	path     string
	size     int64
	stamp    time.Time // truncated to DateLayout granularity, rotates on change
	openTime time.Time // when the current file was opened (interval rotation)
	seq      int       // in-day sequence number, 0 = first file of the day
	closed   bool

	stopCleaner chan struct{}
	cleanerWG   sync.WaitGroup
	closeOnce   sync.Once
}

// NewRollingWriter creates a RollingWriter and opens the first log file.
//
// 启动时会扫描日志目录：如果当日的日志文件未满，则继续追加写入该文件并沿
// 用其序号；否则从下一个序号开新文件，避免覆盖历史日志。
func NewRollingWriter(cfg *Config) (*RollingWriter, error) {
	cfg = cfg.withDefaults()
	if cfg.Dir == "" {
		return nil, errors.New("github.com/maxhaosl/sllogger/writer: Dir is required")
	}
	if cfg.BaseName == "" {
		return nil, errors.New("github.com/maxhaosl/sllogger/writer: BaseName is required")
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}

	w := &RollingWriter{
		cfg:         cfg,
		namer:       newNamer(cfg),
		stopCleaner: make(chan struct{}),
	}
	w.cleaner = newCleaner(cfg, w.namer, &w.metrics)
	// Cleanup must never delete the file being written to.
	w.cleaner.active = func() string {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.path
	}

	now := cfg.Clock.Now()
	stamp := w.namer.stampOf(now)
	seq, size, resume := w.findResumeFile(stamp)
	if !resume {
		size = 0
	}
	if err := w.openFile(now, stamp, int(seq), size, resume); err != nil {
		return nil, err
	}

	// Start periodic cleanup (requirements 6/7/8/9).
	w.cleanerWG.Add(1)
	go w.cleanupLoop()

	return w, nil
}

// Config returns the normalized configuration in use.
func (w *RollingWriter) Config() *Config {
	return w.cfg
}

// ConcurrentSafe reports that RollingWriter is safe for concurrent use: every
// mutating operation is guarded by an internal mutex. Callers (Config.Build)
// use this to avoid wrapping it in a redundant outer lock.
func (w *RollingWriter) ConcurrentSafe() bool { return true }

// Metrics returns the writer metrics.
func (w *RollingWriter) Metrics() *Metrics {
	return &w.metrics
}

// Write implements io.Writer. Before every write it checks, in order:
//
//  1. date change (per DateLayout)  -> rotate, seq resets to 0
//  2. interval elapsed              -> rotate, seq increments
//  3. size limit would be exceeded  -> rotate, seq increments
func (w *RollingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, ErrClosed
	}

	// A single call may carry a large batch (the async writer flushes many
	// entries at once), so the payload is split at line boundaries and
	// rotated between chunks. Without this, one batch would push a file far
	// beyond MaxSize.
	var written int
	for len(p) > 0 {
		now := w.cfg.Clock.Now()
		stamp := w.namer.stampOf(now)

		switch {
		case !stamp.Equal(w.stamp):
			// Date change: the file name contains the date, so it must
			// rotate and reset the sequence number (design doc #24).
			if err := w.rotateLocked(now, stamp, 0); err != nil {
				return written, err
			}
		case w.cfg.RotationInterval > 0 && now.Sub(w.openTime) >= w.cfg.RotationInterval:
			// Time-based rotation (design doc #23).
			if err := w.rotateLocked(now, stamp, w.seq+1); err != nil {
				return written, err
			}
		case w.cfg.MaxSize > 0 && w.size >= w.cfg.MaxSize:
			// The previous chunk filled the file (design doc #22).
			if err := w.rotateLocked(now, stamp, w.seq+1); err != nil {
				return written, err
			}
		}

		chunk := p
		if w.cfg.MaxSize > 0 {
			remain := w.cfg.MaxSize - w.size
			if remain <= 0 {
				// The current file is full: move on to a new one.
				if err := w.rotateLocked(now, stamp, w.seq+1); err != nil {
					return written, err
				}
				continue
			}
			if int64(len(chunk)) > remain {
				cut := lastLineEnd(chunk, remain)
				switch {
				case cut > 0:
					// Cut at the last complete line inside the remaining
					// space so log lines are never split across files.
					chunk = chunk[:cut]
				case w.size > 0:
					// A whole entry does not fit in the remaining space:
					// start a new file instead of overshooting this one.
					if err := w.rotateLocked(now, stamp, w.seq+1); err != nil {
						return written, err
					}
					continue
				default:
					// Empty file and a single entry larger than MaxSize:
					// write it whole, otherwise we would loop forever.
				}
			}
		}

		n, err := w.file.Write(chunk)
		if n > 0 {
			w.size += int64(n)
			written += n
		}
		if err != nil {
			w.metrics.WriteErrors.Add(1)
			return written, err
		}
		if n == 0 {
			break
		}
		p = p[n:]
	}

	w.metrics.Written.Add(1)
	return written, nil
}

// lastLineEnd returns the length of the longest prefix of p that is at most
// limit bytes long and ends at a line boundary. It returns 0 when no such
// prefix exists.
func lastLineEnd(p []byte, limit int64) int {
	if limit > int64(len(p)) {
		limit = int64(len(p))
	}
	for i := int(limit) - 1; i >= 0; i-- {
		if p[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// Sync flushes file data to disk.
func (w *RollingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.file == nil {
		return nil
	}
	return w.file.Sync()
}

// Rotate closes the current file and opens a new one with the next sequence
// number.
func (w *RollingWriter) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	now := w.cfg.Clock.Now()
	return w.rotateLocked(now, w.namer.stampOf(now), w.seq+1)
}

// Close flushes, closes the current file, stops the cleanup loop and performs
// a final cleanup pass. Close is idempotent.
func (w *RollingWriter) Close() error {
	var err error
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		if w.file != nil {
			_ = w.file.Sync()
			err = w.file.Close()
			w.file = nil
		}
		close(w.stopCleaner)
		w.mu.Unlock()

		w.cleanerWG.Wait()
		w.cleaner.Clean()
	})
	return err
}

// rotateLocked switches to a new file. The caller must hold w.mu.
//
// The outgoing file is closed but not fsync'ed: rotation can happen very
// often under load, and an fsync per rotation would dominate throughput.
// Durability is provided by Sync() (which callers can schedule) and by
// Close(), both of which do fsync.
func (w *RollingWriter) rotateLocked(now, stamp time.Time, seq int) error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	return w.openFile(now, stamp, seq, 0, false)
}

// openFile opens the log file for (stamp, seq) and initializes state.
func (w *RollingWriter) openFile(now, stamp time.Time, seq int, initialSize int64, resume bool) error {
	path := w.namer.FilePath(stamp, seq)

	var (
		file *os.File
		size int64
		err  error
	)
	if resume {
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			size = initialSize
		}
	}
	if file == nil {
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	}
	if err != nil {
		return err
	}
	if fi, statErr := file.Stat(); statErr == nil {
		size = fi.Size()
	}

	w.file = file
	w.path = path
	w.size = size
	w.stamp = stamp
	w.openTime = now
	w.seq = seq
	w.metrics.RotateCount.Add(1)
	return nil
}

// findResumeFile scans the log directory for today's files and decides
// whether the newest one can be resumed (appended to).
func (w *RollingWriter) findResumeFile(stamp time.Time) (seq int, size int64, resume bool) {
	entries, err := os.ReadDir(w.cfg.Dir)
	if err != nil {
		return 0, 0, false
	}
	var (
		maxSeq   int
		maxSize  int64
		foundAny bool
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, statErr := e.Info()
		if statErr != nil {
			continue
		}
		pf, ok := w.namer.parseFileName(e.Name(), fi)
		if !ok || pf.date != stamp {
			continue
		}
		if !foundAny || pf.seq > maxSeq {
			maxSeq = pf.seq
			maxSize = pf.size
			foundAny = true
		}
	}
	if !foundAny {
		return 0, 0, false
	}
	if w.cfg.MaxSize <= 0 || maxSize < w.cfg.MaxSize {
		return maxSeq, maxSize, true
	}
	return maxSeq + 1, 0, false
}

// cleanupLoop runs periodic cleanup passes until Close.
func (w *RollingWriter) cleanupLoop() {
	defer w.cleanerWG.Done()
	ticker := w.cfg.Clock.NewTicker(w.cfg.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCleaner:
			return
		case <-ticker.C:
			w.cleaner.Clean()
		}
	}
}
