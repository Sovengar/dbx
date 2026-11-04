package nl2sql

import (
	"context"
	"strings"

	"github.com/sovengar/agentbridge"
)

type Qwen struct {
	apiKey  string
	model   string
	baseURL string
}

func NewQwen(apiKey, model string) *Qwen {
	if model == "" {
		model = "qwen-turbo"
	}
	return NewQwenAt(apiKey, model, qwenDefaultBaseURL)
}

// qwenDefaultBaseURL is Qwen's OpenAI-compatible base.
const qwenDefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

// NewQwenAt is NewQwen with an explicit endpoint. See NewAnthropicAt.
func NewQwenAt(apiKey, model, baseURL string) *Qwen {
	if model == "" {
		model = "qwen-turbo"
	}
	return &Qwen{apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/")}
}

func (q *Qwen) Name() string { return "qwen" }

// Generate asks for SQL through the shared invoker. Qwen sends no max_tokens,
// and the shared invoker omits the field when it is zero.
func (q *Qwen) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	return chat(ctx, "qwen", agentbridge.APIInvoker{
		Endpoint: q.baseURL,
		APIKey:   q.apiKey,
		Model:    q.model,
	}, nil, prompt, schema)
}
