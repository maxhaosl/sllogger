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

	"github.com/maxhaosl/sllogger/encoder"
	"github.com/maxhaosl/sllogger/slcore"
	"github.com/maxhaosl/sllogger/writer"
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

	// Outputs 配置多个独立日志输出：每个输出有各自的格式（Encoding/TemplateConfig）、
	// 各自的滚动文件（Rolling）或路径（OutputPaths），以及各自的级别路由
	// （Levels/ExcludeLevels/Level）。
	//
	// 适用于三类需求：
	//   - 按级别分流到不同日志文件（如 ERROR/WARN 写错误文件，INFO/DEBUG 写普通文件）；
	//   - 每文件不同日志格式（如 LOG_FEIGN 用 JSON、LOG_MGMONITOR 用 ^ 分隔模板）；
	//   - 上线只输出部分级别（只配置 ERROR/WARN 输出，或不配置 INFO/DEBUG/TRACE 输出）。
	//
	// 非空时优先于单输出字段（Encoding / TemplateConfig / Rolling / OutputPaths）。
	Outputs []Output `json:"outputs" yaml:"outputs"`

	// InitialFields is a collection of fields to add to the root logger.
	InitialFields map[string]interface{} `json:"initialFields" yaml:"initialFields"`

	// Clock allows injecting a custom clock for both log entries and the
	// rolling writer (useful for tests). Defaults to the system clock.
	Clock Clock `json:"-" yaml:"-"`
}

// Output 描述一个独立的日志输出（多文件 / 按级别分流 / 独立格式）。
//
// 一条日志进入某个 Output 的条件：先通过全局 Config.Level，再通过本 Output
// 的级别路由（优先级：Levels 白名单 > ExcludeLevels 黑名单 > Level 最低级别）：
//
//   - Levels 非空：仅白名单中的级别写入本输出（如 []string{"error","warn"}）。
//   - 否则 ExcludeLevels 非空：黑名单中的级别被丢弃。
//   - 否则 Level（最低级别，含）作为门槛；零值视为 DebugLevel（全收）。
//
// 典型用法：
//
//	Outputs: []Output{
//	  { Name:"feign",     Encoding:"json",     Rolling: feignCfg,
//	    Levels: []string{"info","debug","trace","warn","error"} },
//	  { Name:"feign_err", Encoding:"json",     Rolling: feignErrCfg,
//	    Levels: []string{"error","warn"} },
//	  { Name:"mgmonitor", Encoding:"template", Rolling: mgCfg,
//	    TemplateConfig: encoder.TemplateConfig{Separator:"^", Template:"log_level^date^msg"} },
//	}
type Output struct {
	// Name 仅作标识，方便排查。
	Name string `json:"name" yaml:"name"`

	// Encoding 同 Config.Encoding（json/template/callinfo/requestinfo/自定义）。
	Encoding string `json:"encoding" yaml:"encoding"`

	// EncoderConfig 仅 Encoding="json" 时生效。
	EncoderConfig slcore.EncoderConfig `json:"encoderConfig" yaml:"encoderConfig"`

	// TemplateConfig 仅 Encoding="template"/"callinfo"/"requestinfo" 时生效。
	TemplateConfig encoder.TemplateConfig `json:"templateConfig" yaml:"templateConfig"`

	// Rolling 配置本输出的滚动文件（与 OutputPaths 二选一）。
	Rolling *writer.Config `json:"rolling" yaml:"rolling"`

	// OutputPaths 普通路径（"stdout"/"stderr"/文件路径），与 Rolling 二选一；
	// 都空时回退到 "stderr"。
	OutputPaths []string `json:"outputPaths" yaml:"outputPaths"`

	// Level 本输出的最低级别（含）。零值视为 DebugLevel（全收）。
	Level AtomicLevel `json:"level" yaml:"level"`

	// Levels 白名单：仅这些级别写入本输出。非空时优先生效，覆盖 Level/ExcludeLevels。
	// 取值为级别名：trace/debug/info/warn/error/dpanic/panic/fatal（不区分大小写）。
	Levels []string `json:"levels" yaml:"levels"`

	// ExcludeLevels 黑名单：这些级别不写入本输出。
	ExcludeLevels []string `json:"excludeLevels" yaml:"excludeLevels"`
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

// NewProductionEncoderConfig returns an opinionated EncoderConfig for
// production environments. Messages encoded with this configuration will use
// Zap's JSON encoder, with keys "ts", "level", "msg", etc.
//
// For example, use the following to change the time encoding format:
//
//	cfg := sllogger.NewProductionEncoderConfig()
//	cfg.EncodeTime = slcore.ISO8601TimeEncoder
func NewProductionEncoderConfig() slcore.EncoderConfig {
	return slcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    slcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     slcore.DefaultLineEnding,
		EncodeLevel:    slcore.LowercaseLevelEncoder,
		EncodeTime:     slcore.EpochTimeEncoder,
		EncodeDuration: slcore.SecondsDurationEncoder,
		EncodeCaller:   slcore.ShortCallerEncoder,
	}
}

