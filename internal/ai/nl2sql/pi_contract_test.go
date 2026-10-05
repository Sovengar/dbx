package nl2sql

// Scenario: El segundo proveedor, que es una COPIA del primero.
//
// PiProvider.Generate is OpenAICompatible.Generate with three differences: a different URL
// path, a different response shape (`entries[].text` instead of `choices[].message.content`),
// and a different provider name in the error messages. Everything else — the six error
// exits, the fence handling, the requireSQL gate — is the same code, written twice in the
// same file.
//
// That is why this file exists rather than a line in the other provider's table. A ladder
// with one copy tested and one copy untested is the shape that rots: the untested copy can
// lose a guard, or gain one, or drift on the status code it checks, and the tested copy says
// nothing. The table below runs the SAME cases through both and asserts the SAME property
// on each, so a divergence is a test failure rather than a surprise at runtime.
//
// The two shapes differ in one more way worth pinning: Pi's refusal arrives as prose like
// every other provider's, and requireSQL has to catch it here too.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// provider is the one method both providers implement, which is what lets one table drive
// both. Anything they do NOT share is tested on each separately.
type provider interface {
	Name() string
	Generate(ctx context.Context, prompt, schema string) (string, error)
}

// piBodies are the responses on Pi's wire format. The good and bad cases mirror
// openAIResponses; the shapes differ only in the envelope.
var piBodies = []struct {
	name      string
	status    int
	body      string
	wantSQL   bool
	wantError string
}{
	{"the good case", http.StatusOK, `{"entries":[{"text":"SELECT 1"}]}`, true, ""},

	// The ways a model says nothing, on Pi's shape. A provider whose refusal slips through
	// puts an English sentence into the SQL editor.
	{"a refusal", http.StatusOK, `{"entries":[{"text":"I can't help with that."}]}`, false, "no SQL"},
	{"an empty text", http.StatusOK, `{"entries":[{"text":""}]}`, false, "no SQL"},
	{"a safety filter that empties the text", http.StatusOK, `{"entries":[{"text":"   "}]}`, false, "no SQL"},

	// The bug the fence fix exists for: the fence ate the DELETE and the app ran FROM orders.
	// Asserted as "no SQL" would be wrong - it is a valid statement - so it is asserted as the
	// STATEMENT instead.
	//
	// Written with an interpreted string for two reasons, both learned: a raw one ends at
	// the first backtick of the fence it contains, and the newline inside a JSON string
	// literal has to be the two-character ESCAPE - a real newline there is a parse error
	// the provider reports as "failed to parse response", which reads like a wire-format
	// problem and is not one.
	{"a DELETE the fence does not eat", http.StatusOK,
		"{\"entries\":[{\"text\":\"```DELETE\\nFROM orders\\n```\"}]}", true, ""},

	{"an API error object", http.StatusOK, `{"error":{"message":"model overloaded"}}`, false, "model overloaded"},
	{"no entries at all", http.StatusOK, `{"entries":[]}`, false, "empty response"},
	{"a body that is not JSON", http.StatusOK, "<html>gateway timeout</html>", false, "failed to parse"},
	{"an HTTP error", http.StatusInternalServerError, `{"error":"upstream is down"}`, false, "500"},
	{"a rate limit", http.StatusTooManyRequests, "slow down", false, "429"},
}

// TestEveryProviderRefusesAMalformedResponse is the table, run through both providers.
func TestEveryProviderRefusesAMalformedResponse(t *testing.T) {
	t.Run("the OpenAI-compatible wire format", func(t *testing.T) {
		for _, tc := range openAIResponses {
			t.Run(tc.name, func(t *testing.T) {
				baseURL, _ := serveWith(t, tc.status, tc.body)
				p := provider(NewOpenAICompatible("test-provider", "key", "model", baseURL))
				assertProviderAnswer(t, p, tc.wantSQL, tc.wantError)
			})
		}
	})

	t.Run("the Pi wire format", func(t *testing.T) {
		for _, tc := range piBodies {
			t.Run(tc.name, func(t *testing.T) {
				baseURL, _ := serveWith(t, tc.status, tc.body)
				p := provider(NewPiAt("key", "model", baseURL))
				assertProviderAnswer(t, p, tc.wantSQL, tc.wantError)
			})
		}
	})
}

// assertProviderAnswer is the shared property: either the SQL is non-empty, or there is an
// error naming what happened. Never one without the other.
func assertProviderAnswer(t *testing.T, p provider, wantSQL bool, wantError string) {
	t.Helper()
	sql, err := p.Generate(context.Background(), "a question", "CREATE TABLE t (id int)")

	if sql == "" && err == nil {
		t.Fatalf("%s returned empty SQL and no error, so the keypress would do nothing at all", p.Name())
	}
	if wantSQL {
		if err != nil {
			t.Fatalf("%s: the good case failed: %v", p.Name(), err)
		}
		if sql == "" {
			t.Fatalf("%s: the good case produced no SQL", p.Name())
		}
		return
	}
	if err == nil {
		t.Fatalf("%s: the bad case returned SQL %q and no error", p.Name(), sql)
	}
	if sql != "" {
		t.Errorf("%s: the refusal still returned SQL %q", p.Name(), sql)
	}
	if wantError != "" && !strings.Contains(err.Error(), wantError) {
		t.Errorf("%s: the error is %q, want it to mention %q", p.Name(), err, wantError)
	}
}

