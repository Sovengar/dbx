package nl2sql

// The contracts of provider resolution, asserted as properties over the environment rather
// than one scenario at a time.
//
// Resolve is a lookup over two inputs the user does not type at the same moment: the
// CONFIG, which names a provider and a model, and the ENVIRONMENT, which holds the API
// keys and occasionally a config file. The whole function is the negotiation between them,
// and every branch in it is a sentence about which one wins.
//
// The fixtures isolate both. Every key this package reads is cleared with t.Setenv, so a
// developer with a real ANTHROPIC_API_KEY in their shell cannot change a result here, and
// HOME is redirected at a temp directory so the config-file detectors cannot find the real
// ~/.config/opencode or ~/.jcode.
//
// Three things it promises:
//
//	P1  an explicit provider in the config is the one that is used. Auto-detection only
//	    fills in a blank.
//	P2  a provider that needs a key says WHICH key is missing, by name. "authentication
//	    failed" three steps later is the failure mode this prevents.
//	P3  the model comes from the config, and every provider resolves it the same way.
//	    This is the promise that was broken: qwen and the OpenAI-compatible branch
//	    ignored the configured model entirely while their three siblings consulted it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// everyEnvVar is every variable this package reads, in one place so a fixture that wants a
// clean environment cannot forget one. Adding a key to the source without adding it here
// would make this file's results depend on the developer's shell.
var everyEnvVar = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"DEEPSEEK_API_KEY",
	"DASHSCOPE_API_KEY",
	"INFLECTION_API_KEY",
	"NOUS_API_KEY",
	"HERMES_API_KEY",
	"OPENCODE_API_KEY",
	"ZEN_API_KEY",
}

// isolate clears every key this package reads and points HOME at an empty directory, so a
// config-file detector finds nothing and auto-detection has to be earned rather than
// inherited from the developer's shell.
func isolate(t *testing.T) string {
	t.Helper()
	for _, k := range everyEnvVar {
		t.Setenv(k, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

// ---------------------------------------------------------------------------
// P1: an explicit provider wins
// ---------------------------------------------------------------------------

// Scenario: El proveedor del config manda; la autodeteccion solo rellena huecos.
//
// This is the distinction that makes the config file worth having. `dbx ask --provider
// deepseek` with an ANTHROPIC_API_KEY in the environment must reach DeepSeek, because a
// flag that loses to whatever key happens to be exported is a flag that does not work.
func TestResolve_AnExplicitProviderBeatsAutoDetection(t *testing.T) {
	isolate(t)
	// A key for a provider we are NOT asking for.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-not-this-one")

	for _, tc := range []struct {
		provider string
		keyEnv   string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"deepseek", "DEEPSEEK_API_KEY"},
		{"qwen", "DASHSCOPE_API_KEY"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv(tc.keyEnv, "sk-the-one-we-asked-for")
			p, err := Resolve(Config{Provider: tc.provider})
			if err != nil {
				t.Fatalf("Resolve(%q) failed with an Anthropic key also set: %v", tc.provider, err)
			}
			if p == nil {
				t.Fatalf("Resolve(%q) returned no provider and no error", tc.provider)
			}
			if got := p.Name(); !strings.Contains(strings.ToLower(got), tc.provider) {
				t.Errorf("Resolve(%q) built the %q provider", tc.provider, got)
			}
		})
	}
}

// Scenario: Sin proveedor en el config, se autodetecta, y en ORDEN DE PREFERENCIA.
//
// The order is a decision — which provider a user is assumed to want when they have more
// than one key exported — and it is only observable when two keys are set at once. So it
// is asserted pairwise rather than one key at a time.
func TestResolve_WithNoProviderItDetectsOneInPreferenceOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		keys  []string
		want  string
		unset []string
	}{
		{
			name: "only Anthropic",
			keys: []string{"ANTHROPIC_API_KEY"}, want: "anthropic",
		},
		{
			name: "only OpenAI",
			keys: []string{"OPENAI_API_KEY"}, want: "openai",
		},
		{
			name: "only DeepSeek",
			keys: []string{"DEEPSEEK_API_KEY"}, want: "deepseek",
		},
		{
			name: "only DashScope",
			keys: []string{"DASHSCOPE_API_KEY"}, want: "qwen",
		},
		{
			name: "only Inflection",
			keys: []string{"INFLECTION_API_KEY"}, want: "pi",
		},
		{
			name: "only Nous",
			keys: []string{"NOUS_API_KEY"}, want: "hermes",
		},
		{
			name: "Hermes' own variable is as good as Nous'",
			keys: []string{"HERMES_API_KEY"}, want: "hermes",
		},
		{
			name: "only OpenCode",
			keys: []string{"OPENCODE_API_KEY"}, want: "opencode-go",
		},
		{
			name: "Zen is an OpenCode key too",
			keys: []string{"ZEN_API_KEY"}, want: "opencode-go",
		},
		{
			name: "Anthropic beats OpenAI",
			keys: []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, want: "anthropic",
		},
		{
			name: "OpenAI beats DeepSeek",
			keys: []string{"OPENAI_API_KEY", "DEEPSEEK_API_KEY"}, want: "openai",
		},
		{
			name: "DeepSeek beats DashScope",
			keys: []string{"DEEPSEEK_API_KEY", "DASHSCOPE_API_KEY"}, want: "deepseek",
		},
		{
			name: "DashScope beats Inflection",
			keys: []string{"DASHSCOPE_API_KEY", "INFLECTION_API_KEY"}, want: "qwen",
		},
		{
			name: "Nous beats OpenCode",
			keys: []string{"NOUS_API_KEY", "OPENCODE_API_KEY"}, want: "hermes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			for _, k := range tc.keys {
				t.Setenv(k, "sk-test")
			}
			p, err := Resolve(Config{})
			if err != nil {
				t.Fatalf("Resolve with %v failed: %v", tc.keys, err)
			}
			if got := p.Name(); !strings.Contains(strings.ToLower(got), tc.want) {
				t.Errorf("with %v the provider is %q, want %q", tc.keys, got, tc.want)
			}
		})
	}
}

