package nl2sql

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sovengar/ai"
)

// ModelSource returns the model catalog of the configured provider behind the
// shared ai.ModelSource interface, so dbx holds no endpoint or /models parsing
// of its own. The answer is cached: the picker queries it on every open.
func ModelSource(cfg Config) (ai.ModelSource, error) {
	name := cfg.Provider
	if name == "" {
		name = autoDetect()
	}
	if name == "" {
		return nil, fmt.Errorf("no AI provider detected: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or configure opencode/jcode")
	}

	var apiKey, baseURL string
	if pcfg, ok := cfg.Providers[name]; ok {
		apiKey = os.Getenv(pcfg.APIKeyEnv)
		baseURL = pcfg.BaseURL
		if baseURL == "" {
			baseURL = defaultBaseURL(name)
		}
	} else {
		var found bool
		switch name {
		case "opencode", "opencode-go":
			apiKey, _, baseURL, found = DetectOpenCodeConfig()
		case "hermes":
			apiKey, _, baseURL, found = DetectHermesConfig()
		case "jcode":
			_, apiKey, _, baseURL, found = DetectJCodeConfig()
		case "pi":
			apiKey = os.Getenv("INFLECTION_API_KEY")
			found = apiKey != ""
		default:
			return nil, fmt.Errorf("provider %s exposes no /models endpoint", name)
		}
		if !found {
			return nil, fmt.Errorf("provider %s not detected", name)
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("provider %s exposes no /models endpoint", name)
	}

	return &ai.CachedSource{
		Source: ai.APISource{Endpoint: baseURL, APIKey: apiKey},
		TTL:    time.Minute,
	}, nil
}

// Models lists the model IDs of the configured provider. It is the one call the
// CLI and the TUI need, so neither repeats the source wiring.
func Models(ctx context.Context, cfg Config) ([]string, error) {
	source, err := ModelSource(cfg)
	if err != nil {
		return nil, err
	}
	options, err := source.Options(ctx, ai.Agent{Name: cfg.Provider})
	if err != nil {
		return nil, err
	}
	return options.Models, nil
}
