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

// Package slcore provides the logging primitives (Level, Core, Encoder,
// WriteSyncer) that the higher-level sllogger package builds on.
//
// # Interface overview
//
// The package is organized around a small set of composable interfaces:
//
//   - LevelEnabler  decides whether a level is enabled.
//   - Core         is the minimum logging interface; it embeds LevelEnabler and
//     adds With/Check/Write/Sync. Concrete cores (ioCore, Tee, Sampler,
//     IncreaseLevelCore, LazyWith) wrap one another, so behaviours compose.
//   - Encoder      serializes an Entry + Fields into bytes. It embeds
//     ObjectEncoder; ArrayEncoder embeds PrimitiveArrayEncoder.
//   - WriteSyncer  is an io.Writer that can also flush; LevelWriteSyncer is an
//     optional extension that receives the entry level.
//   - Clock        abstracts time for deterministic tests.
//   - ReflectedEncoder  serializes arbitrary values via reflection.
//   - ArrayMarshaler / ObjectMarshaler  let user types plug into the encoders.
//   - CheckWriteHook  lets callers inspect/rewrite the chosen cores per entry.
//   - SamplerOption   configures the Sampler core.
//
// All interfaces are intentionally small and embedding-friendly: a Core is just
// a LevelEnabler plus four methods, and an Encoder is just an ObjectEncoder
// plus Clone/EncodeEntry. This keeps the contract stable while allowing many
// orthogonal wrappers.
package slcore

import (
	"io"
	"time"

	"github.com/maxhaosl/sllogger/buffer"
)

// LevelEnabler decides whether a given logging level is enabled when logging a
// message.
//
// Enablers are used to implement structured logging libraries' leveled logging
// APIs. A LevelEnabler is the sole source of truth for which level a logger
// should be logging at.
type LevelEnabler interface {
	Enabled(Level) bool
}

// Core is a minimal, composable logging interface. It embeds LevelEnabler so
// that any Core can answer Enabled. Concrete implementations wrap one another
// (e.g. a sampling Core around an I/O Core), so behaviours compose.
type Core interface {
	LevelEnabler

	// With adds structured context to the Core.
	With([]Field) Core
	// Check determines whether the supplied Entry should be logged, using the
	// provided CheckedEntry to accumulate the result.
	Check(Entry, *CheckedEntry) *CheckedEntry
	// Write serializes the Entry and Fields and writes them to the Core's sink.
	Write(Entry, []Field) error
	// Sync flushes buffered I/O, if any.
	Sync() error
}

// Encoder is a format-agnostic interface for all log entry marshalers. Since
// log encoders don't need to support the same wide range of use cases as
// general-purpose marshalers, it's possible to make them faster and
// lower-allocation.
type Encoder interface {
	ObjectEncoder

	// Clone copies the encoder, ensuring that adding fields to the copy doesn't
	// affect the original.
	Clone() Encoder

	// EncodeEntry encodes an entry and fields, along with any accumulated
	// context, into a byte buffer and returns it. Any fields that are empty,
	// including fields on the `Entry` type, should be omitted.
	EncodeEntry(Entry, []Field) (*buffer.Buffer, error)
}

// ObjectEncoder is a strongly-typed, encoding-agnostic interface for adding a
// map- or struct-like object to the logging context.
type ObjectEncoder interface {
	// Logging-specific marshalers.
	AddArray(key string, marshaler ArrayMarshaler) error
	AddObject(key string, marshaler ObjectMarshaler) error

	// Built-in types.
	AddBinary(key string, value []byte)     // for arbitrary bytes
	AddByteString(key string, value []byte) // for UTF-8 encoded bytes
	AddBool(key string, value bool)
	AddComplex128(key string, value complex128)
	AddComplex64(key string, value complex64)
	AddDuration(key string, value time.Duration)
	AddFloat64(key string, value float64)
	AddFloat32(key string, value float32)
	AddInt(key string, value int)
	AddInt64(key string, value int64)
	AddInt32(key string, value int32)
	AddInt16(key string, value int16)
	AddInt8(key string, value int8)
	AddString(key, value string)
	AddTime(key string, value time.Time)
	AddUint(key string, value uint)
	AddUint64(key string, value uint64)
	AddUint32(key string, value uint32)
	AddUint16(key string, value uint16)
	AddUint8(key string, value uint8)
	AddUintptr(key string, value uintptr)

	// AddReflected uses reflection to serialize arbitrary objects, so it can be
	// slow and allocation-heavy.
	AddReflected(key string, value interface{}) error
	// OpenNamespace opens an isolated namespace where all subsequent fields will
	// be added.
	OpenNamespace(key string)
}