// Scenario: Sin ninguna clave, el error DICE QUE FALTA y como arreglarlo.
//
// The message is the whole output here: a user with no keys configured gets one line, and
// it has to name the variables that would fix it. An error that says "no provider
// detected" and stops is the failure mode.
func TestResolve_WithNothingConfiguredTheErrorNamesTheVariables(t *testing.T) {
	isolate(t)

	p, err := Resolve(Config{})
	if err == nil {
		t.Fatalf("Resolve with no keys and no config files returned the provider %v", p)
	}
	if p != nil {
		t.Errorf("Resolve returned a provider AND an error: %v", p)
	}

	for _, want := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %s: %q", want, err)
		}
	}
	// And it is not a bare "unknown provider", which would send a user
	// looking at their config instead of their shell.
	if strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("an unconfigured shell reported an unknown PROVIDER: %q", err)
	}

	t.Run("an unknown provider is reported as unknown", func(t *testing.T) {
		isolate(t)
		_, err := Resolve(Config{Provider: "not-a-provider"})
		if err == nil {
			t.Fatal("an unknown provider was accepted")
		}
		if !strings.Contains(err.Error(), "not-a-provider") {
			t.Errorf("the error does not name the provider: %q", err)
		}
	})
}

// ---------------------------------------------------------------------------
// P2: a missing key names itself
// ---------------------------------------------------------------------------

// Scenario: Cada proveedor dice QUE variable falta, por su nombre.
//
// Each provider reads a differently named variable — DashScope for qwen, Nous or Hermes
// for hermes — and a message that named the wrong one would send a user to set a variable
// that does nothing. Asserted per provider, for both the named and the configured paths.
func TestResolve_AMissingKeyIsReportedByName(t *testing.T) {
	named := []struct {
		provider string
		want     string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"deepseek", "DEEPSEEK_API_KEY"},
		{"qwen", "DASHSCOPE_API_KEY"},
	}
	for _, tc := range named {
		t.Run(tc.provider, func(t *testing.T) {
			isolate(t)
			_, err := Resolve(Config{Provider: tc.provider})
			if err == nil {
				t.Fatalf("Resolve(%q) with no key succeeded", tc.provider)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error for %q does not name %s: %q", tc.provider, tc.want, err)
			}
			if !strings.Contains(err.Error(), "API key") && !strings.Contains(err.Error(), "API_KEY") {
				t.Errorf("the error for %q does not say a key is what is missing: %q", tc.provider, err)
			}
		})
	}

	t.Run("a configured provider is asked for ITS OWN variable", func(t *testing.T) {
		isolate(t)
		// A provider declared in the config with a custom variable name, which is
		// how a self-hosted OpenAI-compatible endpoint is pointed at.
		_, err := Resolve(Config{
			Provider: "ollama",
			Providers: map[string]ProviderConfig{
				"ollama": {APIKeyEnv: "MY_OLLAMA_KEY", Model: "llama3", BaseURL: "http://localhost:11434/v1"},
			},
		})
		if err == nil {
			t.Fatal("a configured provider with no key succeeded")
		}
		if !strings.Contains(err.Error(), "MY_OLLAMA_KEY") {
			t.Errorf("the error does not name the CONFIGURED variable: %q", err)
		}
	})
}

