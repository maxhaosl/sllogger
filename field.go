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

// Derived from go.uber.org/zap/field.go.
package sllogger

import (
	"fmt"
	"math"
	"time"

	"sllogger/slcore"
)

// Field is an alias for slcore.Field. Aliasing this type dramatically
// improves the navigability of this package's API documentation.
type Field = slcore.Field

// Skip constructs a no-op field, which is often useful when handling invalid
// inputs in other Field constructors.
func Skip() Field {
	return Field{Type: slcore.SkipType}
}

// Binary constructs a field that carries an opaque binary blob.
func Binary(key string, val []byte) Field {
	return Field{Key: key, Type: slcore.BinaryType, Interface: val}
}

// Bool constructs a field that carries a bool.
func Bool(key string, val bool) Field {
	var ival int64
	if val {
		ival = 1
	}
	return Field{Key: key, Type: slcore.BoolType, Integer: ival}
}

// ByteString constructs a field that carries UTF-8 encoded text as a []byte.
func ByteString(key string, val []byte) Field {
	return Field{Key: key, Type: slcore.ByteStringType, Interface: val}
}

// Complex128 constructs a field that carries a complex number.
func Complex128(key string, val complex128) Field {
	return Field{Key: key, Type: slcore.Complex128Type, Interface: val}
}

// Complex64 constructs a field that carries a complex number.
func Complex64(key string, val complex64) Field {
	return Field{Key: key, Type: slcore.Complex64Type, Interface: val}
}

// Float64 constructs a field that carries a float64.
func Float64(key string, val float64) Field {
	return Field{Key: key, Type: slcore.Float64Type, Integer: int64(math.Float64bits(val))}
}

// Float32 constructs a field that carries a float32.
func Float32(key string, val float32) Field {
	return Field{Key: key, Type: slcore.Float32Type, Integer: int64(math.Float32bits(val))}
}

// Int constructs a field with the given key and value.
func Int(key string, val int) Field {
	return Int64(key, int64(val))
}

// Int64 constructs a field with the given key and value.
func Int64(key string, val int64) Field {
	return Field{Key: key, Type: slcore.Int64Type, Integer: val}
}

// Int32 constructs a field with the given key and value.
func Int32(key string, val int32) Field {
	return Field{Key: key, Type: slcore.Int32Type, Integer: int64(val)}
}

// Int16 constructs a field with the given key and value.
func Int16(key string, val int16) Field {
	return Field{Key: key, Type: slcore.Int16Type, Integer: int64(val)}
}

// Int8 constructs a field with the given key and value.
func Int8(key string, val int8) Field {
	return Field{Key: key, Type: slcore.Int8Type, Integer: int64(val)}
}

// String constructs a field with the given key and value.
func String(key string, val string) Field {
	return Field{Key: key, Type: slcore.StringType, String: val}
}

// Uint constructs a field with the given key and value.
func Uint(key string, val uint) Field {
	return Uint64(key, uint64(val))
}

// Uint64 constructs a field with the given key and value.
func Uint64(key string, val uint64) Field {
	return Field{Key: key, Type: slcore.Uint64Type, Integer: int64(val)}
}

// Uint32 constructs a field with the given key and value.
func Uint32(key string, val uint32) Field {
	return Field{Key: key, Type: slcore.Uint32Type, Integer: int64(val)}
}

// Uint16 constructs a field with the given key and value.
func Uint16(key string, val uint16) Field {
	return Field{Key: key, Type: slcore.Uint16Type, Integer: int64(val)}
}

// Uint8 constructs a field with the given key and value.
func Uint8(key string, val uint8) Field {
	return Field{Key: key, Type: slcore.Uint8Type, Integer: int64(val)}
}

// Uintptr constructs a field with the given key and value.
func Uintptr(key string, val uintptr) Field {
	return Field{Key: key, Type: slcore.UintptrType, Integer: int64(val)}
}

// Reflect constructs a field with the given key and an arbitrary object. It uses
// an encoding-appropriate, reflection-based function to lazily serialize nearly
// any object into the logging context.
func Reflect(key string, val interface{}) Field {
	return Field{Key: key, Type: slcore.ReflectType, Interface: val}
}

// Namespace creates a named, isolated scope within the logger's context.
func Namespace(key string) Field {
	return Field{Key: key, Type: slcore.NamespaceType}
}

// Stringer constructs a field with the given key and the output of the value's
// String method.
func Stringer(key string, val fmt.Stringer) Field {
	return Field{Key: key, Type: slcore.StringerType, Interface: val}
}

// Time constructs a Field with the given key and value.
func Time(key string, val time.Time) Field {
	return Field{Key: key, Type: slcore.TimeFullType, Interface: val}
}

// Duration constructs a field with the given key and value.
func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Type: slcore.DurationType, Integer: int64(val)}
}

// Array constructs a field with the given key and value. The array marshaler
// is lazily evaluated.
func Array(key string, val slcore.ArrayMarshaler) Field {
	return Field{Key: key, Type: slcore.ArrayMarshalerType, Interface: val}
}

// Object constructs a field with the given key and object marshaler.
func Object(key string, val slcore.ObjectMarshaler) Field {
	return Field{Key: key, Type: slcore.ObjectMarshalerType, Interface: val}
}

// Any is a generic, lazy way to construct a field with the given key and
// value. It falls back to Reflect for unknown types.
func Any(key string, value interface{}) Field {
	switch val := value.(type) {
	case slcore.ObjectMarshaler:
		return Object(key, val)
	case slcore.ArrayMarshaler:
		return Array(key, val)
	case bool:
		return Bool(key, val)
	case []byte:
		return Binary(key, val)
	case complex128:
		return Complex128(key, val)
	case complex64:
		return Complex64(key, val)
	case float32:
		return Float32(key, val)
	case float64:
		return Float64(key, val)
	case int:
		return Int(key, val)
	case int8:
		return Int8(key, val)
	case int16:
		return Int16(key, val)
	case int32:
		return Int32(key, val)
	case int64:
		return Int64(key, val)
	case string:
		return String(key, val)
	case uint:
		return Uint(key, val)
	case uint8:
		return Uint8(key, val)
	case uint16:
		return Uint16(key, val)
	case uint32:
		return Uint32(key, val)
	case uint64:
		return Uint64(key, val)
	case uintptr:
		return Uintptr(key, val)
	// time.Time and time.Duration both implement fmt.Stringer, so they must
	// be matched before it to keep their richer encodings.
	case time.Time:
		return Time(key, val)
	case time.Duration:
		return Duration(key, val)
	case error:
		return NamedError(key, val)
	case fmt.Stringer:
		return Stringer(key, val)
	case nil:
		return Reflect(key, nil)
	default:
		return Reflect(key, val)
	}
}
