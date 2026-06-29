// Package ai defines the provider-agnostic seam for Zorail's future AI
// features (summarization, spam scoring, OTP/link extraction). No provider is
// implemented yet — this is the contract a later phase will satisfy with
// pluggable backends (Claude, Mistral, Ollama, ...), selected by config.
//
// Keeping this interface narrow and content-only (it takes text/HTML, not a
// full model.Message) means providers never see storage details and can be
// unit-tested in isolation.
package ai

import "context"

// Provider is one configured AI backend. Implementations live in subpackages
// (e.g. ai/claude, ai/ollama) and are constructed from config at startup.
type Provider interface {
	// Name identifies the provider for logging and config (e.g. "claude").
	Name() string

	// Complete runs a single prompt and returns the model's text response.
	// Higher-level features (Summarize, ClassifySpam, ExtractOTP, ExtractLinks)
	// will be built on top of this primitive in a later phase.
	Complete(ctx context.Context, req Request) (string, error)
}

// Request is a minimal, provider-neutral completion request.
type Request struct {
	System    string  // system prompt
	Prompt    string  // user prompt (typically the email content)
	MaxTokens int     // 0 means provider default
	Temp      float64 // sampling temperature
}

// Registry maps a provider name to a constructed Provider, so config can pick
// which one (or several) to enable per API key in a later phase.
type Registry map[string]Provider
