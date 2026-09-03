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
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"sllogger/slcore"
)

// writeSyncCloser is the minimal interface AsyncWriter needs from its
// downstream writer.
type writeSyncCloser interface {
	slcore.WriteSyncer
	io.Closer
}

// AsyncWriter decouples business goroutines from file IO (design doc #26):
//
//	Business ──> queue ──> worker ──> RollingWriter ──> File
//
// 多生产者 + 消费者。默认（Shards=1）为单消费者，同进程内保序；
// Shards>1 时按分片并发消费以突破单消费者吞吐上限，代价是跨分片不保序。
//
// 队列满时默认丢弃并计数（业务稳定性优先），可通过 BlockOnFull 改为阻塞。
type AsyncWriter struct {
	// shards 是分片队列。Shards=1 时只有一个元素，行为与早期版本一致。
	shards []chan []byte
	// next 用于 round-robin 路由生产者到分片。atomic 自增的成本远低于
	// 多生产者争抢单个 channel。
	next atomic.Uint64

	inner   writeSyncCloser
	cfg     *Config
	metrics Metrics

	// Entry buffers are pooled: Write is on the hot path and must not
	// allocate on every call. The worker returns the buffer to the pool as
	// soon as it has copied the bytes into the current batch.
	pool sync.Pool

	done   chan struct{}
	wg     sync.WaitGroup
	syncCh chan chan struct{}

	// closed is atomic so Write/Sync never contend on a mutex.
	closed    atomic.Bool
	closeOnce sync.Once

	// inFlight counts writers that have passed the closed check and may
	// still enqueue. Close waits for it to reach zero before draining the
	// leftovers, which guarantees that "Write returned nil" implies the
	// entry is handed to a worker (and therefore eventually persisted).
	inFlight atomic.Int64
}

// ConcurrentSafe reports that AsyncWriter is safe for concurrent use, so
// callers (Config.Build) can skip wrapping it in another mutex layer.
// AsyncWriter serializes all writes through worker goroutines and channels,
// which already provides the necessary synchronization.
func (a *AsyncWriter) ConcurrentSafe() bool { return true }

// NewAsyncWriter wraps inner with an async batch queue and starts the worker
// goroutine(s).
func NewAsyncWriter(inner writeSyncCloser, cfg *Config) *AsyncWriter {
	cfg = cfg.withDefaults()
	shards := cfg.Shards
	if shards < 1 {
		shards = 1
	}
	// 每个分片均分队列容量，总分容量与单队列时保持一致。
	per := cfg.QueueSize / shards
	if per < 1 {
		per = 1
	}
	qs := make([]chan []byte, shards)
	for i := range qs {
		qs[i] = make(chan []byte, per)
	}

	a := &AsyncWriter{
		shards: qs,
		inner:  inner,
		cfg:    cfg,
		done:   make(chan struct{}),
		syncCh: make(chan chan struct{}),
	}
	a.pool.New = func() any {
		buf := make([]byte, 0, initialEntryCap)
		return &buf
	}
	// 每个分片启动一个 worker。Shards=1 时即传统的单消费者。
	a.wg.Add(len(a.shards))
	for i := range a.shards {
		go a.run(a.shards[i])
	}
	return a
}

// initialEntryCap is the initial capacity of pooled entry buffers.
const initialEntryCap = 512

// Metrics returns the async writer metrics.
func (a *AsyncWriter) Metrics() *Metrics {
	return &a.metrics
}

// WriteLevel implements slcore.LevelWriteSyncer.
//
// It applies the level-aware queue-full policy (design doc #29): entries at
// or above BlockLevel block until accepted, while lower-severity entries are
// dropped and counted.
func (a *AsyncWriter) WriteLevel(lvl slcore.Level, p []byte) (int, error) {
	// A zero BlockLevel means "no level policy": defer to BlockOnFull.
	if a.cfg.BlockLevel == 0 || a.cfg.BlockOnFull {
		return a.Write(p)
	}
	if lvl >= a.cfg.BlockLevel {
		return a.writeBlocking(p)
	}
	// Low-severity entries must never block the business.
	return a.Write(p)
}

