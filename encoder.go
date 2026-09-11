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

// Derived from go.uber.org/zap/encoder.go (RegisterEncoder).
package sllogger

import (
	"fmt"
	"strings"

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
)

var (
	_encoderNameToConstructor = map[string]func(slcore.EncoderConfig) (slcore.Encoder, error){
		"json": func(cfg slcore.EncoderConfig) (slcore.Encoder, error) {
			return encoder.NewJSONEncoder(cfg), nil
		},
	}
)

// RegisterEncoder registers an encoder constructor, which the Config struct
// can then reference. By default, the "json" encoder is registered.
//
// Attempting to register an encoder whose name is already taken returns an
// error.
func RegisterEncoder(name string, constructor func(slcore.EncoderConfig) (slcore.Encoder, error)) error {
	lower := strings.ToLower(name)
	switch lower {
	case "json", "console": // json is taken; console is unused by sllogger but reserved to match zap semantics
		return fmt.Errorf("encoder name %q is reserved", name)
	}
	if _, ok := _encoderNameToConstructor[lower]; ok {
		return fmt.Errorf("encoder name %q is already registered", name)
	}
	_encoderNameToConstructor[lower] = constructor
	return nil
}
