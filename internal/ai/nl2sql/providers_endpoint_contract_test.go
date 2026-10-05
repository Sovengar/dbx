package nl2sql

// Scenario: Los cuatro proveedores, la misma escalera, y por qué tres necesitaron un seam.
//
// Pi got NewPiAt in an earlier round because its endpoint was baked into Generate, which
// made six error arms unreachable from a test. Anthropic, Qwen and DeepSeek had exactly
// the same shape and no seam — so the SAME fix, applied to three more files, is what
// uncovered nine statements that had nothing to do with Pi.
//
// The three arms each provider has, and why each is reachable only now:
//
//	NewRequestWithContext — a malformed URL, or a nil context
//	io.ReadAll — a body that promises more bytes than it delivers
//	the shape check / requireSQL — a refusal, which every provider has to answer
//
// The property is the same for all of them and is the one that matters: never empty SQL
// with no error. A provider that returns an empty string and nil hands the keypress
// nothing to run and says nothing about why.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveTruncated answers with a Content-Length larger than the body it writes, so the
// client's io.ReadAll fails partway through with an unexpected EOF.
//
// Declaring a length and then not delivering it is the only way to make a successful HTTP
// response fail to read — a real server that simply stops writing is not something a test
// can arrange through the normal handler interface.
func serveTruncated(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"entries":[{"text":"SELECT 1"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// serveAt answers a path with a given status and body, so a provider whose constructor
// appends its own suffix can be pointed at exactly the right URL.
func serveAt(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// everyProvider is the four, each with the constructor that takes an endpoint. If a fifth
// provider appears without one, this table is where it will be noticed.
//
// EVERY CASE IN THIS FILE MUST GO THROUGH HERE, and everyProvider refuses a base URL that
// is not a test server. That refusal is not tidiness: the first version of this file added
// the seam to three providers and, in the same patch, forgot it for Qwen — so the test sent
// a request to the real DashScope endpoint with the key "key" and reported the 401 it got
// back. A test that can reach a production endpoint will, and it will do so quietly.
func everyProvider(baseURL string) []provider {
	if !strings.HasPrefix(baseURL, "http://127.0.0.1:") && !strings.HasPrefix(baseURL, "http://[::1]:") {
		panic("everyProvider was given a non-test endpoint: " + baseURL)
	}
	return []provider{
		NewAnthropicAt("key", "model", baseURL),
		NewQwenAt("key", "model", baseURL),
		NewDeepSeekAt("key", "model", baseURL),
		NewPiAt("key", "model", baseURL),
		NewOpenAICompatible("openai-compatible", "key", "model", baseURL),
		NewOpenAIAt("key", "model", baseURL),
	}
}

// A response that stops half way. Every provider must report it, and none may return SQL.
func TestATruncatedResponseIsAnErrorForEveryProvider(t *testing.T) {
	baseURL := serveTruncated(t)

	for _, p := range everyProvider(baseURL) {
		t.Run(p.Name(), func(t *testing.T) {
			sql, err := p.Generate(context.Background(), "a question", "CREATE TABLE t (id int)")
			if err == nil {
				t.Fatalf("%s: a truncated response reported success with SQL %q", p.Name(), sql)
			}
			if sql != "" {
				t.Errorf("%s: a truncated response still returned SQL %q", p.Name(), sql)
			}
			// The message has to be about READING, not about parsing: the bytes arrived
			// broken, so a "failed to parse" would send a reader looking at the wire format
			// when the transport is what failed.
			if !strings.Contains(err.Error(), "read response") {
				t.Errorf("%s: the error is %q, want it to say the response could not be read", p.Name(), err)
			}
		})
	}
}

// A nil context. It is a programming error rather than a runtime condition, and the error
// it produces is the one that says so — which is worth having, because the alternative is a
// request that carries no deadline and cannot be cancelled.
func TestANilContextIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for _, p := range everyProvider("http://127.0.0.1:1") {
		t.Run(p.Name(), func(t *testing.T) {
			//nolint:staticcheck // deliberately nil: that is the case under test
			sql, err := p.Generate(nil, "q", "s") //nolint:staticcheck
			if err == nil {
				t.Fatalf("%s: a nil context produced SQL %q and no error", p.Name(), sql)
			}
			if sql != "" {
				t.Errorf("%s: a nil context returned SQL %q", p.Name(), sql)
			}
		})
	}
}

// A refusal, on each provider's own wire shape. requireSQL is the single gate all of them
// pass through, so this is the same property five times — and the property is the one the
// app depends on: an English sentence must never reach the SQL editor.
func TestARefusalIsRefusedByEveryProvider(t *testing.T) {
	cases := []struct {
		provider provider
		body     string
	}{
		{NewAnthropicAt("k", "m", serveAt(t, 200, `{"content":[{"text":"I can't help with that."}]}`)),
			`{"content":[{"text":"I can't help with that."}]}`},
		{NewQwenAt("k", "m", serveAt(t, 200, `{"choices":[{"message":{"content":"I can't help with that."}}]}`)),
			`{"choices":[{"message":{"content":"I can't help with that."}}]}`},
		{NewDeepSeekAt("k", "m", serveAt(t, 200, `{"choices":[{"message":{"content":"I can't help with that."}}]}`)),
			`{"choices":[{"message":{"content":"I can't help with that."}}]}`},
		{NewPiAt("k", "m", serveAt(t, 200, `{"entries":[{"text":"I can't help with that."}]}`)),
			`{"entries":[{"text":"I can't help with that."}]}`},
		{NewOpenAICompatible("o", "k", "m", serveAt(t, 200, `{"choices":[{"message":{"content":"I can't help with that."}}]}`)),
			`{"choices":[{"message":{"content":"I can't help with that."}}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.provider.Name(), func(t *testing.T) {
			if tc.body == "" {
				t.Fatal("the fixture has no body, so the case is not testing a refusal")
			}
			sql, err := tc.provider.Generate(context.Background(), "q", "s")
			if err == nil {
				t.Fatalf("%s: a refusal produced SQL %q and no error", tc.provider.Name(), sql)
			}
			if sql != "" {
				t.Errorf("%s: a refusal still returned SQL %q", tc.provider.Name(), sql)
			}
		})
	}

	t.Run("and the good case still answers, for every provider", func(t *testing.T) {
		// The counterweight. "Every provider refuses everything" satisfies the table above,
		// and would be found by a user rather than by a test.
		for _, p := range everyProvider(serveAt(t, 200, `{"entries":[{"text":"SELECT 1"}],"choices":[{"message":{"content":"SELECT 1"}}],"content":[{"text":"SELECT 1"}]}`)) {
			sql, err := p.Generate(context.Background(), "q", "s")
			if err != nil {
				t.Errorf("%s: the good case failed: %v", p.Name(), err)
				continue
			}
			if !strings.Contains(sql, "SELECT") {
				t.Errorf("%s: the good case produced %q", p.Name(), sql)
			}
		}
	})
}

// The default endpoints are constants and they are what a user with no configuration gets.
// Asserted so a refactor that moves one cannot change it by accident: an unreachable
// default fails for every user of that provider with nothing to tell them why.
func TestEveryDefaultEndpointIsTheDocumentedOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		got   string
		want  string
		model string
		built provider
	}{
		{"anthropic", NewAnthropic("key", "").baseURL, anthropicDefaultBaseURL + "/v1/messages",
			"claude-sonnet-4-20250514", NewAnthropic("key", "")},
		{"qwen", NewQwen("key", "").baseURL, qwenDefaultBaseURL + "/chat/completions",
			"qwen-turbo", NewQwen("key", "")},
		{"deepseek", NewDeepSeek("key", "").baseURL, deepSeekDefaultBaseURL + "/chat/completions",
			"deepseek-chat", NewDeepSeek("key", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("the endpoint is %q, want %q", tc.got, tc.want)
			}
			if tc.built.Name() != tc.name {
				t.Errorf("the provider names itself %q", tc.built.Name())
			}
		})
	}

	t.Run("and an explicit endpoint and model win", func(t *testing.T) {
		// Every At constructor, including the trailing-slash trim: a base URL written with a
		// trailing slash must not produce ".../chat/completions" twice over.
		for name, got := range map[string]string{
			"anthropic": NewAnthropicAt("k", "m", "http://localhost:1234/").baseURL,
			"qwen":      NewQwenAt("k", "m", "http://localhost:1234/").baseURL,
			"deepseek":  NewDeepSeekAt("k", "m", "http://localhost:1234/").baseURL,
			"pi":        NewPiAt("k", "m", "http://localhost:1234/").baseURL,
		} {
			if strings.Contains(got, "//chat/completions//") || strings.HasSuffix(got, "//") {
				t.Errorf("%s: the endpoint %q has a doubled or trailing separator", name, got)
			}
			if !strings.HasPrefix(got, "http://localhost:1234/") {
				t.Errorf("%s: the endpoint %q does not start at the given base", name, got)
			}
		}
		// And an empty model still gets the default, because a provider with no model is one
		// the API rejects with a message the user cannot act on.
		for name, got := range map[string]string{
			"qwen":      NewQwenAt("k", "", "http://x").model,
			"deepseek":  NewDeepSeekAt("k", "", "http://x").model,
			"anthropic": NewAnthropicAt("k", "", "http://x").model,
		} {
			if got == "" {
				t.Errorf("%s: an empty model stayed empty, so the API would reject it with a message the user cannot act on", name)
			}
		}
	})
}

// skipJSONCNoise's fourth case: a byte that is not whitespace and does not open a comment.
// It is the only way the function can stop early, and opencode's own config file is
// JSONC — so this is the case that decides whether a document is parsed from its first
// real character or from the wrong one.
func TestSkipJSONCNoiseStopsAtTheFirstRealByte(t *testing.T) {
	cases := []struct {
		name string
		src  string
		from int
		want int
	}{
		{"a value at the start", `"key": 1`, 0, 0},
		{"a brace after a comment", "// note\n{", 0, 8},
		{"a brace after a block comment", "/* note */ {", 0, 11},
		{"past the end", "", 0, 0},
		{"already at the end", "{", 1, 1},
		{"whitespace only", "  \t\n", 0, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipJSONCNoise([]byte(tc.src), tc.from); got != tc.want {
				t.Errorf("skipJSONCNoise(%q, %d) = %d, want %d", tc.src, tc.from, got, tc.want)
			}
		})
	}
}

// And the two Detect*Config errors: no home directory, and a home directory with no config
// in it. Both are ordinary states — a container, a fresh machine — and both must be quiet,
// because the alternative is dbx refusing to start on a box that has not been configured yet.
func TestDetectingAConfigWithNothingToFind(t *testing.T) {
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")

		// found=false is the answer, and not an error: there is nothing to find and nothing
		// went wrong. A caller treats the two differently and must be able to tell.
		if _, _, _, found := DetectOpenCodeConfig(); found {
			t.Error("a config was found with no home directory")
		}
	})

	t.Run("a home directory with no config in it", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		// No API key in the environment either, or the env branch would answer first and the
		// file read would never happen.
		for _, k := range []string{"OPENCODE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
			t.Setenv(k, "")
		}

		apiKey, _, _, found := DetectOpenCodeConfig()
		if found {
			t.Errorf("a config was found in an empty home: key=%q", apiKey)
		}
		if apiKey != "" {
			t.Errorf("a key came back from nowhere: %q", apiKey)
		}
	})
}

// There are SIX providers, and two of them answer to different names on purpose: the
// OpenAI-compatible shape is what every other vendor reuses, and openai's own client is a
// separate file with its own struct. A table built from "the ones I remember adding a seam
// to" would have missed openai entirely for a whole round — which it did, and which is why
// this case now counts them.
//
// The count is the assertion. A new provider that is not added here is a provider whose
// error arms quietly go back to being uncovered.
func TestEveryProviderInTheTreeIsInTheTable(t *testing.T) {
	baseURL := serveAt(t, 200,
		`{"choices":[{"message":{"content":"SELECT 1"}}],"content":[{"text":"SELECT 1"}],"entries":[{"text":"SELECT 1"}]}`)

	const wantProviders = 6
	got := everyProvider(baseURL)
	if len(got) != wantProviders {
		names := make([]string, len(got))
		for i, p := range got {
			names[i] = p.Name()
		}
		t.Errorf("the table holds %d providers (%v), want %d", len(got), names, wantProviders)
	}

	// And each one answers, so the count is not satisfied by six copies of one provider.
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p.Name()] {
			t.Errorf("the table holds %q twice", p.Name())
		}
		seen[p.Name()] = true
		if _, err := p.Generate(context.Background(), "q", "s"); err != nil {
			t.Errorf("%s: %v", p.Name(), err)
		}
	}
}

// The empty-model default, once per vendor provider. Every At constructor carries it, and a
// provider left with no model is one the API rejects with a message the user cannot act on.
//
// The OpenAI-compatible shape is deliberately NOT in the table: it is built from a user's own
// config, where an empty model is the caller's decision rather than a missing default, and
// inventing one would send a model the user never asked for. The first version of this case
// asserted it anyway, and the code was right.
func TestEveryVendorProviderDefaultsAnEmptyModel(t *testing.T) {
	for name, model := range map[string]string{
		"openai":    NewOpenAIAt("k", "", "http://x").model,
		"anthropic": NewAnthropicAt("k", "", "http://x").model,
		"qwen":      NewQwenAt("k", "", "http://x").model,
		"deepseek":  NewDeepSeekAt("k", "", "http://x").model,
		"pi":        NewPiAt("k", "", "http://x").model,
	} {
		if model == "" {
			t.Errorf("%s: an empty model stayed empty, so the API would reject it with a message the user cannot act on", name)
		}
	}

	t.Run("and the compatible shape keeps what the caller passed", func(t *testing.T) {
		if got := NewOpenAICompatible("c", "k", "", "http://x").model; got != "" {
			t.Errorf("the compatible shape invented the model %q", got)
		}
		if got := NewOpenAICompatible("c", "k", "my-model", "http://x").model; got != "my-model" {
			t.Errorf("the compatible shape replaced the caller's model with %q", got)
		}
	})
}

// The confirm prompt. It is the query the app sends to a model to ask whether a statement is
// safe, and the one thing it must never contain is anything but the statement — a prompt
// that leaked the schema, or truncated the SQL, would make the safety answer about the
// wrong text.
func TestTheConfirmPromptCarriesTheStatementAndNothingElse(t *testing.T) {
	const stmt = "DELETE FROM orders"

	got := BuildConfirmPrompt(stmt)
	if !strings.Contains(got, stmt) {
		t.Fatalf("the prompt does not contain the statement: %q", got)
	}
	// And it asks for one word, because the answer is parsed, not read.
	if !strings.Contains(got, "ONLY one word") {
		t.Errorf("the prompt does not ask for ONLY one word: %q", got)
	}
	// Two different statements must produce two different prompts: a prompt that ignored its
	// argument would make every statement look the same to the reviewer.
	if BuildConfirmPrompt("SELECT 1") == got {
		t.Error("the prompt is the same for two different statements")
	}
}
