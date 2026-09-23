package nl2sql

import (
	"context"
	"fmt"
	"os"
)

type Provider interface {
	Name() string
	Generate(ctx context.Context, prompt string, schema string) (string, error)
}

type Config struct {
	Provider  string
	Model     string
	Providers map[string]ProviderConfig
}

type ProviderConfig struct {
	APIKeyEnv string
	Model     string
	BaseURL   string
}

func Resolve(cfg Config) (Provider, error) {
	name := cfg.Provider
	if name == "" {
		name = autoDetect()
	}

	if name == "" {
		return nil, fmt.Errorf("no AI provider detected: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or configure opencode/jcode")
	}

	// Check for OpenAI-compatible providers
	if pcfg, ok := cfg.Providers[name]; ok {
		apiKey := os.Getenv(pcfg.APIKeyEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set %s env var", pcfg.APIKeyEnv)
		}
		baseURL := pcfg.BaseURL
		if baseURL == "" {
			baseURL = defaultBaseURL(name)
		}
		return NewOpenAICompatible(name, apiKey, pcfg.Model, baseURL), nil
	}

	// Auto-detect from configs
	switch name {
	case "opencode", "opencode-go":
		return resolveOpenCode(cfg.Model)
	case "pi":
		return resolvePi()
	case "hermes":
		return resolveHermes()
	case "jcode":
		return resolveJCode()
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set ANTHROPIC_API_KEY env var")
		}
		model := cfg.Model
		if model == "" {
			model = cfg.Providers["anthropic"].Model
		}
		return NewAnthropic(apiKey, model), nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set OPENAI_API_KEY env var")
		}
		model := cfg.Model
		if model == "" {
			model = cfg.Providers["openai"].Model
		}
		return NewOpenAI(apiKey, model), nil
	case "deepseek":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set DEEPSEEK_API_KEY env var")
		}
		model := cfg.Model
		if model == "" {
			model = cfg.Providers["deepseek"].Model
		}
		return NewDeepSeek(apiKey, model), nil
	case "qwen":
		apiKey := os.Getenv("DASHSCOPE_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set DASHSCOPE_API_KEY env var")
		}
		return NewQwen(apiKey, cfg.Providers["qwen"].Model), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func autoDetect() string {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return "anthropic"
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return "openai"
	}
	if os.Getenv("DEEPSEEK_API_KEY") != "" {
		return "deepseek"
	}
	if os.Getenv("DASHSCOPE_API_KEY") != "" {
		return "qwen"
	}
	if os.Getenv("INFLECTION_API_KEY") != "" {
		return "pi"
	}
	if os.Getenv("NOUS_API_KEY") != "" || os.Getenv("HERMES_API_KEY") != "" {
		return "hermes"
	}
	if os.Getenv("OPENCODE_API_KEY") != "" || os.Getenv("ZEN_API_KEY") != "" {
		return "opencode-go"
	}
	// Check opencode auth.json
	if _, _, _, found := DetectOpenCodeConfig(); found {
		return "opencode-go"
	}
	if _, _, _, _, found := DetectJCodeConfig(); found {
		return "jcode"
	}
	return "" // No provider detected
}

func defaultBaseURL(name string) string {
	switch name {
	case "opencode", "opencode-go":
		return "https://opencode.ai/zen/go/v1"
	case "hermes":
		return "https://inference-api.nousresearch.com/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	default:
		return ""
	}
}

func resolveOpenCode(configModel string) (Provider, error) {
	apiKey, model, baseURL, found := DetectOpenCodeConfig()
	if !found {
		return nil, fmt.Errorf("OpenCode not detected: set OPENCODE_API_KEY or ZEN_API_KEY")
	}
	// Use config model if provided, otherwise use detected model
	if configModel != "" {
		model = configModel
	}
	return NewOpenAICompatible("opencode-go", apiKey, model, baseURL), nil
}

func resolvePi() (Provider, error) {
	apiKey, model, _, found := DetectPiConfig()
	if !found {
		return nil, fmt.Errorf("pi not detected: set INFLECTION_API_KEY")
	}
	return NewPi(apiKey, model), nil
}

func resolveHermes() (Provider, error) {
	apiKey, model, baseURL, found := DetectHermesConfig()
	if !found {
		return nil, fmt.Errorf("hermes not detected: set NOUS_API_KEY or HERMES_API_KEY")
	}
	return NewOpenAICompatible("hermes", apiKey, model, baseURL), nil
}

func resolveJCode() (Provider, error) {
	provider, apiKey, model, baseURL, found := DetectJCodeConfig()
	if !found {
		return nil, fmt.Errorf("jcode not detected: no provider configured in ~/.jcode/config.json")
	}
	return NewOpenAICompatible("jcode/"+provider, apiKey, model, baseURL), nil
}
