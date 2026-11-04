package nl2sql

import (
	"context"
	"strings"

	"github.com/sovengar/agentbridge"
)

type OpenAI struct {
	apiKey  string
	model   string
	baseURL string
}

func NewOpenAI(apiKey, model string) *OpenAI {
	if model == "" {
		model = "gpt-4o"
	}
	return NewOpenAIAt(apiKey, model, openAIDefaultBaseURL)
}

// openAIDefaultBaseURL is the BASE, with the version path appended by the constructor. See
// anthropicDefaultBaseURL for why it is not the full endpoint.
const openAIDefaultBaseURL = "https://api.openai.com/v1"

// NewOpenAIAt is NewOpenAI with an explicit endpoint. See NewAnthropicAt.
func NewOpenAIAt(apiKey, model, baseURL string) *OpenAI {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAI{apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/")}
}

func (o *OpenAI) Name() string { return "openai" }

// Generate asks for SQL through the shared invoker. The transport is
// agentbridge's; the SQL contract is this package's.
func (o *OpenAI) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	return chat(ctx, "openai", agentbridge.APIInvoker{
		Endpoint:  o.baseURL,
		APIKey:    o.apiKey,
		Model:     o.model,
		MaxTokens: 1024,
	}, nil, prompt, schema)
}
