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

import "sync/atomic"

// Metrics tracks runtime counters of the writer layer. All fields are
// atomically updated and safe for concurrent use.
type Metrics struct {
	// Enqueued is the number of entries accepted by the async queue.
	Enqueued atomic.Uint64
	// Written is the number of write calls issued to the file.
	Written atomic.Uint64
	// Dropped is the number of entries dropped because the queue was full.
	Dropped atomic.Uint64
	// WriteErrors is the number of file write errors.
	WriteErrors atomic.Uint64
	// RotateCount is the number of performed rotations.
	RotateCount atomic.Uint64
	// RemovedFiles is the number of files removed by cleanup.
	RemovedFiles atomic.Uint64
}

// MetricsSnapshot is a point-in-time copy of Metrics counters.
//
// QueueSize and QueueDepth are filled in by AsyncWriter.Snapshot (design
// doc #30); they stay zero for a bare RollingWriter.
type MetricsSnapshot struct {
	Enqueued     uint64
	Written      uint64
	Dropped      uint64
	WriteErrors  uint64
	RotateCount  uint64
	RemovedFiles uint64

	// QueueSize is the total queue capacity across all shards.
	QueueSize uint64
	// QueueDepth is the number of entries currently buffered.
	QueueDepth uint64
}

// Snapshot returns a copy of the current counters.
//
// AsyncWriter overrides this to include queue metrics; RollingWriter's own
// snapshot leaves them at zero.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Enqueued:     m.Enqueued.Load(),
		Written:      m.Written.Load(),
		Dropped:      m.Dropped.Load(),
		WriteErrors:  m.WriteErrors.Load(),
		RotateCount:  m.RotateCount.Load(),
		RemovedFiles: m.RemovedFiles.Load(),
	}
}