// NewDevelopmentEncoderConfig returns an opinionated EncoderConfig for
// development environments. Messages encoded with this configuration will use
// Zap's console encoder intended to print human-readable output.
func NewDevelopmentEncoderConfig() slcore.EncoderConfig {
	return slcore.EncoderConfig{
		// Keys can be anything except the empty string.
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		FunctionKey:    slcore.OmitKey,
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     slcore.DefaultLineEnding,
		EncodeLevel:    slcore.CapitalLevelEncoder,
		EncodeTime:     slcore.ISO8601TimeEncoder,
		EncodeDuration: slcore.StringDurationEncoder,
		EncodeCaller:   slcore.ShortCallerEncoder,
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
	if cfg.Level.l == nil {
		return nil, errors.New("missing Level")
	}
	if len(cfg.Outputs) > 0 {
		return cfg.buildMultiOutput(opts...)
	}

	enc, err := cfg.buildEncoder()
	if err != nil {
		return nil, err
	}

	out, closers, err := cfg.openOutput()
	if err != nil {
		return nil, err
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

// buildMultiOutput builds a logger that fans out to every Config.Output via a
// slcore.Tee core. Each output has its own encoder, syncer and level filter;
// an entry is written to an output only if it passes both the global Level and
// that output's level routing (Levels / ExcludeLevels / Level).
func (cfg Config) buildMultiOutput(opts ...Option) (*Logger, error) {
	var (
		cores   []slcore.Core
		closers []io.Closer
		ok      bool
	)
	defer func() {
		if !ok {
			for _, c := range closers {
				_ = c.Close()
			}
		}
	}()

	for i := range cfg.Outputs {
		core, c, err := cfg.buildOutputCore(cfg.Outputs[i])
		if err != nil {
			return nil, err
		}
		cores = append(cores, core)
		closers = append(closers, c...)
	}

	errSink, _, err := Open(cfg.errorOutputPathsOrDefault()...)
	if err != nil {
		return nil, err
	}

	buildOpts := cfg.buildOptions(errSink)
	if cfg.Clock != nil {
		buildOpts = append(buildOpts, WithClock(cfg.Clock))
	}

	log := New(slcore.NewTee(cores...), buildOpts...)
	log.closers = append(log.closers, closers...)

	if len(opts) > 0 {
		log = log.WithOptions(opts...)
	}
	ok = true
	return log, nil
}

// buildOutputCore builds one Config.Output into an independent core (encoder +
// syncer + level enabler). The global Level is AND-ed with the output's own
// level routing so that disabling a level globally also disables it everywhere.
func (cfg Config) buildOutputCore(o Output) (slcore.Core, []io.Closer, error) {
	if cfg.Clock != nil && o.Rolling != nil && o.Rolling.Clock == nil {
		o.Rolling.Clock = cfg.Clock
	}

	// Per-output template config wins; ServiceID falls back to that output's
	// Rolling.ServiceName when not set explicitly.
	tc := o.TemplateConfig
	if tc.ServiceID == "" && o.Rolling != nil && o.Rolling.ServiceName != "" {
		tc.ServiceID = o.Rolling.ServiceName
	}
	enc, err := cfg.buildEncoderFor(o.Encoding, o.EncoderConfig, tc)
	if err != nil {
		return nil, nil, err
	}

	out, closers, err := cfg.openOutputFor(o.Rolling, o.OutputPaths)
	if err != nil {
		return nil, nil, err
	}

	// Precompute this output's level routing once at Build time so the
	// hot-path Enabled check performs zero allocation (calling parseLevels on
	// every entry previously allocated a map per output per entry).
	min := DebugLevel
	if o.Level.l != nil {
		min = o.Level.Level()
	}
	enab := &outputLevelEnabler{
		global: cfg.Level,
		allow:  parseLevels(o.Levels),
		deny:   parseLevels(o.ExcludeLevels),
		min:    min,
	}
	return slcore.NewCore(enc, lockIfNeeded(out), enab), closers, nil
}

// outputLevelEnabler precomputes an Output's level routing so that the
// hot-path Enabled check allocates nothing. It ANDs the global Config.Level
// gate with this output's own routing:
//
//   - Levels 白名单优先（非空时仅白名单中的级别放行）
//   - 否则 ExcludeLevels 黑名单（命中则丢弃）
//   - 否则 Level 最低级别（含；零值视为 DebugLevel 全收）
//
// global is held by value: AtomicLevel stores a shared *atom pointer, so a
// copy is safe to keep past Build.
type outputLevelEnabler struct {
	global AtomicLevel
	allow  map[Level]bool
	deny   map[Level]bool
	min    Level
}

func (e *outputLevelEnabler) Enabled(l Level) bool {
	if !e.global.Enabled(l) {
		return false
	}
	if len(e.allow) > 0 {
		return e.allow[l]
	}
	if len(e.deny) > 0 {
		return !e.deny[l]
	}
	return l >= e.min
}

// openOutputFor resolves a single output's syncer: a RollingWriter (optionally
// fronted by the async writer) when r is set, otherwise the given paths.
func (cfg Config) openOutputFor(r *writer.Config, paths []string) (slcore.WriteSyncer, []io.Closer, error) {
	if r != nil {
		rw, err := writer.NewRollingWriter(r)
		if err != nil {
			return nil, nil, fmt.Errorf("open rolling writer: %w", err)
		}
		var closers []io.Closer
		closers = append(closers, rw)

		var out slcore.WriteSyncer = rw
		if r.Async {
			aw := writer.NewAsyncWriter(rw, r)
			closers = append([]io.Closer{aw}, closers...)
			out = aw
		}
		return out, closers, nil
	}

	p := paths
	if len(p) == 0 {
		p = []string{"stderr"}
	}
	out, closeOut, err := Open(p...)
	if err != nil {
		return nil, nil, err
	}
	return out, []io.Closer{closerFunc(closeOut)}, nil
}

// parseLevels resolves level names to a set. Unknown names are ignored; the
// conventional "trace" (absent from the zap level ladder) maps to DebugLevel.
func parseLevels(names []string) map[Level]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[Level]bool, len(names))
	for _, n := range names {
		lv, err := slcore.ParseLevel(n)
		if err != nil {
			if strings.EqualFold(strings.TrimSpace(n), "trace") {
				lv = DebugLevel
			} else {
				continue
			}
		}
		m[lv] = true
	}
	return m
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

// openOutput resolves the single (backward-compatible) main output.
func (cfg Config) openOutput() (slcore.WriteSyncer, []io.Closer, error) {
	if cfg.Clock != nil && cfg.Rolling != nil && cfg.Rolling.Clock == nil {
		cfg.Rolling.Clock = cfg.Clock
	}
	return cfg.openOutputFor(cfg.Rolling, cfg.OutputPaths)
}

type closerFunc func()

func (f closerFunc) Close() error {
	f()
	return nil
}

func (cfg Config) buildEncoder() (slcore.Encoder, error) {
	return cfg.buildEncoderFor(cfg.Encoding, cfg.EncoderConfig, cfg.templateConfigWithService())
}

// buildEncoderFor builds an encoder from an explicit encoding source so that
// each Config.Output can carry its own format independently of the others.
func (cfg Config) buildEncoderFor(encoding string, encCfg slcore.EncoderConfig, tc encoder.TemplateConfig) (slcore.Encoder, error) {
	switch strings.ToLower(encoding) {
	case "", "json":
		ec := encCfg
		// An unset EncoderConfig gets the conventional JSON key names, so
		// Config{"Encoding":"json"} works out of the box.
		if isZeroEncoderConfig(ec) {
			ec = encoder.DefaultJSONEncoderConfig()
		}
		return encoder.NewJSONEncoder(ec), nil
	case "template":
		if strings.TrimSpace(tc.Template) == "" {
			return nil, errors.New(`sllogger: encoding "template" requires TemplateConfig.Template`)
		}
		return encoder.NewTemplateEncoder(tc)
	case "callinfo":
		return encoder.NewCallInfoEncoder(tc)
	case "requestinfo":
		return encoder.NewRequestInfoEncoder(tc)
	default:
		if ctor, ok := _encoderNameToConstructor[strings.ToLower(encoding)]; ok {
			return ctor(encCfg)
		}
		return nil, fmt.Errorf("sllogger: unrecognized encoding: %q", encoding)
	}
}

// templateConfigWithService fills ServiceID from the rolling config when it
// is not set explicitly.
func (cfg Config) templateConfigWithService() encoder.TemplateConfig {
	return cfg.templateConfigWithServiceFor(cfg.Rolling)
}

// templateConfigWithServiceFor is the per-output variant: ServiceID is taken
// from that output's Rolling.ServiceName (falling back to the global TemplateConfig).
func (cfg Config) templateConfigWithServiceFor(r *writer.Config) encoder.TemplateConfig {
	tc := cfg.TemplateConfig
	if tc.ServiceID == "" && r != nil && r.ServiceName != "" {
		tc.ServiceID = r.ServiceName
	}
	return tc
}
