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

package slcore

import "sync"

// spyRec is shared between a spyCore and every child produced by With, so the
// original core can observe both With calls and writes performed through its
// derived cores (this mirrors how lazy-with and tee delegate to children).
// It is protected by a mutex so it can be used from concurrent goroutines.
type spyRec struct {
	mu     sync.Mutex
	writes []Entry
	fields [][]Field
	syncs  int
}

func (r *spyRec) recordWrite(e Entry) {
	r.mu.Lock()
	r.writes = append(r.writes, e)
	r.mu.Unlock()
}

func (r *spyRec) recordWith(fields []Field) {
	r.mu.Lock()
	r.fields = append(r.fields, fields)
	r.mu.Unlock()
}

func (r *spyRec) recordSync() {
	r.mu.Lock()
	r.syncs++
	r.mu.Unlock()
}

// spyCore records every Check/Write/Sync/With call so tests can assert on the
// behaviour of composed cores (tee, sampler, increase-level, lazy-with).
type spyCore struct {
	level Level
	rec   *spyRec
}

func newSpyCore(l Level) *spyCore { return &spyCore{level: l, rec: &spyRec{}} }

func (c *spyCore) Enabled(l Level) bool { return c.level.Enabled(l) }
func (c *spyCore) Level() Level         { return c.level }

func (c *spyCore) With(fields []Field) Core {
	c.rec.recordWith(fields)
	return &spyCore{level: c.level, rec: c.rec}
}

func (c *spyCore) Check(e Entry, ce *CheckedEntry) *CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

func (c *spyCore) Write(e Entry, fields []Field) error {
	c.rec.recordWrite(e)
	return nil
}

func (c *spyCore) Sync() error {
	c.rec.recordSync()
	return nil
}

// errCore is a spyCore that returns configured errors from Write/Sync.
type errCore struct {
	spyCore
	writeErr error
	syncErr  error
}

func newErrCore(l Level, werr, serr error) *errCore {
	return &errCore{spyCore: *newSpyCore(l), writeErr: werr, syncErr: serr}
}

func (c *errCore) Write(e Entry, fields []Field) error {
	c.spyCore.Write(e, fields)
	return c.writeErr
}

func (c *errCore) Sync() error {
	c.spyCore.Sync()
	return c.syncErr
}
