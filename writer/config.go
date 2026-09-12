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

// Package writer implements the file layer of sllogger: a self-managed
// RollingWriter with size/time/date rotation, historical file cleanup and an
// optional async batch writer.
//
// 文件命名规则（默认）：
//
//	{Dir}/{BaseName}.{date}.{service}.log          当日第一个文件
//	{Dir}/{BaseName}.{date}.{service}.{seq}.log    滚动后的文件（seq 从 01 开始）
//
// 占位符：{base} {date} {service} {app} {seq} {pid}。其中 {service} 为
// "<ServiceName><ServicePort>"，{app} 为不带端口的 ServiceName。
//
// 例如：
//
//	/data/logs/
//	    LOG_CALL_INFO.2026-09-03.playurl8080.log
//	    LOG_CALL_INFO.2026-09-03.playurl8080.01.log
//	    LOG_CALL_INFO.2026-09-03.playurl8080.02.log
//
// 也可按小时 + AppName 命名（见 example/calllog）：
//
//	{Dir}/LOG_CALL_INFO.{yyyy-MM-dd-HH}.{seq}.{app}.log
//	    build/logs/LOG_CALL_INFO.2026-09-11-15.playurl.log
//	    build/logs/LOG_CALL_INFO.2026-09-11-15.01.playurl.log
package writer

import (
	"os"
	"time"

	"github.com/maxhaosl/sllogger/slcore"
)

// Default configuration values.
const (
	// DefaultMaxSize is the default per-file size limit (1GB).
	DefaultMaxSize int64 = 1 << 30
	// DefaultRotationInterval is the default time-based rotation interval (1h).
	DefaultRotationInterval = time.Hour
	// DefaultQueueSize is the default async queue capacity.
	DefaultQueueSize = 10000
	// DefaultBatchSize is the default number of entries per batch write.
	DefaultBatchSize = 100
	// DefaultFlushInterval is the default async flush interval.
	DefaultFlushInterval = 100 * time.Millisecond
	// DefaultCleanupInterval is the default interval between cleanup passes.
	DefaultCleanupInterval = 10 * time.Minute
	// DefaultDateLayout is the default date part of the file name.
	DefaultDateLayout = "2006-01-02"
	// DefaultNamePattern is the pattern for the first file of a period.
	DefaultNamePattern = "{base}.{date}.{service}.log"
	// DefaultRotatedNamePattern is the pattern for rotated files.
	DefaultRotatedNamePattern = "{base}.{date}.{service}.{seq}.log"
)

