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

// Derived from go.uber.org/zap/config.go.
package sllogger

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"sllogger/encoder"
	"sllogger/slcore"
	"sllogger/writer"
)

// concurrentWriteSyncer marks a WriteSyncer that is already safe for
// concurrent use.
type concurrentWriteSyncer interface {
	ConcurrentSafe() bool
}

// lockIfNeeded wraps w in a mutex unless it reports itself as concurrency
// safe.
func lockIfNeeded(w slcore.WriteSyncer) slcore.WriteSyncer {
	if cs, ok := w.(concurrentWriteSyncer); ok && cs.ConcurrentSafe() {
		return w
	}
	return slcore.Lock(w)
}

// isZeroEncoderConfig reports whether no JSON encoder keys were configured.
// EncoderConfig holds func fields, which are not comparable, so the keys are
// checked individually.
func isZeroEncoderConfig(c slcore.EncoderConfig) bool {
	return c.MessageKey == "" && c.LevelKey == "" && c.TimeKey == "" &&
		c.NameKey == "" && c.CallerKey == "" && c.FunctionKey == "" &&
		c.StacktraceKey == "" && c.EncodeLevel == nil && c.EncodeTime == nil &&
		c.EncodeDuration == nil && c.EncodeCaller == nil && c.EncodeName == nil &&
		c.LineEnding == "" && c.ConsoleSeparator == ""
}

// TemplateConfig is an alias for encoder.TemplateConfig, so callers can
// configure content templates without importing the encoder package.
type TemplateConfig = encoder.TemplateConfig

// RollingConfig is an alias for writer.Config.
type RollingConfig = writer.Config

// Config offers a declarative way to construct a logger. It doesn't do
// anything that can't be done with New, Options, and the various slcore
// wrappers, but it's a simpler way to toggle common options.
type Config struct {
	// Level is the minimum enabled logging level.
	Level AtomicLevel `json:"level" yaml:"level"`
	// Development puts the logger in development mode.
	Development bool `json:"development" yaml:"development"`
	// DisableCaller stops annotating logs with the caller info.
	DisableCaller bool `json:"disableCaller" yaml:"disableCaller"`
	// DisableStacktrace disables automatic stacktrace capturing.
	DisableStacktrace bool `json:"disableStacktrace" yaml:"disableStacktrace"`

	// Encoding selects the log format:
	//
	//   - "" / "json":        one-line JSON (EncoderConfig applies)
	//   - "template":         custom content template (TemplateConfig applies)
	//   - "callinfo":         preset CALL_INFO template (TemplateConfig applies)
	//   - "requestinfo":      preset RequestInfo template
	Encoding string `json:"encoding" yaml:"encoding"`

	// EncoderConfig configures the JSON encoder.
	EncoderConfig slcore.EncoderConfig `json:"encoderConfig" yaml:"encoderConfig"`

	// TemplateConfig configures the content-template encoder.
	TemplateConfig encoder.TemplateConfig `json:"templateConfig" yaml:"templateConfig"`

	// OutputPaths is a list of paths to write logging output to. Special
	// values "stdout" and "stderr" are supported. Ignored when Rolling is
	// set.
	OutputPaths []string `json:"outputPaths" yaml:"outputPaths"`

	// ErrorOutputPaths is a list of paths to write internal logger errors to.
	ErrorOutputPaths []string `json:"errorOutputPaths" yaml:"errorOutputPaths"`

	// Rolling 配置滚动文件输出（需求 1/2/4-9）。非空时优先于 OutputPaths。
	Rolling *writer.Config `json:"rolling" yaml:"rolling"`

	// InitialFields is a collection of fields to add to the root logger.
	InitialFields map[string]interface{} `json:"initialFields" yaml:"initialFields"`

	// Clock allows injecting a custom clock for both log entries and the
	// rolling writer (useful for tests). Defaults to the system clock.
	Clock Clock `json:"-" yaml:"-"`
}