// ArrayEncoder is a strongly-typed, encoding-agnostic interface for adding
// array-like objects to the logging context.
type ArrayEncoder interface {
	// Built-in types.
	PrimitiveArrayEncoder

	// Time-related types.
	AppendDuration(time.Duration)
	AppendTime(time.Time)

	// Logging-specific marshalers.
	AppendArray(ArrayMarshaler) error
	AppendObject(ObjectMarshaler) error

	// AppendReflected uses reflection to serialize arbitrary objects, so it's
	// slow and allocation-heavy.
	AppendReflected(value interface{}) error
}

// PrimitiveArrayEncoder is the subset of the ArrayEncoder interface that deals
// only in Go's built-in types. It's included only so that Duration- and
// TimeEncoders cannot trigger infinite recursion.
type PrimitiveArrayEncoder interface {
	// Built-in types.
	AppendBool(bool)
	AppendByteString([]byte) // for UTF-8 encoded bytes
	AppendComplex128(complex128)
	AppendComplex64(complex64)
	AppendFloat64(float64)
	AppendFloat32(float32)
	AppendInt(int)
	AppendInt64(int64)
	AppendInt32(int32)
	AppendInt16(int16)
	AppendInt8(int8)
	AppendString(string)
	AppendUint(uint)
	AppendUint64(uint64)
	AppendUint32(uint32)
	AppendUint16(uint16)
	AppendUint8(uint8)
	AppendUintptr(uintptr)
}

// WriteSyncer is an io.Writer that can also flush any buffered data. Note that
// *os.File (and thus, os.Stderr and os.Stdout) implement WriteSyncer.
type WriteSyncer interface {
	io.Writer
	Sync() error
}

// LevelWriteSyncer is an optional extension of WriteSyncer that receives the log
// level along with the payload.
//
// It enables level-aware write policies, for example the design doc's rule (#29)
// that ERROR entries block when the async queue is full while INFO/CALL_INFO
// entries are dropped to protect the business.
//
// ioCore.Write uses this interface when available and falls back to plain Write
// otherwise, so existing WriteSyncer implementations keep working unchanged.
type LevelWriteSyncer interface {
	WriteSyncer
	// WriteLevel writes p, which was encoded from an entry at the given level.
	WriteLevel(lvl Level, p []byte) (int, error)
}

// Clock is used to stamp entries with the current time. It is abstracted so that
// tests can use a deterministic clock instead of time.Now.
type Clock interface {
	Now() time.Time
	NewTicker(time.Duration) *time.Ticker
}

// ReflectedEncoder serializes log fields whose type can't be serialized by the
// structured encoders. Implementations are configured via EncoderConfig.
type ReflectedEncoder interface {
	// Encode encodes v and writes it to the underlying data stream.
	Encode(interface{}) error
}

// ArrayMarshaler lets user-defined types write themselves into an ArrayEncoder.
type ArrayMarshaler interface {
	MarshalLogArray(ArrayEncoder) error
}

// ObjectMarshaler lets user-defined types write themselves into an
// ObjectEncoder.
type ObjectMarshaler interface {
	MarshalLogObject(ObjectEncoder) error
}

// CheckWriteHook is a custom action that may be executed after an entry is
// written.
type CheckWriteHook interface {
	// OnWrite is invoked with the CheckedEntry that was written and a list
	// of fields added with that entry.
	OnWrite(*CheckedEntry, []Field)
}

// SamplerOption configures a Sampler core.
type SamplerOption interface {
	apply(*sampler)
}