// writeBlocking enqueues p, waiting until the queue accepts it.
func (a *AsyncWriter) writeBlocking(p []byte) (int, error) {
	if a.closed.Load() {
		return 0, ErrClosed
	}

	a.inFlight.Add(1)
	defer a.inFlight.Add(-1)
	if a.closed.Load() {
		return 0, ErrClosed
	}

	bp := a.pool.Get().(*[]byte)
	buf := (*bp)[:0]
	buf = append(buf, p...)

	queue := a.shards[a.next.Add(1)%uint64(len(a.shards))]

	select {
	case queue <- buf:
		a.metrics.Enqueued.Add(1)
		return len(p), nil
	case <-a.done:
		a.pool.Put(bp)
		return 0, ErrClosed
	}
}

// Snapshot returns a copy of the counters, including queue metrics
// (design doc #30).
func (a *AsyncWriter) Snapshot() MetricsSnapshot {
	snap := a.metrics.Snapshot()
	snap.QueueSize = a.QueueCapacity()
	snap.QueueDepth = a.QueueDepth()
	return snap
}

// QueueDepth returns the number of entries currently buffered across all
// shards. It is exposed for monitoring (design doc #30).
func (a *AsyncWriter) QueueDepth() uint64 {
	var depth uint64
	for _, q := range a.shards {
		depth += uint64(len(q))
	}
	return depth
}

// QueueCapacity returns the total configured queue capacity across all shards.
func (a *AsyncWriter) QueueCapacity() uint64 {
	var cap uint64
	for _, q := range a.shards {
		cap += uint64(cap_(q))
	}
	return cap
}

// cap_ is a small helper to keep QueueCapacity readable.
func cap_(q chan []byte) int { return cap(q) }

// Write enqueues a copy of p for async writing. It never blocks unless
// BlockOnFull is enabled; when the queue is full, the entry is dropped and
// counted.
func (a *AsyncWriter) Write(p []byte) (int, error) {
	if a.closed.Load() {
		return 0, ErrClosed
	}

	// Register as an in-flight writer so Close cannot drain the queues while
	// we are about to enqueue. The second closed check closes the window
	// between the first check and this registration.
	a.inFlight.Add(1)
	defer a.inFlight.Add(-1)
	if a.closed.Load() {
		return 0, ErrClosed
	}

	// Copy into a pooled buffer: the caller (encoder buffer) may reuse the
	// memory as soon as Write returns.
	bp := a.pool.Get().(*[]byte)
	buf := (*bp)[:0]
	buf = append(buf, p...)

	// Round-robin route producers across shards. An atomic increment is far
	// cheaper than contending on a single channel, which was the dominant
	// cost under many concurrent producers.
	//
	// Trade-off: with Shards>1, entries from different shards may be written
	// out of order (ordering across goroutines was never guaranteed anyway).
	// Shards=1 keeps the original single-queue ordering.
	queue := a.shards[a.next.Add(1)%uint64(len(a.shards))]

	if a.cfg.BlockOnFull {
		select {
		case queue <- buf:
			a.metrics.Enqueued.Add(1)
		case <-a.done:
			a.pool.Put(bp)
			return 0, ErrClosed
		}
		return len(p), nil
	}

	select {
	case queue <- buf:
		a.metrics.Enqueued.Add(1)
	default:
		a.metrics.Dropped.Add(1)
		a.pool.Put(bp)
	}
	return len(p), nil
}

// Sync flushes all pending queued entries and the underlying writer.
//
// With multiple shards, Sync waits for every worker to drain its own shard,
// so it is a full barrier across all queues.
func (a *AsyncWriter) Sync() error {
	if a.closed.Load() {
		return nil
	}

	// Ask every worker to drain and flush its shard.
	resps := make([]chan struct{}, 0, len(a.shards))
	for range a.shards {
		resp := make(chan struct{})
		select {
		case a.syncCh <- resp:
			resps = append(resps, resp)
		case <-a.done:
			return nil
		}
	}
	for _, resp := range resps {
		select {
		case <-resp:
		case <-a.done:
			return nil
		}
	}

	// Only one worker needs to fsync the shared downstream writer, but doing
	// it here (after all workers have flushed) guarantees durability of
	// everything enqueued before the call.
	return a.inner.Sync()
}

