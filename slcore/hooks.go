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

// Derived from go.uber.org/zap/zapcore/hook.go.
package slcore

import (
	"fmt"
	"os"
)

// RegisterHooks returns a Core that runs user-defined callback functions for
// entries.
//
// Hooks are useful for simple side effects, like capturing metrics for the
// number of emitted logs. More complex side effects should be implemented as a
// separate Core.
func RegisterHooks(core Core, hooks ...func(Entry) error) Core {
	if core == nil {
		return core
	}
	return hookCore{
		Core:        core,
		funcs:       hooks,
		errorOutput: Lock(os.Stderr),
	}
}

type hookCore struct {
	Core
	funcs       []func(Entry) error
	errorOutput WriteSyncer
}

func (c hookCore) Check(ent Entry, ce *CheckedEntry) *CheckedEntry {
	ce = c.Core.Check(ent, ce)
	if ce != nil && len(c.funcs) > 0 {
		ce.Before(ent, c.before)
	}
	return ce
}

func (c hookCore) before(ent Entry, fields []Field) (Entry, []Field) {
	for i := range c.funcs {
		if err := c.funcs[i](ent); err != nil && c.errorOutput != nil {
			_, _ = fmt.Fprintf(c.errorOutput, "%v hook error: %v\n", ent.Time, err)
			_ = c.errorOutput.Sync()
		}
	}
	return ent, fields
}

func (c hookCore) With(fields []Field) Core {
	return hookCore{Core: c.Core.With(fields), funcs: c.funcs, errorOutput: c.errorOutput}
}