// The DELETEs and the fences, asserted on what actually comes out. The property is that the
// statement a model wrote is the statement the app runs — which is invisible to every other
// test in this package, because a mangled statement is still a statement.
func TestAFenceNeverCostsTheStatementItsFirstWord(t *testing.T) {
	for _, stmt := range []string{
		"SELECT 1",
		"DELETE FROM orders",
		"UPDATE orders SET total = 0",
		"INSERT INTO orders (id) VALUES (1)",
		"TRUNCATE orders",
	} {
		for _, fence := range []struct {
			name string
			wrap func(string) string
		}{
			{"tagged", func(s string) string { return "```sql\n" + s + "\n```" }},
			{"untagged", func(s string) string { return "```\n" + s + "\n```" }},
			{"bare", func(s string) string { return "```" + s + "\n```" }},
			{"prose before it", func(s string) string { return "Here you go:\n```sql\n" + s + "\n```" }},
			{"no fence", func(s string) string { return s }},
		} {
			t.Run(fmt.Sprintf("%s in a %s fence", stmt, fence.name), func(t *testing.T) {
				for _, p := range []provider{
					NewOpenAICompatible("test", "key", "model", "http://127.0.0.1:1"),
					NewPiAt("key", "model", "http://127.0.0.1:1"),
				} {
					sql, err := requireSQL(p.Name(), fence.wrap(stmt))
					if err != nil {
						t.Fatalf("%s: %v", p.Name(), err)
					}
					first := strings.Fields(sql)[0]
					if first != strings.Fields(stmt)[0] {
						t.Errorf("%s: the statement came back as %q, so its first word is %q and not %q",
							p.Name(), sql, first, strings.Fields(stmt)[0])
					}
					if !looksLikeSQL(sql) {
						t.Errorf("%s: %q is not recognisable as a statement", p.Name(), sql)
					}
				}
			})
		}
	}
}

// A provider pointed at an endpoint that answers nothing. This is the arm a user hits when
// they typed a base_url wrong — the most common misconfiguration — and the error has to say
// the REQUEST failed rather than reporting an empty answer.
func TestBothProvidersReportAnUnreachableEndpoint(t *testing.T) {
	for _, p := range []provider{
		NewOpenAICompatible("opencode", "key", "model", "http://127.0.0.1:1"),
		NewPiAt("key", "model", "http://127.0.0.1:1"),
	} {
		t.Run(p.Name(), func(t *testing.T) {
			sql, err := p.Generate(context.Background(), "q", "s")
			if err == nil {
				t.Fatalf("%s: an unreachable endpoint reported success", p.Name())
			}
			if sql != "" {
				t.Errorf("%s: an unreachable endpoint returned SQL %q", p.Name(), sql)
			}
			if !strings.Contains(err.Error(), "request failed") {
				t.Errorf("%s: the error is %q, want it to say the request failed", p.Name(), err)
			}
		})
	}
}

// NewPi's default endpoint is a CONSTANT, and it is what a user with no configuration gets.
// Asserted so a refactor that moves it cannot change it by accident — an unreachable default
// would fail for every user of that provider with no way to tell why.
func TestTheDefaultEndpointIsTheDocumentedOne(t *testing.T) {
	p := NewPi("key", "")
	if p.baseURL != piDefaultBaseURL {
		t.Errorf("NewPi's endpoint is %q, want %q", p.baseURL, piDefaultBaseURL)
	}
	if p.model != "inflection_3_pi" {
		t.Errorf("NewPi's default model is %q, want inflection_3_pi", p.model)
	}
	if p.Name() != "pi" {
		t.Errorf("NewPi's name is %q, want pi", p.Name())
	}

	t.Run("an explicit endpoint and model win", func(t *testing.T) {
		p := NewPiAt("key", "my-model", "http://localhost:1234")
		if p.baseURL != "http://localhost:1234" || p.model != "my-model" {
			t.Errorf("NewPiAt stored %q/%q, want the given endpoint and model", p.baseURL, p.model)
		}
		// An empty model still gets the default, because a provider with no model is one the
		// API rejects with a message the user cannot act on.
		p = NewPiAt("key", "", "http://localhost:1234")
		if p.model != "inflection_3_pi" {
			t.Errorf("an empty model gave %q, want the default", p.model)
		}
	})
}