// Close stops accepting writes, drains the queue, flushes pending entries and
// closes the underlying writer. Close is idempotent.
func (a *AsyncWriter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		a.closed.Store(true)

		close(a.done)

		// Wait for in-flight writers before draining: a writer that already
		// passed the closed check may still enqueue (its select can pick
		// `queue <- buf` over `<-a.done`). Draining before that happens would
		// leave such an entry behind even though Write returned nil.
		for a.inFlight.Load() != 0 {
			runtime.Gosched()
		}

		a.wg.Wait()

		// 兜底：worker 退出后，队列里可能残留"关闭瞬间入队成功"的条目。
		// 此时已无并发写入者，排空是安全的。
		a.drainRemainingToInner()

		err = a.inner.Close()
	})
	return err
}

// drainRemainingToInner 非阻塞地排空所有分片队列，并把残留数据一次性写入
// 下游。没有残留时不产生任何写入，因此正常路径无额外开销。
func (a *AsyncWriter) drainRemainingToInner() {
	var batch []byte
	// 反复扫描，直到某一轮所有分片都取不到数据：这样即使取空后又有
	// 条目入队，也能被收走。
	for {
		got := false
		for _, q := range a.shards {
			for drained := false; !drained; {
				select {
				case b := <-q:
					batch = append(batch, b...)
					got = true
				default:
					drained = true
				}
			}
		}
		if !got {
			break
		}
	}
	if len(batch) == 0 {
		return
	}
	if _, err := a.inner.Write(batch); err != nil {
		a.metrics.WriteErrors.Add(1)
	}
}

// run is a worker goroutine consuming one shard queue. It batches queued
// entries and writes them in one shot whenever the batch is full or the flush
// interval elapses.
//
// Workers share the downstream writer, which is internally synchronized
// (RollingWriter guards every operation with a mutex), and each flush is a
// large batch write, so contention stays low.
func (a *AsyncWriter) run(queue chan []byte) {
	defer a.wg.Done()

	ticker := a.cfg.Clock.NewTicker(a.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]byte, 0, 64*1024)
	count := 0

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, err := a.inner.Write(batch); err != nil {
			a.metrics.WriteErrors.Add(1)
		}
		batch = batch[:0]
		count = 0
	}

	for {
		select {
		case <-a.done:
			// Graceful shutdown: drain the queue, then flush (design doc #33).
			a.drain(queue, &batch, &count)
			for {
				select {
				case b := <-queue:
					batch = append(batch, b...)
					count++
					a.recycle(&b)
				default:
					flush()
					return
				}
			}
		case b := <-queue:
			batch = append(batch, b...)
			count++
			a.recycle(&b)
			// Batch drain: after waking up, take everything currently queued
			// in one go instead of returning to select for every entry.
			//
			// Under many concurrent producers, one-entry-per-wakeup makes the
			// worker bounce between select and the channel constantly, which
			// showed up as ~71% of CPU time in the scheduler
			// (usleep/pthread_cond_wait/signal). Draining amortizes the
			// wake-up cost and keeps producers running instead of parking.
			a.drain(queue, &batch, &count)
			if count >= a.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case resp := <-a.syncCh:
			// Sync must guarantee that everything enqueued so far is on
			// disk, so drain the queue before flushing. The fsync itself is
			// done once by Sync() after all workers have flushed.
			a.drain(queue, &batch, &count)
			flush()
			close(resp)
		}
	}
}

// drain moves everything currently queued into the batch without blocking,
// recycling each entry buffer as it goes. It also counts the entries so the
// caller can decide when to flush.
func (a *AsyncWriter) drain(queue chan []byte, batch *[]byte, count *int) {
	for {
		select {
		case b := <-queue:
			*batch = append(*batch, b...)
			*count++
			a.recycle(&b)
		default:
			return
		}
	}
}

// recycle returns an entry buffer to the pool. The worker calls it right
// after copying the bytes into the batch, so the buffer is never referenced
// again.
func (a *AsyncWriter) recycle(b *[]byte) {
	*b = (*b)[:0]
	a.pool.Put(b)
}

// ensure interface compatibility.
var (
	_ slcore.WriteSyncer      = (*AsyncWriter)(nil)
	_ slcore.LevelWriteSyncer = (*AsyncWriter)(nil)
	_ slcore.WriteSyncer      = (*RollingWriter)(nil)
	_ writeSyncCloser         = (*RollingWriter)(nil)
	_ writeSyncCloser         = (*AsyncWriter)(nil)
	_ error                   = ErrClosed
)
