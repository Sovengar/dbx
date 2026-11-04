package nl2sql

import (
	"context"
	"strings"

	"github.com/sovengar/agentbridge"
)

type DeepSeek struct {
	apiKey  string
	model   string
	baseURL string
}

func NewDeepSeek(apiKey, model string) *DeepSeek {
	if model == "" {
		model = "deepseek-chat"
	}
	return NewDeepSeekAt(apiKey, model, deepSeekDefaultBaseURL)
}

// deepSeekDefaultBaseURL is DeepSeek's OpenAI-compatible base.
const deepSeekDefaultBaseURL = "https://api.deepseek.com/v1"

// NewDeepSeekAt is NewDeepSeek with an explicit endpoint. See NewAnthropicAt.
func NewDeepSeekAt(apiKey, model, baseURL string) *DeepSeek {
	if model == "" {
		model = "deepseek-chat"
	}
	return &DeepSeek{apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/")}
}

func (d *DeepSeek) Name() string { return "deepseek" }

// Generate asks for SQL through the shared invoker.
func (d *DeepSeek) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	return chat(ctx, "deepseek", agentbridge.APIInvoker{
		Endpoint:  d.baseURL,
		APIKey:    d.apiKey,
		Model:     d.model,
		MaxTokens: 1024,
	}, nil, prompt, schema)
}
