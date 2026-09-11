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

package encoder

import "github.com/maxhaosl/sllogger/slcore"

// Preset templates from doc/go_call_info_logger_design.md:
//
// 1.1 CALL_INFO 日志:
//
//	date|log_type|service_Id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg
//
// 1.2 RequestInfo 日志:
//
//	date|log_type|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg
const (
	// CallInfoTemplate is the preset CALL_INFO field template.
	CallInfoTemplate = "date|log_level|service_id|trace_id|span_id|mobile|userId|clientId|url|method|useTime|serverIp|buss_Id|log_msg"

	// RequestInfoTemplate is the preset RequestInfo field template.
	RequestInfoTemplate = "date|log_level|service_id|trace_id|span_id|header|reqheader|method|rateLimiter|log_msg"
)

// NewCallInfoEncoder returns a TemplateEncoder using the preset CALL_INFO
// template. Custom renderers and other options from cfg are preserved.
func NewCallInfoEncoder(cfg TemplateConfig) (*TemplateEncoder, error) {
	if cfg.Template == "" {
		cfg.Template = CallInfoTemplate
	}
	return NewTemplateEncoder(cfg)
}

// NewRequestInfoEncoder returns a TemplateEncoder using the preset
// RequestInfo template.
func NewRequestInfoEncoder(cfg TemplateConfig) (*TemplateEncoder, error) {
	if cfg.Template == "" {
		cfg.Template = RequestInfoTemplate
	}
	return NewTemplateEncoder(cfg)
}

// RegisterRenderer registers (or overrides) a field renderer on the given
// config. It returns cfg for chaining.
func RegisterRenderer(cfg *TemplateConfig, name string, r Renderer) *TemplateConfig {
	if cfg.Renderers == nil {
		cfg.Renderers = make(map[string]Renderer)
	}
	cfg.Renderers[name] = r
	return cfg
}

// compile-time check that preset encoders satisfy the Encoder interface.
var (
	_ slcore.Encoder = (*TemplateEncoder)(nil)
)