// NewProductionConfig builds a reasonable default production logging
// configuration: InfoLevel and above, JSON encoding to stderr.
func NewProductionConfig() Config {
	return Config{
		Level:            NewAtomicLevelAt(InfoLevel),
		Development:      false,
		Encoding:         "json",
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
}

// NewDevelopmentConfig builds a development configuration: DebugLevel and
// above, JSON encoding to stderr.
func NewDevelopmentConfig() Config {
	return Config{
		Level:            NewAtomicLevelAt(DebugLevel),
		Development:      true,
		Encoding:         "json",
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
}

// NewCallInfoConfig builds a CALL_INFO logging configuration backed by a
// rolling file writer, matching doc/go_call_info_logger_design.md:
//
//	/data/logs/LOG_CALL_INFO.2026-09-03.playurl8080.log
func NewCallInfoConfig(dir, serviceName string, servicePort int) Config {
	cfg := NewProductionConfig()
	cfg.Encoding = "callinfo"
	cfg.OutputPaths = nil
	cfg.Rolling = &writer.Config{
		Dir:         dir,
		BaseName:    "LOG_CALL_INFO",
		ServiceName: serviceName,
		ServicePort: servicePort,
	}
	if cfg.TemplateConfig.ServiceID == "" {
		cfg.TemplateConfig.ServiceID = serviceName
	}
	return cfg
}

// Build constructs a logger from the Config and Options.
func (cfg Config) Build(opts ...Option) (*Logger, error) {
	enc, err := cfg.buildEncoder()
	if err != nil {
		return nil, err
	}

	out, closers, err := cfg.openOutput()
	if err != nil {
		return nil, err
	}

	if cfg.Level.l == nil {
		return nil, errors.New("missing Level")
	}

	errSink, _, err := Open(cfg.errorOutputPathsOrDefault()...)
	if err != nil {
		for _, c := range closers {
			_ = c.Close()
		}
		return nil, err
	}

	buildOpts := cfg.buildOptions(errSink)
	if cfg.Clock != nil {
		buildOpts = append(buildOpts, WithClock(cfg.Clock))
	}

	// AsyncWriter (and RollingWriter) are already safe for concurrent use, so
	// an extra mutex layer would only add overhead — measured at ~11% of CPU
	// under load. Only wrap sinks that are not self-synchronizing.
	log := New(
		slcore.NewCore(enc, lockIfNeeded(out), cfg.Level),
		buildOpts...,
	)
	log.closers = append(log.closers, closers...)

	if len(opts) > 0 {
		log = log.WithOptions(opts...)
	}
	return log, nil
}

func (cfg Config) errorOutputPathsOrDefault() []string {
	if len(cfg.ErrorOutputPaths) == 0 {
		return []string{"stderr"}
	}
	return cfg.ErrorOutputPaths
}

func (cfg Config) buildOptions(errSink slcore.WriteSyncer) []Option {
	opts := []Option{ErrorOutput(errSink)}

	if cfg.Development {
		opts = append(opts, Development())
	}

	if !cfg.DisableCaller {
		opts = append(opts, AddCaller())
	}

	stackLevel := ErrorLevel
	if cfg.Development {
		stackLevel = WarnLevel
	}
	if !cfg.DisableStacktrace {
		opts = append(opts, AddStacktrace(stackLevel))
	}

	if len(cfg.InitialFields) > 0 {
		fs := make([]Field, 0, len(cfg.InitialFields))
		keys := make([]string, 0, len(cfg.InitialFields))
		for k := range cfg.InitialFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fs = append(fs, Any(k, cfg.InitialFields[k]))
		}
		opts = append(opts, Fields(fs...))
	}

	return opts
}

// openOutput resolves the main output: rolling writer or plain paths.
func (cfg Config) openOutput() (slcore.WriteSyncer, []io.Closer, error) {
	if cfg.Clock != nil && cfg.Rolling != nil && cfg.Rolling.Clock == nil {
		cfg.Rolling.Clock = cfg.Clock
	}

	if cfg.Rolling != nil {
		rw, err := writer.NewRollingWriter(cfg.Rolling)
		if err != nil {
			return nil, nil, fmt.Errorf("open rolling writer: %w", err)
		}
		var closers []io.Closer
		closers = append(closers, rw)

		var out slcore.WriteSyncer = rw
		if cfg.Rolling.Async {
			aw := writer.NewAsyncWriter(rw, cfg.Rolling)
			closers = append([]io.Closer{aw}, closers...)
			out = aw
			// AsyncWriter implements slcore.LevelWriteSyncer, so ioCore
			// forwards the entry level and the level-aware queue-full policy
			// (design doc #29) takes effect.
		}
		return out, closers, nil
	}

	paths := cfg.OutputPaths
	if len(paths) == 0 {
		paths = []string{"stderr"}
	}
	out, closeOut, err := Open(paths...)
	if err != nil {
		return nil, nil, err
	}
	return out, []io.Closer{closerFunc(closeOut)}, nil
}

type closerFunc func()

func (f closerFunc) Close() error {
	f()
	return nil
}

func (cfg Config) buildEncoder() (slcore.Encoder, error) {
	switch strings.ToLower(cfg.Encoding) {
	case "", "json":
		encCfg := cfg.EncoderConfig
		// An unset EncoderConfig gets the conventional JSON key names, so
		// Config{"Encoding":"json"} works out of the box.
		if isZeroEncoderConfig(encCfg) {
			encCfg = encoder.DefaultJSONEncoderConfig()
		}
		return encoder.NewJSONEncoder(encCfg), nil
	case "template":
		if strings.TrimSpace(cfg.TemplateConfig.Template) == "" {
			return nil, errors.New(`sllogger: encoding "template" requires TemplateConfig.Template`)
		}
		return encoder.NewTemplateEncoder(cfg.templateConfigWithService())
	case "callinfo":
		return encoder.NewCallInfoEncoder(cfg.templateConfigWithService())
	case "requestinfo":
		return encoder.NewRequestInfoEncoder(cfg.templateConfigWithService())
	default:
		return nil, fmt.Errorf("sllogger: unrecognized encoding: %q", cfg.Encoding)
	}
}

// templateConfigWithService fills ServiceID from the rolling config when it
// is not set explicitly.
func (cfg Config) templateConfigWithService() encoder.TemplateConfig {
	tc := cfg.TemplateConfig
	if tc.ServiceID == "" && cfg.Rolling != nil && cfg.Rolling.ServiceName != "" {
		tc.ServiceID = cfg.Rolling.ServiceName
	}
	return tc
}
