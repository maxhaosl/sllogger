// Copyright (c) 2017 Uber Technologies, Inc.
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

// Derived from go.uber.org/zap/zapcore/error.go.
package slcore

import (
	"errors"
	"fmt"
	"reflect"
)

// Encodes the given error into fields of an object. A field with the given
// name is added for the error message.
//
// If the error implements fmt.Formatter, a field with the name ${key}Verbose
// is also added with the full verbose error message.
func encodeError(key string, err error, enc ObjectEncoder) (retErr error) {
	// Try to capture panics (from nil references or otherwise) when calling
	// the Error() method.
	defer func() {
		if rerr := recover(); rerr != nil {
			if v := reflect.ValueOf(err); v.Kind() == reflect.Pointer && v.IsNil() {
				enc.AddString(key, "<nil>")
				return
			}
			retErr = fmt.Errorf("PANIC=%v", rerr)
		}
	}()

	basic := err.Error()
	enc.AddString(key, basic)

	if e, ok := err.(fmt.Formatter); ok {
		verbose := fmt.Sprintf("%+v", e)
		if verbose != basic {
			enc.AddString(key+"Verbose", verbose)
		}
	}
	return nil
}

// appendErrors joins multiple non-nil errors into a single error using
// errors.Join. It replaces go.uber.org/multierr to keep the library
// dependency-free.
func appendErrors(err error, errs ...error) error {
	return errors.Join(append([]error{err}, errs...)...)
}
