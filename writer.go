// Copyright (c) 2016-2022 Uber Technologies, Inc.
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

// Derived from go.uber.org/zap/writer.go, without the sink registry.
package sllogger

import (
	"fmt"
	"io"
	"os"

	"sllogger/slcore"
)

// Open is a high-level wrapper that takes a variadic number of paths, opens or
// creates each of the specified resources, and combines them into a locked
// WriteSyncer. It also returns any error encountered and a function to close
// any opened files.
//
// Without a scheme, the special paths "stdout" and "stderr" are interpreted as
// os.Stdout and os.Stderr. Other paths are treated as local file paths
// (opened for appending).
func Open(paths ...string) (slcore.WriteSyncer, func(), error) {
	writers := make([]slcore.WriteSyncer, 0, len(paths))
	closers := make([]io.Closer, 0, len(paths))
	closeAll := func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}

	var openErr error
	for _, path := range paths {
		var ws slcore.WriteSyncer
		var closer io.Closer
		var err error
		switch path {
		case "stdout":
			ws = os.Stdout
		case "stderr":
			ws = os.Stderr
		default:
			f, ferr := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
			if ferr != nil {
				err = fmt.Errorf("open file %q: %w", path, ferr)
			} else {
				ws = f
				closer = f
			}
		}
		if err != nil {
			openErr = fmt.Errorf("open output %q: %w", path, err)
			continue
		}
		writers = append(writers, ws)
		if closer != nil {
			closers = append(closers, closer)
		}
	}
	if openErr != nil {
		closeAll()
		return nil, nil, openErr
	}

	return CombineWriteSyncers(writers...), closeAll, nil
}

// CombineWriteSyncers is a utility that combines multiple WriteSyncers into a
// single, locked WriteSyncer. If no inputs are supplied, it returns a no-op
// WriteSyncer.
func CombineWriteSyncers(writers ...slcore.WriteSyncer) slcore.WriteSyncer {
	if len(writers) == 0 {
		return slcore.AddSync(io.Discard)
	}
	return slcore.Lock(slcore.NewMultiWriteSyncer(writers...))
}