// ---------------------------------------------------------------------------
// P3: the model comes from the config
// ---------------------------------------------------------------------------

// Scenario: El modelo sale del config, y TODOS los proveedores lo resuelven igual.
//
// `ai.model` is the global setting and `ai.providers.<name>.model` is the per-provider one.
// The three named providers consulted the global one and fell back to the per-provider one;
// qwen consulted NEITHER and used the per-provider model directly, and the
// OpenAI-compatible branch did the same. That is not a design difference — it is one arm
// of a switch that was extended without the new arm copying the rule — and it means
// setting `ai.model` silently does nothing for qwen and for every self-hosted endpoint.
//
// The rule pinned here: the configured global model wins, the per-provider model is the
// fallback, and every provider does the same thing. Which of the two a user should be able
// to override is a separate question; that they must behave ALIKE is not.
func TestResolve_EveryProviderResolvesTheModelTheSameWay(t *testing.T) {
	const globalModel = "the-global-model"
	const providerModel = "the-provider-model"

	providers := []struct {
		name    string
		keyEnv  string
		config  map[string]ProviderConfig
		wantFor string // which model the provider should end up carrying
	}{
		{
			name: "anthropic", keyEnv: "ANTHROPIC_API_KEY", wantFor: globalModel,
			config: map[string]ProviderConfig{"anthropic": {Model: providerModel}},
		},
		{
			name: "openai", keyEnv: "OPENAI_API_KEY", wantFor: globalModel,
			config: map[string]ProviderConfig{"openai": {Model: providerModel}},
		},
		{
			name: "deepseek", keyEnv: "DEEPSEEK_API_KEY", wantFor: globalModel,
			config: map[string]ProviderConfig{"deepseek": {Model: providerModel}},
		},
		{
			name: "qwen", keyEnv: "DASHSCOPE_API_KEY", wantFor: globalModel,
			config: map[string]ProviderConfig{"qwen": {Model: providerModel}},
		},
		{
			name: "a self-hosted OpenAI-compatible endpoint", keyEnv: "MY_KEY", wantFor: globalModel,
			config: map[string]ProviderConfig{"my-llm": {
				APIKeyEnv: "MY_KEY", Model: providerModel, BaseURL: "http://localhost:1234/v1",
			}},
		},
	}

	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			t.Setenv(tc.keyEnv, "sk-test")
			name := tc.name
			if name == "a self-hosted OpenAI-compatible endpoint" {
				name = "my-llm"
			}

			p, err := Resolve(Config{Provider: name, Model: globalModel, Providers: tc.config})
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", name, err)
			}
			if got := providerModelOf(t, p); got != tc.wantFor {
				t.Errorf("with a global model of %q and a per-provider model of %q, %q carries %q, want %q",
					globalModel, providerModel, name, got, tc.wantFor)
			}

			// And with NO global model, the per-provider one is the fallback.
			// Otherwise "ai.model" could not be unset and the fallback would be
			// unreachable, which is what makes it untestable today.
			p2, err := Resolve(Config{Provider: name, Providers: tc.config})
			if err != nil {
				t.Fatalf("Resolve(%q) without a global model failed: %v", name, err)
			}
			if got := providerModelOf(t, p2); got != providerModel {
				t.Errorf("with no global model, %q carries %q, want the per-provider %q", name, got, providerModel)
			}
		})
	}
}

// providerModelOf reads the model back off a resolved provider. The Provider interface
// deliberately does not expose it, so the test reaches for the concrete types — which is
// the point: an interface that cannot be asked what model it is cannot be tested for
// carrying the right one.
func providerModelOf(t *testing.T, p Provider) string {
	t.Helper()
	// The fields are unexported, which is why this test lives in the package
	// rather than beside it: the Provider interface cannot be asked what model
	// it carries, so an interface-level test could not check the one thing
	// this promise is about.
	switch v := p.(type) {
	case *Anthropic:
		return v.model
	case *OpenAI:
		return v.model
	case *DeepSeek:
		return v.model
	case *Qwen:
		return v.model
	case *OpenAICompatible:
		return v.model
	default:
		t.Fatalf("Resolve built a %T, which the model test does not know how to read", p)
		return ""
	}
}

