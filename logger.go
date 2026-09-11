// Copyright (c) 2016 Uber Technologies, Inc.
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

// Derived from go.uber.org/zap/logger.go.
package sllogger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/internal/bufferpool"
	"github.com/maxhaosl/sllogger/slcore"
)

// A Logger provides fast, leveled, structured logging. All methods are safe
// for concurrent use.
//
// The Logger is designed for contexts in which every microsecond and every
// allocation matters, so its API intentionally favors performance and type
// safety over brevity. For most applications, the SugaredLogger strikes a
// better balance between performance and ergonomics.
type Logger struct {
	core slcore.Core

	development bool
	addCaller   bool
	onPanic     slcore.CheckWriteHook // default is WriteThenPanic
	onFatal     slcore.CheckWriteHook // default is WriteThenFatal

	name        string
	errorOutput slcore.WriteSyncer

	addStack slcore.LevelEnabler

	callerSkip int

	clock slcore.Clock

	// closers are resources (rolling writers, async writers, files) owned by
	// the logger created via Config.Build. Cloned loggers share the same
	// backing slice; Close is idempotent.
	closers []io.Closer
	closeMu *sync.Once
}

// New constructs a new Logger from the provided slcore.Core and Options. If
// the passed slcore.Core is nil, it falls back to using a no-op
// implementation.
func New(core slcore.Core, options ...Option) *Logger {
	if core == nil {
		return NewNop()
	}
	log := &Logger{
		core:        core,
		errorOutput: slcore.Lock(os.Stderr),
		addStack:    FatalLevel + 1,
		clock:       slcore.DefaultClock,
		closeMu:     &sync.Once{},
	}
	return log.WithOptions(options...)
}

// NewNop returns a no-op Logger. It never writes out logs or internal errors,
// and it never runs user-defined hooks.
func NewNop() *Logger {
	return &Logger{
		core:        slcore.NewNopCore(),
		errorOutput: slcore.AddSync(io.Discard),
		addStack:    FatalLevel + 1,
		clock:       slcore.DefaultClock,
		closeMu:     &sync.Once{},
	}
}

// NewExample builds a Logger that's designed for use in sllogger's testable
// examples. It writes DebugLevel and above logs to standard out as JSON, but
// omits the timestamp and calling function to keep example output
// short and deterministic.
func NewExample(options ...Option) *Logger {
	encoderCfg := slcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		NameKey:        "logger",
		EncodeLevel:    slcore.LowercaseLevelEncoder,
		EncodeTime:     slcore.ISO8601TimeEncoder,
		EncodeDuration: slcore.StringDurationEncoder,
	}
	core := slcore.NewCore(encoder.NewJSONEncoder(encoderCfg), os.Stdout, DebugLevel)
	return New(core).WithOptions(options...)
}

// Must is a helper that wraps a call to a function returning (*Logger, error)
// and panics if the error is non-nil.
func Must(logger *Logger, err error) *Logger {
	if err != nil {
		panic(err)
	}
	return logger
}

// Sugar wraps the Logger to provide a more ergonomic, but slightly slower,
// API.
func (log *Logger) Sugar() *SugaredLogger {
	core := log.clone()
	core.callerSkip += 2
	return &SugaredLogger{base: core}
}

// Named adds a new path segment to the logger's name. Segments are joined by
// periods. By default, Loggers are unnamed.
func (log *Logger) Named(s string) *Logger {
	if s == "" {
		return log
	}
	l := log.clone()
	if log.name == "" {
		l.name = s
	} else {
		l.name = strings.Join([]string{l.name, s}, ".")
	}
	return l
}

// WithOptions clones the current Logger, applies the supplied Options, and
// returns the resulting Logger. It's safe to use concurrently.
func (log *Logger) WithOptions(opts ...Option) *Logger {
	c := log.clone()
	for _, opt := range opts {
		opt.apply(c)
	}
	return c
}

// With creates a child logger and adds structured context to it. Fields added
// to the child don't affect the parent, and vice versa.
func (log *Logger) With(fields ...Field) *Logger {
	if len(fields) == 0 {
		return log
	}
	l := log.clone()
	l.core = l.core.With(fields)
	return l
}

// Level reports the minimum enabled level for this logger.
func (log *Logger) Level() Level {
	return slcore.LevelOf(log.core)
}

// Check returns a CheckedEntry if logging a message at the specified level
// is enabled.
func (log *Logger) Check(lvl Level, msg string) *slcore.CheckedEntry {
	return log.check(lvl, msg)
}