// Config configures a RollingWriter (and its optional async front end).
//
// 需求映射：
//
//  1. 日志输出路径       -> Dir
//  2. 日志文件名自定义   -> BaseName / NamePattern / RotatedNamePattern / DateLayout
//  4. 单文件大小滚动     -> MaxSize
//  5. 定时滚动           -> RotationInterval（跨天/跨周期始终滚动）
//  6. 文件个数上限       -> MaxBackups
//  7. 单类型总大小上限   -> MaxTotalSize
//  8. 目录总大小上限     -> MaxDirSize
//  9. 保留时长           -> MaxAge
type Config struct {
	// Dir is the log output directory, e.g. /data/logs.
	Dir string

	// BaseName is the base file name, e.g. LOG_CALL_INFO.
	BaseName string

	// NamePattern is the file name pattern for the first file of a period.
	// Supported placeholders: {base} {date} {service} {app} {seq} {pid}.
	// {service} renders "<ServiceName><ServicePort>"; {app} renders
	// "<ServiceName>" only (no port).
	NamePattern string

	// RotatedNamePattern is the file name pattern used after rotation, when
	// the sequence number is greater than zero. The {seq} placeholder is
	// formatted with at least two digits (01, 02, ...).
	RotatedNamePattern string

	// DateLayout is the Go time layout used to render {date}, and also
	// controls the granularity of date-based rotation. The default
	// "2006-01-02" rotates on day change.
	DateLayout string

	// ServiceName and ServicePort are rendered into {service} as
	// "<ServiceName><ServicePort>" (e.g. "playurl8080").
	ServiceName string
	ServicePort int

	// MaxSize is the maximum size of a single log file in bytes. When the
	// current size plus the incoming write would exceed MaxSize, the file is
	// rotated. Zero disables size-based rotation.
	MaxSize int64

	// RotationInterval is the time-based rotation interval, e.g. 1h. Zero
	// disables interval-based rotation. Day change (per DateLayout) always
	// rotates regardless of this value.
	RotationInterval time.Duration

	// MaxBackups limits the number of kept files of this log type. The oldest
	// files are removed first. Zero disables the limit.
	MaxBackups int

	// MaxTotalSize limits the total size (bytes) of all files of this log
	// type. The oldest files are removed first. Zero disables the limit.
	MaxTotalSize int64

	// MaxDirSize limits the total size (bytes) of all *.log files under Dir.
	// The oldest files of this log type are removed first. Zero disables the
	// limit.
	MaxDirSize int64

	// MaxAge limits how long log files are kept, e.g. 24h or 72h. Files whose
	// modification time is older than now-MaxAge are removed. Zero disables
	// the limit.
	MaxAge time.Duration

	// CleanupInterval is how often a cleanup pass runs. Defaults to 10m.
	CleanupInterval time.Duration

	// Async enables the async batch writer in front of the RollingWriter.
	Async bool

	// QueueSize is the capacity of the async queue. Defaults to 10000.
	QueueSize int

	// BatchSize is the number of encoded entries per batch write. Defaults
	// to 100.
	BatchSize int

	// FlushInterval is the maximum time between batch flushes. Defaults to
	// 100ms.
	FlushInterval time.Duration

	// BlockOnFull makes the async writer block instead of dropping entries
	// when the queue is full. Call INFO/call-info level logging should use
	// the default (drop) to protect the business.
	BlockOnFull bool

	// BlockLevel enables level-aware queue-full handling (design doc #29):
	//
	//   - >= BlockLevel  : block until the queue accepts the entry
	//                      (use for ERROR, which must not be lost)
	//   - <  BlockLevel  : drop and count it
	//                      (use for CALL_INFO/INFO, to protect the business)
	//
	// It only takes effect when the sink receives the entry level, i.e. when
	// the logger writes through slcore.LevelWriteSyncer (which sllogger's
	// Config.Build sets up automatically) and BlockOnFull is false.
	//
	// Default (0) means "no level-based policy": the BlockOnFull field alone
	// decides. Set it to ErrorLevel to get the recommended policy.
	BlockLevel Level

	// Shards splits the async queue into N independent queues, each with its
	// own worker goroutine, so that many concurrent producers stop contending
	// on a single channel.
	//
	//   1 (default): single queue + single worker; entries are written in
	//     enqueue order process-wide.
	//   >1: higher throughput under many producers, but entries from
	//     different shards may interleave out of order. Ordering across
	//     goroutines was never guaranteed, so this is usually acceptable for
	//     logs; keep 1 if strict process-wide ordering is required.
	//
	// The queue capacity is divided evenly among shards, so total buffering
	// stays the same. Sync waits for all shards.
	Shards int

	// Clock allows injecting a custom clock for tests. Defaults to the
	// system clock.
	Clock slcore.Clock

	// FileMode is the permission bits applied when a log file is created.
	// Defaults to 0o600 (owner read/write only) so logs are not world- or
	// group-readable. Set to e.g. 0o640 or 0o644 if other accounts must read
	// them. Zero means "use the default".
	FileMode os.FileMode
}

// Level is an alias for slcore.Level so callers can configure BlockLevel
// without importing slcore.
type Level = slcore.Level

// withDefaults returns a normalized copy of the config.
func (c *Config) withDefaults() *Config {
	cp := *c
	if cp.NamePattern == "" {
		cp.NamePattern = DefaultNamePattern
	}
	if cp.RotatedNamePattern == "" {
		cp.RotatedNamePattern = DefaultRotatedNamePattern
	}
	if cp.DateLayout == "" {
		cp.DateLayout = DefaultDateLayout
	}
	if cp.MaxSize == 0 {
		cp.MaxSize = DefaultMaxSize
	}
	// RotationInterval == 0 表示禁用定时(interval)滚动，仅按 DateLayout(小时)与
	// 文件大小滚动；非 0（如 1h）才启用定时滚动。此处不再强制默认 1h。
	if cp.RotationInterval == 0 {
		cp.RotationInterval = 0
	}
	if cp.CleanupInterval <= 0 {
		cp.CleanupInterval = DefaultCleanupInterval
	}
	if cp.QueueSize <= 0 {
		cp.QueueSize = DefaultQueueSize
	}
	if cp.BatchSize <= 0 {
		cp.BatchSize = DefaultBatchSize
	}
	if cp.FlushInterval <= 0 {
		cp.FlushInterval = DefaultFlushInterval
	}
	if cp.Clock == nil {
		cp.Clock = slcore.DefaultClock
	}
	if cp.FileMode == 0 {
		cp.FileMode = 0o600
	}
	return &cp
}

// service renders the {service} placeholder value.
func (c *Config) service() string {
	if c.ServiceName == "" && c.ServicePort == 0 {
		return ""
	}
	return c.ServiceName + itoa(c.ServicePort)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
