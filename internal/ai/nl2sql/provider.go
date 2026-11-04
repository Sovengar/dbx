package nl2sql

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// detected is what a provider's own discovery found on this machine.
type detected struct {
	apiKey  string
	model   string
	baseURL string
	// agent is the name the provider reports; empty means the spec's name. Jcode
	// qualifies it with the upstream provider (`jcode/openai`).
	agent string
	found bool
}

// providerSpec is the ONE place a built-in provider is described: the env vars
// that identify it, its default endpoint, how it discovers itself, and how it is
// built. Resolve, autoDetect and defaultBaseURL all read this table instead of
// each repeating the list — the three copies are how qwen once ignored
// `ai.model` while the other four honoured it.
type providerSpec struct {
	name        string
	envKeys     []string
	defaultBase string
	// discover is set for providers whose key and endpoint live in a config
	// file; an env-only provider leaves it nil.
	discover func() detected
	build    func(apiKey, model, baseURL string) Provider
}

// hermesDefaultBaseURL is the Nous inference endpoint.
const hermesDefaultBaseURL = "https://inference-api.nousresearch.com/v1"

// providerSpecs is ordered: autoDetect takes the first entry whose key is
// present, so a machine with both ANTHROPIC_API_KEY and OPENAI_API_KEY keeps
// picking anthropic, as it always did.
var providerSpecs = []providerSpec{
	{
		name: "anthropic", envKeys: []string{"ANTHROPIC_API_KEY"},
		defaultBase: anthropicDefaultBaseURL,
		build: func(apiKey, model, baseURL string) Provider {
			return NewAnthropicAt(apiKey, model, baseURL)
		},
	},
	{
		name: "openai", envKeys: []string{"OPENAI_API_KEY"},
		defaultBase: openAIDefaultBaseURL,
		build: func(apiKey, model, baseURL string) Provider {
			return NewOpenAIAt(apiKey, model, baseURL)
		},
	},
	{
		name: "deepseek", envKeys: []string{"DEEPSEEK_API_KEY"},
		defaultBase: deepSeekDefaultBaseURL,
		build: func(apiKey, model, baseURL string) Provider {
			return NewDeepSeekAt(apiKey, model, baseURL)
		},
	},
	{
		name: "qwen", envKeys: []string{"DASHSCOPE_API_KEY"},
		defaultBase: qwenDefaultBaseURL,
		build: func(apiKey, model, baseURL string) Provider {
			return NewQwenAt(apiKey, model, baseURL)
		},
	},
	{
		name: "pi", envKeys: []string{"INFLECTION_API_KEY"},
		defaultBase: piDefaultBaseURL,
		build: func(apiKey, model, baseURL string) Provider {
			return NewPiAt(apiKey, model, baseURL)
		},
	},
	{
		name: "hermes", envKeys: []string{"NOUS_API_KEY", "HERMES_API_KEY"},
		defaultBase: hermesDefaultBaseURL,
		discover: func() detected {
			apiKey, model, baseURL, found := DetectHermesConfig()
			return detected{apiKey: apiKey, model: model, baseURL: baseURL, found: found}
		},
		build: func(apiKey, model, baseURL string) Provider {
			return NewOpenAICompatible("hermes", apiKey, model, baseURL)
		},
	},
	{
		name: "opencode-go", envKeys: []string{"OPENCODE_API_KEY", "ZEN_API_KEY"},
		defaultBase: defaultOpenCodeBaseURL,
		discover: func() detected {
			apiKey, model, baseURL, found := DetectOpenCodeConfig()
			return detected{apiKey: apiKey, model: model, baseURL: baseURL, found: found}
		},
		build: func(apiKey, model, baseURL string) Provider {
			return NewOpenAICompatible("opencode-go", apiKey, model, baseURL)
		},
	},
	{
		name: "jcode",
		discover: func() detected {
			provider, apiKey, model, baseURL, found := DetectJCodeConfig()
			return detected{
				apiKey: apiKey, model: model, baseURL: baseURL,
				agent: "jcode/" + provider, found: found,
			}
		},
		build: func(apiKey, model, baseURL string) Provider {
			// The agent name is qualified by the discovery above; this builder is
			// only reached through it.
			return NewOpenAICompatible("jcode", apiKey, model, baseURL)
		},
	},
}

// aliases maps a configured name that is not the spec name onto it.
var providerAliases = map[string]string{"opencode": "opencode-go"}

func specByName(name string) (providerSpec, bool) {
	if canonical, ok := providerAliases[name]; ok {
		name = canonical
	}
	for _, spec := range providerSpecs {
		if spec.name == name {
			return spec, true
		}
	}
	return providerSpec{}, false
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
	// model, a gateway — and an entry for a built-in used to shadow it.
	if spec, ok := specByName(name); ok {
		return resolveBuiltin(cfg, name, spec)
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

// resolveBuiltin builds one built-in provider: discovery when the spec has it,
// otherwise the first env var that is set, then the one model rule and the one
// builder.
func resolveBuiltin(cfg Config, name string, spec providerSpec) (Provider, error) {
	apiKey := firstEnv(spec.envKeys)
	model := ""
	baseURL := spec.defaultBase
	agent := spec.name

	if spec.discover != nil {
		found := spec.discover()
		if !found.found {
			return nil, notDetected(spec)
		}
		apiKey = found.apiKey
		model = found.model
		if found.baseURL != "" {
			baseURL = found.baseURL
		}
		if found.agent != "" {
			agent = found.agent
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found: set %s env var", strings.Join(spec.envKeys, " or "))
	}
	model = overrideModel(model, modelFor(cfg, name))
	if agent != spec.name {
		return NewOpenAICompatible(agent, apiKey, model, baseURL), nil
	}
	return spec.build(apiKey, model, baseURL), nil
}

// notDetected names the variable (or the file) the user must provide. The
// wording is what a contract test asserts, so it stays "not detected".
func notDetected(spec providerSpec) error {
	if len(spec.envKeys) > 0 {
		return fmt.Errorf("%s not detected: set %s", spec.name, strings.Join(spec.envKeys, " or "))
	}
	return fmt.Errorf("%s not detected: no provider configured in ~/.jcode/config.json", spec.name)
}

func firstEnv(keys []string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// modelFor picks the model for a provider: the global `ai.model` when it is set,
// and the provider's own `ai.providers.<name>.model` when it is not.
func modelFor(cfg Config, name string) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	if canonical, ok := providerAliases[name]; ok {
		name = canonical
	}
	return cfg.Providers[name].Model
}

// autoDetect returns the first provider this machine can answer with: the first
// spec whose env var is set, then the first that discovers itself on disk.
func autoDetect() string {
	for _, spec := range providerSpecs {
		if firstEnv(spec.envKeys) != "" {
			return spec.name
		}
	}
	// Check opencode auth.json, then jcode's config.
	for _, spec := range providerSpecs {
		if spec.discover == nil {
			continue
		}
		if spec.name == "hermes" {
			continue // env-only discovery; a stale cache is not a reason to pick it
		}
		if spec.discover().found {
			return spec.name
		}
	}
	return "" // No provider detected
}

// defaultBaseURL is the endpoint a config-only provider falls back to.
func defaultBaseURL(name string) string {
	if canonical, ok := providerAliases[name]; ok {
		name = canonical
	}
	for _, spec := range providerSpecs {
		if spec.name == name {
			return spec.defaultBase
		}
	}
	return ""
}

// overrideModel is the whole rule: a hand-configured model wins, and the detected one is
// the fallback.
func overrideModel(detected, configured string) string {
	if configured != "" {
		return configured
	}
	return detected
}
