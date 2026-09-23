package nl2sql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type OpenAICompatible struct {
	apiKey  string
	model   string
	baseURL string
	name    string
}

func NewOpenAICompatible(name, apiKey, model, baseURL string) *OpenAICompatible {
	return &OpenAICompatible{
		name:    name,
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
	}
}

func (o *OpenAICompatible) Name() string { return o.name }

func (o *OpenAICompatible) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(openAIRequest{
		Model: o.model,
		Messages: []openAIMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 1024,
	})

	url := strings.TrimRight(o.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	// OpenCode Go requires x-opencode-session header
	if o.name == "opencode-go" {
		req.Header.Set("x-opencode-session", "dbx-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	}

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
		return "", fmt.Errorf("empty response from %s", o.name)
	}

	sql := strings.TrimSpace(result.Choices[0].Message.Content)
	sql = cleanSQL(sql)
	return sql, nil
}

const defaultOpenCodeBaseURL = "https://opencode.ai/zen/go/v1"

func DetectOpenCodeConfig() (apiKey, model, baseURL string, found bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Check environment variables first
	if key := os.Getenv("OPENCODE_API_KEY"); key != "" {
		apiKey = key
		found = true
	}

	if key := os.Getenv("ZEN_API_KEY"); key != "" {
		apiKey = key
		baseURL = defaultOpenCodeBaseURL
		found = true
	}

	// Check opencode auth.json
	if !found {
		authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
		if authData, err := os.ReadFile(authPath); err == nil {
			var authCfg map[string]struct {
				Type string `json:"type"`
				Key  string `json:"key"`
			}
			if json.Unmarshal(authData, &authCfg) == nil {
				if p, ok := authCfg["opencode-go"]; ok && p.Key != "" {
					apiKey = p.Key
					baseURL = defaultOpenCodeBaseURL
					found = true
				}
			}
		}
	}

	// Check opencode config for a configured baseURL (v2 opencode.jsonc, then v1 opencode.json)
	if configured := detectOpenCodeBaseURL(home); configured != "" {
		baseURL = configured
	}

	if baseURL == "" {
		baseURL = defaultOpenCodeBaseURL
	}

	if model == "" {
		model = "mimo-v2.5"
	}

	return
}

// detectOpenCodeBaseURL reads the opencode config and returns a configured
// provider baseURL, if any. v2 stores config in opencode.jsonc (JSONC); v1 used
// opencode.json. Both live in ~/.config/opencode.
func detectOpenCodeBaseURL(home string) string {
	configDir := filepath.Join(home, ".config", "opencode")
	for _, name := range []string{"opencode.jsonc", "opencode.json"} {
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil {
			continue
		}
		if baseURL := baseURLFromOpenCodeConfig(data); baseURL != "" {
			return baseURL
		}
	}
	return ""
}

// baseURLFromOpenCodeConfig extracts a provider baseURL from an opencode config
// document, supporting the v2 schema (providers.<name>.settings.baseURL) and the
// v1 schema (provider.<name>.options.baseURL). The document may be JSONC.
func baseURLFromOpenCodeConfig(data []byte) string {
	var cfg struct {
		// v2
		Providers map[string]struct {
			Settings struct {
				BaseURL string `json:"baseURL"`
			} `json:"settings"`
		} `json:"providers"`
		// v1
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return ""
	}

	for _, name := range []string{"opencode-go", "opencode", "openai"} {
		if p, ok := cfg.Providers[name]; ok && p.Settings.BaseURL != "" {
			return p.Settings.BaseURL
		}
	}
	for _, name := range []string{"opencode-go", "opencode", "openai"} {
		if p, ok := cfg.Provider[name]; ok && p.Options.BaseURL != "" {
			return p.Options.BaseURL
		}
	}
	return ""
}

// stripJSONC converts a JSONC document (JSON with // and /* */ comments and
// optional trailing commas) into plain JSON. It is string-aware so that values
// containing "//" (e.g. URLs) are preserved.
func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		switch c := src[i]; {
		case c == '"':
			out = append(out, c)
			i++
			for i < len(src) {
				out = append(out, src[i])
				if src[i] == '\\' && i+1 < len(src) {
					out = append(out, src[i+1])
					i += 2
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i < len(src) && !(src[i] == '*' && i+1 < len(src) && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == ',':
			if j := skipJSONCNoise(src, i+1); j < len(src) && (src[j] == '}' || src[j] == ']') {
				i++ // drop trailing comma
				continue
			}
			out = append(out, c)
			i++
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// skipJSONCNoise advances past whitespace and comments starting at i.
func skipJSONCNoise(src []byte, i int) int {
	for i < len(src) {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r':
			i++
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i < len(src) && !(src[i] == '*' && i+1 < len(src) && src[i+1] == '/') {
				i++
			}
			i += 2
		default:
			return i
		}
	}
	return i
}

func DetectHermesConfig() (apiKey, model, baseURL string, found bool) {
	if key := os.Getenv("NOUS_API_KEY"); key != "" {
		apiKey = key
		found = true
	}

	if key := os.Getenv("HERMES_API_KEY"); key != "" {
		apiKey = key
		found = true
	}

	if !found {
		return
	}

	baseURL = "https://inference-api.nousresearch.com/v1"
	model = "Hermes-4-70B"
	return
}

func DetectPiConfig() (apiKey, model, baseURL string, found bool) {
	if key := os.Getenv("INFLECTION_API_KEY"); key != "" {
		apiKey = key
		found = true
	}

	if !found {
		return
	}

	baseURL = "https://layercake.pubwestus3.inf7ks8.com/external/api"
	model = "inflection_3_pi"
	return
}

type PiProvider struct {
	apiKey  string
	model   string
	baseURL string
}

func NewPi(apiKey, model string) *PiProvider {
	if model == "" {
		model = "inflection_3_pi"
	}
	return &PiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://layercake.pubwestus3.inf7ks8.com/external/api",
	}
}

func (p *PiProvider) Name() string { return "pi" }

type piRequest struct {
	Model   string    `json:"model,omitempty"`
	Config  string    `json:"config,omitempty"`
	Context []piMsg   `json:"context"`
}

type piMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type piResponse struct {
	Entries []struct {
		Text string `json:"text"`
	} `json:"entries"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *PiProvider) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(piRequest{
		Config: p.model,
		Context: []piMsg{
			{Type: "Instruction", Text: system},
			{Type: "Human", Text: prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/inference", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var result piResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Entries) == 0 {
		return "", fmt.Errorf("empty response from Pi")
	}

	sql := strings.TrimSpace(result.Entries[0].Text)
	sql = cleanSQL(sql)
	return sql, nil
}

func DetectJCodeConfig() (provider, apiKey, model, baseURL string, found bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	configPath := filepath.Join(home, ".jcode", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var cfg struct {
		Providers map[string]struct {
			APIKey string `json:"api_key"`
			BaseURL string `json:"base_url"`
		} `json:"providers"`
		Model string `json:"model"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	for name, p := range cfg.Providers {
		if p.APIKey != "" {
			provider = name
			apiKey = p.APIKey
			baseURL = p.BaseURL
			found = true
			break
		}
	}

	if model == "" {
		model = cfg.Model
	}

	return
}