// ---------------------------------------------------------------------------
// The base URL fallback
// ---------------------------------------------------------------------------

// Scenario: Una URL vacia se resuelve por NOMBRE, y las que no tienen una se quedan vacias.
//
// A provider declared in the config with no base URL gets the well-known endpoint for its
// name — so configuring `deepseek` with just a key works with no URL typed. A name with no
// well-known endpoint gets the empty string, because guessing one would send a request
// somewhere arbitrary.
func TestDefaultBaseURL_KnownNamesGetTheirEndpointAndOthersGetNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"opencode", "https://opencode.ai/zen/go/v1"},
		{"opencode-go", "https://opencode.ai/zen/go/v1"},
		{"hermes", "https://inference-api.nousresearch.com/v1"},
		{"deepseek", "https://api.deepseek.com/v1"},
		{"anthropic", ""},
		{"openai", ""},
		{"qwen", ""},
		{"", ""},
		{"something-else", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultBaseURL(tc.name); got != tc.want {
				t.Errorf("defaultBaseURL(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}

	t.Run("a configured provider with no URL gets its name's endpoint", func(t *testing.T) {
		isolate(t)
		t.Setenv("MY_DEEPSEEK_KEY", "sk-test")
		p, err := Resolve(Config{
			Provider: "my-deepseek",
			Providers: map[string]ProviderConfig{
				"my-deepseek": {APIKeyEnv: "MY_DEEPSEEK_KEY", Model: "m"},
			},
		})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		oc, ok := p.(*OpenAICompatible)
		if !ok {
			t.Fatalf("Resolve built a %T, want an *OpenAICompatible", p)
		}
		if oc.baseURL != defaultBaseURL("my-deepseek") {
			t.Errorf("the base URL is %q, want %q", oc.baseURL, defaultBaseURL("my-deepseek"))
		}
	})

	t.Run("a configured URL is used as given", func(t *testing.T) {
		isolate(t)
		t.Setenv("MY_KEY", "sk-test")
		const mine = "http://10.0.0.5:8080/v1"
		p, err := Resolve(Config{
			Provider: "my-llm",
			Providers: map[string]ProviderConfig{
				"my-llm": {APIKeyEnv: "MY_KEY", Model: "m", BaseURL: mine},
			},
		})
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if got := p.(*OpenAICompatible).baseURL; got != mine {
			t.Errorf("the base URL is %q, want the configured %q", got, mine)
		}
	})
}

// ---------------------------------------------------------------------------
// The config-file detectors
// ---------------------------------------------------------------------------

// Scenario: Un auth.json en el HOME de mentira cuenta como una deteccion.
//
// The two file-backed detectors read paths under HOME, so redirecting HOME is enough to
// make them succeed or fail on demand — which is the only way to reach their success arms
// without a developer's real credentials. The fixtures write the file the detector reads,
// so a change to the file's shape fails here rather than at the first run on a laptop that
// has one.
func TestResolve_TheConfigFileDetectorsFollowHOME(t *testing.T) {
	t.Run("an opencode auth.json is detected", func(t *testing.T) {
		home := isolate(t)
		// The path and the key are the ones the detector reads:
		// ~/.local/share/opencode/auth.json, under "opencode-go". Both were
		// guessed wrong in a first version of this fixture, which is why the
		// detector's own constants are worth a test rather than a re-read.
		writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"), map[string]any{
			"opencode-go": map[string]any{
				"type": "api",
				"key":  "sk-from-a-file",
			},
		})

		if _, _, _, found := DetectOpenCodeConfig(); !found {
			t.Fatal("the opencode detector did not find the auth.json it was pointed at")
		}
		p, err := Resolve(Config{})
		if err != nil {
			t.Fatalf("Resolve with only an auth.json failed: %v", err)
		}
		if !strings.Contains(p.Name(), "opencode") {
			t.Errorf("Resolve built %q, want the opencode provider", p.Name())
		}
	})

	t.Run("an auth.json that is not there is not detected", func(t *testing.T) {
		isolate(t)
		if _, _, _, found := DetectOpenCodeConfig(); found {
			t.Error("the opencode detector found a config in an empty HOME")
		}
		if _, _, _, _, found := DetectJCodeConfig(); found {
			t.Error("the jcode detector found a config in an empty HOME")
		}
		if _, _, _, found := DetectPiConfig(); found {
			t.Error("the pi detector found a config in an empty HOME")
		}
	})

	t.Run("an env key is enough without any file", func(t *testing.T) {
		isolate(t)
		t.Setenv("ZEN_API_KEY", "sk-from-the-environment")
		apiKey, _, _, found := DetectOpenCodeConfig()
		if !found {
			t.Fatal("ZEN_API_KEY alone did not satisfy the opencode detector")
		}
		if apiKey != "sk-from-the-environment" {
			t.Errorf("the detector read the key %q", apiKey)
		}
		p, err := Resolve(Config{})
		if err != nil {
			t.Fatalf("Resolve with only ZEN_API_KEY failed: %v", err)
		}
		if !strings.Contains(p.Name(), "opencode") {
			t.Errorf("Resolve built %q, want the opencode provider", p.Name())
		}
	})

	t.Run("a jcode config.json is detected", func(t *testing.T) {
		home := isolate(t)
		writeJSON(t, filepath.Join(home, ".jcode", "config.json"), map[string]any{
			"model": "the-jcode-model",
			"providers": map[string]any{
				"my-gateway": map[string]any{
					"api_key":  "sk-from-jcode",
					"base_url": "http://127.0.0.1:9999/v1",
				},
			},
		})

		name, apiKey, model, baseURL, found := DetectJCodeConfig()
		if !found {
			t.Fatal("the jcode detector did not find the config.json it was pointed at")
		}
		if name != "my-gateway" {
			t.Errorf("the detector chose the provider %q, want my-gateway", name)
		}
		if apiKey != "sk-from-jcode" {
			t.Errorf("the detector read the key %q", apiKey)
		}
		if model != "the-jcode-model" {
			t.Errorf("the detector read the model %q, want the-jcode-model", model)
		}
		if baseURL != "http://127.0.0.1:9999/v1" {
			t.Errorf("the detector read the base URL %q", baseURL)
		}

		// And it is reachable through auto-detection, named after the provider
		// the config chose.
		p, err := Resolve(Config{})
		if err != nil {
			t.Fatalf("Resolve with only a jcode config failed: %v", err)
		}
		oc, ok := p.(*OpenAICompatible)
		if !ok {
			t.Fatalf("Resolve built a %T, want an *OpenAICompatible", p)
		}
		if oc.name != "jcode/my-gateway" {
			t.Errorf("the provider is named %q, want jcode/my-gateway", oc.name)
		}
		if oc.baseURL != "http://127.0.0.1:9999/v1" {
			t.Errorf("the provider's base URL is %q, want the configured one", oc.baseURL)
		}

		// An explicitly requested model overrides what the file said, which is
		// the whole reason resolveOpenCode takes the configured model as an
		// argument rather than reading the file's and stopping there.
		p2, err := Resolve(Config{Provider: "jcode", Model: "override"})
		if err != nil {
			t.Fatalf("Resolve(jcode) failed: %v", err)
		}
		if got := p2.(*OpenAICompatible).model; got != "override" {
			t.Errorf("with a configured model of %q the provider carries %q, want the configured one", "override", got)
		}
	})

	t.Run("a named provider whose config is missing says so", func(t *testing.T) {
		isolate(t)
		for _, tc := range []struct{ provider, want string }{
			{"opencode", "OPENCODE_API_KEY"},
			{"pi", "INFLECTION_API_KEY"},
			{"hermes", "NOUS_API_KEY"},
			{"jcode", "jcode"},
		} {
			t.Run(tc.provider, func(t *testing.T) {
				_, err := Resolve(Config{Provider: tc.provider})
				if err == nil {
					t.Fatalf("Resolve(%q) with nothing configured succeeded", tc.provider)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the error for %q does not mention %q: %q", tc.provider, tc.want, err)
				}
				if !strings.Contains(err.Error(), "not detected") &&
					!strings.Contains(err.Error(), "not found") {
					t.Errorf("the error for %q does not say the provider was not detected: %q", tc.provider, err)
				}
			})
		}
	})
}

// writeJSON writes a fixture config file, creating the directories, and fails the test
// rather than returning an error the caller would have to remember to check.
func writeJSON(t *testing.T, path string, body map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("could not create the fixture's directory: %v", err)
	}
	raw, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("could not encode the fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("could not write the fixture: %v", err)
	}
}
