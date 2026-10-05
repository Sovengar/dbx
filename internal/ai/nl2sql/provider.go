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

	// A BUILT-IN NAME IS BUILT-IN, even when the config also carries an entry for
	// it. The providers table exists to name ADDITIONAL endpoints — a self-hosted
	// model, a gateway — and an entry for a built-in used to shadow it: setting
	// `ai.providers.anthropic.model` sent "anthropic" down the OpenAI-compatible
	// arm, where the API key variable is whatever the entry says and is empty
	// unless the user also set it. So the one thing a user could do to pick a
	// model was the one thing that broke their provider.
	switch name {
	case "opencode", "opencode-go":
		return resolveOpenCode(cfg.Model)
	case "pi":
		return resolvePi(cfg.Model)
	case "hermes":
		return resolveHermes(cfg.Model)
	case "jcode":
		return resolveJCode(cfg.Model)
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set ANTHROPIC_API_KEY env var")
		}
		return NewAnthropic(apiKey, modelFor(cfg, name)), nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set OPENAI_API_KEY env var")
		}
		return NewOpenAI(apiKey, modelFor(cfg, name)), nil
	case "deepseek":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set DEEPSEEK_API_KEY env var")
		}
		return NewDeepSeek(apiKey, modelFor(cfg, name)), nil
	case "qwen":
		apiKey := os.Getenv("DASHSCOPE_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set DASHSCOPE_API_KEY env var")
		}
		// This arm used to read only the providers table, so `ai.model` did
		// nothing here while it worked for the three arms above it.
		return NewQwen(apiKey, modelFor(cfg, name)), nil
	}

	// Not a built-in: the config table names it, and that is the whole point of
	// the table.
	if pcfg, ok := cfg.Providers[name]; ok {
		apiKey := os.Getenv(pcfg.APIKeyEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key not found: set %s env var", pcfg.APIKeyEnv)
		}
		baseURL := pcfg.BaseURL
		if baseURL == "" {
			baseURL = defaultBaseURL(name)
		}
		return NewOpenAICompatible(name, apiKey, modelFor(cfg, name), baseURL), nil
	}

	return nil, fmt.Errorf("unknown provider: %s", name)
}

// modelFor picks the model for a provider: the global `ai.model` when it is set,
// and the provider's own `ai.providers.<name>.model` when it is not.
//
// One rule, one place, every provider. It used to be written out four times with
// qwen left out, which is how the qwen arm ended up ignoring the global setting —
// and the duplication is why nobody noticed that the fifth path, the providers
// table, ignored it too.
func modelFor(cfg Config, name string) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	return cfg.Providers[name].Model
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

// The four file-and-environment backed providers, and the one rule they share: a model
// configured by hand overrides whatever the detector found, and when none is configured
// the detector's own model is used.
//
// Only resolveOpenCode took the configured model as an argument; the other three read the
// detector's value and stopped, so `ai.model` silently did nothing for pi, hermes and
// jcode. Three copies of the same six lines, each written slightly differently, which is
// what let the fourth be different.
func resolveOpenCode(configModel string) (Provider, error) {
	apiKey, model, baseURL, found := DetectOpenCodeConfig()
	if !found {
		return nil, fmt.Errorf("OpenCode not detected: set OPENCODE_API_KEY or ZEN_API_KEY")
	}
	return NewOpenAICompatible("opencode-go", apiKey, overrideModel(model, configModel), baseURL), nil
}

func resolvePi(configModel string) (Provider, error) {
	apiKey, model, _, found := DetectPiConfig()
	if !found {
		return nil, fmt.Errorf("pi not detected: set INFLECTION_API_KEY")
	}
	return NewPi(apiKey, overrideModel(model, configModel)), nil
}

func resolveHermes(configModel string) (Provider, error) {
	apiKey, model, baseURL, found := DetectHermesConfig()
	if !found {
		return nil, fmt.Errorf("hermes not detected: set NOUS_API_KEY or HERMES_API_KEY")
	}
	return NewOpenAICompatible("hermes", apiKey, overrideModel(model, configModel), baseURL), nil
}

func resolveJCode(configModel string) (Provider, error) {
	provider, apiKey, model, baseURL, found := DetectJCodeConfig()
	if !found {
		return nil, fmt.Errorf("jcode not detected: no provider configured in ~/.jcode/config.json")
	}
	return NewOpenAICompatible("jcode/"+provider, apiKey, overrideModel(model, configModel), baseURL), nil
}

// overrideModel is the whole rule: a hand-configured model wins, and the detected one is
// the fallback.
func overrideModel(detected, configured string) string {
	if configured != "" {
		return configured
	}
	return detected
}
