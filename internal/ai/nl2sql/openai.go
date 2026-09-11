package nl2sql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAI struct {
	apiKey string
	model  string
}

func NewOpenAI(apiKey, model string) *OpenAI {
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAI{apiKey: apiKey, model: model}
}

func (o *OpenAI) Name() string { return "openai" }

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMsg     `json:"messages"`
	MaxTokens int            `json:"max_tokens"`
}

type openAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (o *OpenAI) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(openAIRequest{
		Model: o.model,
		Messages: []openAIMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 1024,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from OpenAI")
	}

	sql := strings.TrimSpace(result.Choices[0].Message.Content)
	sql = cleanSQL(sql)
	return sql, nil
}
