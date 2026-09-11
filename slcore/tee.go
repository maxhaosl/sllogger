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

// Derived from go.uber.org/zap/zapcore/tee.go (multierr replaced with errors.Join).
package slcore

import (
	"errors"
)

type multiCore []Core

var (
	_ Core           = (*multiCore)(nil)
	_ leveledEnabler = (*multiCore)(nil)
)

func (mc multiCore) With(fields []Field) Core {
	clone := make(multiCore, len(mc))
	for i := range mc {
		clone[i] = mc[i].With(fields)
	}
	return clone
}

func (mc multiCore) Level() Level {
	minLvl := _maxLevel // mc is never empty
	for i := range mc {
		if lvl := LevelOf(mc[i]); lvl < minLvl {
			minLvl = lvl
		}
	}
	return minLvl
}

func (mc multiCore) Enabled(lvl Level) bool {
	for i := range mc {
		if mc[i].Enabled(lvl) {
			return true
		}
	}
	return false
}

func (mc multiCore) Check(ent Entry, ce *CheckedEntry) *CheckedEntry {
	for i := range mc {
		ce = mc[i].Check(ent, ce)
	}
	return ce
}

func (mc multiCore) Write(ent Entry, fields []Field) error {
	var err error
	for i := range mc {
		if e := mc[i].Write(ent, fields); e != nil {
			err = errors.Join(err, e)
		}
	}
	return err
}

func (mc multiCore) Sync() error {
	var err error
	for i := range mc {
		if e := mc[i].Sync(); e != nil {
			err = errors.Join(err, e)
		}
	}
	return err
}

// NewTee creates a Core that duplicates log entries into two or more
// underlying Cores.
//
// Calling this with no arguments is valid but unnecessary; it returns a
// no-op Core.
func NewTee(cores ...Core) Core {
	if len(cores) == 0 {
		return NewNopCore()
	}
	if len(cores) == 1 {
		return cores[0]
	}
	return multiCore(cores)
}