// Log logs a message at the specified level.
func (log *Logger) Log(lvl Level, msg string, fields ...Field) {
	if ce := log.check(lvl, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Debug logs a message at DebugLevel.
func (log *Logger) Debug(msg string, fields ...Field) {
	if ce := log.check(DebugLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Info logs a message at InfoLevel.
func (log *Logger) Info(msg string, fields ...Field) {
	if ce := log.check(InfoLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Warn logs a message at WarnLevel.
func (log *Logger) Warn(msg string, fields ...Field) {
	if ce := log.check(WarnLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Error logs a message at ErrorLevel.
func (log *Logger) Error(msg string, fields ...Field) {
	if ce := log.check(ErrorLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// DPanic logs a message at DPanicLevel. If the logger is in development mode,
// it then panics.
func (log *Logger) DPanic(msg string, fields ...Field) {
	if ce := log.check(DPanicLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Panic logs a message at PanicLevel. The logger then panics, even if logging
// at PanicLevel is disabled.
func (log *Logger) Panic(msg string, fields ...Field) {
	if ce := log.check(PanicLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Fatal logs a message at FatalLevel. The logger then calls os.Exit(1), even
// if logging at FatalLevel is disabled.
func (log *Logger) Fatal(msg string, fields ...Field) {
	if ce := log.check(FatalLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Sync calls the underlying Core's Sync method, flushing any buffered log
// entries. Applications should take care to call Sync before exiting.
func (log *Logger) Sync() error {
	return log.core.Sync()
}

// Close flushes and releases all resources owned by the logger (async
// queues, rolling writers, open files). It is safe to call multiple times.
func (log *Logger) Close() error {
	log.closeMu.Do(func() {
		_ = log.core.Sync()
		for _, c := range log.closers {
			_ = c.Close()
		}
	})
	return nil
}

// Core returns the Logger's underlying slcore.Core.
func (log *Logger) Core() slcore.Core {
	return log.core
}

// Name returns the Logger's underlying name, or an empty string if the logger
// is unnamed.
func (log *Logger) Name() string {
	return log.name
}

func (log *Logger) clone() *Logger {
	clone := *log
	return &clone
}

func (log *Logger) check(lvl Level, msg string) *slcore.CheckedEntry {
	// Logger.check must always be called directly by a method in the
	// Logger interface (e.g., Check, Info, Fatal).
	// This skips Logger.check and the Info/Fatal/Check/etc. method that
	// called it.
	const callerSkipOffset = 2

	// Check the level first to reduce the cost of disabled log calls.
	// Since Panic and higher may exit, we skip the optimization for those
	// levels.
	if lvl < DPanicLevel && !log.core.Enabled(lvl) {
		return nil
	}

	// Create basic checked entry thru the core; this will be non-nil if the
	// log message will actually be written somewhere.
	ent := slcore.Entry{
		LoggerName: log.name,
		Time:       log.clock.Now(),
		Level:      lvl,
		Message:    msg,
	}
	ce := log.core.Check(ent, nil)
	willWrite := ce != nil

	// Set up any required terminal behavior.
	switch ent.Level {
	case PanicLevel:
		ce = ce.After(ent, terminalHookOverride(slcore.WriteThenPanic, log.onPanic))
	case FatalLevel:
		ce = ce.After(ent, terminalHookOverride(slcore.WriteThenFatal, log.onFatal))
	case DPanicLevel:
		if log.development {
			ce = ce.After(ent, terminalHookOverride(slcore.WriteThenPanic, log.onPanic))
		}
	}

	// Only do further annotation if we're going to write this message.
	if !willWrite {
		return ce
	}

	// Thread the error output through to the CheckedEntry.
	ce.ErrorOutput = log.errorOutput

	addStack := log.addStack.Enabled(ce.Level)
	if !log.addCaller && !addStack {
		return ce
	}

	if log.addCaller {
		ce.Caller = slcore.CaptureCaller(log.callerSkip + callerSkipOffset)
	}
	if addStack {
		buf := bufferpool.Get()
		defer buf.Free()
		buf.AppendString(slcore.CaptureStack(log.callerSkip + callerSkipOffset))
		ce.Stack = buf.String()
	}

	return ce
}

func terminalHookOverride(defaultHook, override slcore.CheckWriteHook) slcore.CheckWriteHook {
	// A nil or WriteThenNoop hook will lead to continued execution after
	// a Panic or Fatal log entry, which is unexpected.
	if override == nil || override == slcore.WriteThenNoop {
		return defaultHook
	}
	return override
}

// keep bufferpool import used even when annotations are disabled.
var _ = bufferpool.Get
var _ = fmt.Sprintf
