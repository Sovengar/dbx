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

type DeepSeek struct {
	apiKey string
	model  string
}

func NewDeepSeek(apiKey, model string) *DeepSeek {
	if model == "" {
		model = "deepseek-chat"
	}
	return &DeepSeek{apiKey: apiKey, model: model}
}

func (d *DeepSeek) Name() string { return "deepseek" }

type deepSeekRequest struct {
	Model     string        `json:"model"`
	Messages  []deepSeekMsg `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type deepSeekMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (d *DeepSeek) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(deepSeekRequest{
		Model: d.model,
		Messages: []deepSeekMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 1024,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.deepseek.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result deepSeekResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from DeepSeek")
	}

	sql := strings.TrimSpace(result.Choices[0].Message.Content)
	sql = cleanSQL(sql)
	return sql, nil
}
