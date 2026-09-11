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

type Qwen struct {
	apiKey string
	model  string
}

func NewQwen(apiKey, model string) *Qwen {
	if model == "" {
		model = "qwen-turbo"
	}
	return &Qwen{apiKey: apiKey, model: model}
}

func (q *Qwen) Name() string { return "qwen" }

type qwenRequest struct {
	Model    string       `json:"model"`
	Messages []qwenMsg    `json:"messages"`
}

type qwenMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type qwenResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (q *Qwen) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(qwenRequest{
		Model: q.model,
		Messages: []qwenMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+q.apiKey)

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

	var result qwenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from Qwen")
	}

	sql := strings.TrimSpace(result.Choices[0].Message.Content)
	sql = cleanSQL(sql)
	return sql, nil
}
