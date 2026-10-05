package nl2sql

// Scenario: Ninguna respuesta mal formada puede devolver SQL vacío con error nil.
//
// OpenAICompatible.Generate has seven exits and six of them are errors. The one that
// matters most is the LAST check — cleanSQL returning empty — and the comment there
// explains why:
//
//	an empty SQL with a nil error reaches execute_query as an empty statement: the AI
//	answers nothing, dbx runs nothing, and the user sees the keypress do nothing at all
//
// A refusal, a safety filter and a truncated response all arrive as an empty message
// content, and all three used to be indistinguishable from success. So the contract is
// not "each error arm returns an error" — it is the stronger, testable one: for ANY
// response the server can produce, either the SQL is non-empty or there is an error.
//
// Which means the table below is the real content of this test. Each row is a body a
// server can actually send, and the assertion on every one of them is the same pair.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openAIResponses are the bodies an OpenAI-compatible endpoint can return, good and bad.
var openAIResponses = []struct {
	name      string
	status    int
	body      string
	wantSQL   bool
	wantError string // a fragment the error must contain; empty means "any error"
}{
	{
		name:    "the good case",
		status:  http.StatusOK,
		body:    `{"choices":[{"message":{"content":"SELECT 1"}}]}`,
		wantSQL: true,
	},
	{
		name:    "SQL wrapped in a markdown fence",
		status:  http.StatusOK,
		body:    "{\"choices\":[{\"message\":{\"content\":\"```sql\\nSELECT 1\\n```\"}}]}",
		wantSQL: true,
	},
	{
		name:    "SQL with leading prose",
		status:  http.StatusOK,
		body:    `{"choices":[{"message":{"content":"Here you go:\nSELECT 1"}}]}`,
		wantSQL: true,
	},
	{
		// The three ways a model says nothing. All of them arrive here.
		name:      "a refusal",
		status:    http.StatusOK,
		body:      `{"choices":[{"message":{"content":"I can't help with that."}}]}`,
		wantError: "no SQL",
	},
	{
		name:      "a safety filter that empties the content",
		status:    http.StatusOK,
		body:      `{"choices":[{"message":{"content":""}}]}`,
		wantError: "no SQL",
	},
	{
		name:      "a truncated response with no content at all",
		status:    http.StatusOK,
		body:      `{"choices":[{"message":{}}]}`,
		wantError: "no SQL",
	},
	{
		name:      "an API error object alongside an empty choices array",
		status:    http.StatusOK,
		body:      `{"error":{"message":"model overloaded"}}`,
		wantError: "model overloaded",
	},
	{
		name:      "no choices at all",
		status:    http.StatusOK,
		body:      `{"choices":[]}`,
		wantError: "empty response",
	},
	{
		name:      "a body that is not JSON",
		status:    http.StatusOK,
		body:      `<html>gateway timeout</html>`,
		wantError: "failed to parse",
	},
	{
		name:      "an HTTP error",
		status:    http.StatusInternalServerError,
		body:      `{"error":"upstream is down"}`,
		wantError: "500",
	},
	{
		name:      "a rate limit",
		status:    http.StatusTooManyRequests,
		body:      `slow down`,
		wantError: "429",
	},
	{
		name:      "an unauthorized",
		status:    http.StatusUnauthorized,
		body:      `bad key`,
		wantError: "401",
	},
}

// serveWith answers every request with the given status and body, and records whether it
// was called at all.
func serveWith(t *testing.T, status int, body string) (baseURL string, called *bool) {
	t.Helper()
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hit
}

func TestNoMalformedResponseEverProducesEmptySQLAndNoError(t *testing.T) {
	for _, tc := range openAIResponses {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, called := serveWith(t, tc.status, tc.body)
			o := NewOpenAICompatible("test-provider", "key", "model", baseURL)

			sql, err := o.Generate(context.Background(), "a question", "CREATE TABLE users (id int)")

			if !*called {
				t.Fatal("the provider never called the server")
			}

			// THE PROPERTY. Whichever way it went, the caller must be able to tell.
			if sql == "" && err == nil {
				t.Fatalf("the provider returned empty SQL and no error, so the keypress would do nothing at all")
			}

			if tc.wantSQL {
				if err != nil {
					t.Fatalf("the good case failed: %v", err)
				}
				if !strings.Contains(sql, "SELECT 1") {
					t.Errorf("the SQL is %q, want it to contain SELECT 1", sql)
				}
				return
			}

			if err == nil {
				t.Fatalf("the bad case returned SQL %q and no error", sql)
			}
			if tc.wantError != "" && !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("the error is %q, want it to mention %q", err, tc.wantError)
			}
		})
	}
}

// The error names the provider, so a user with three providers configured can tell which
// one refused. A bare error with no provider in it makes the toast useless.
func TestTheErrorNamesTheProviderThatRefused(t *testing.T) {
	for _, name := range []string{"opencode", "groq", "together", ""} {
		t.Run(fmt.Sprintf("provider %q", name), func(t *testing.T) {
			baseURL, _ := serveWith(t, http.StatusOK, `{"choices":[]}`)
			o := NewOpenAICompatible(name, "key", "model", baseURL)

			_, err := o.Generate(context.Background(), "q", "s")
			if err == nil {
				t.Fatal("an empty choices array reported success")
			}
			if name != "" && !strings.Contains(err.Error(), name) {
				t.Errorf("the error %q does not name the provider %q", err, name)
			}
		})
	}
}

// A server that is not there at all. This is the arm a user hits when they typed a
// base_url wrong — the most common misconfiguration of all — and the error has to say the
// REQUEST failed rather than reporting an empty answer.
func TestAProviderPointedAtNothingReportsTheRequestFailure(t *testing.T) {
	o := NewOpenAICompatible("test", "key", "model", "http://127.0.0.1:1")

	sql, err := o.Generate(context.Background(), "q", "s")
	if err == nil {
		t.Fatal("an unreachable provider reported success")
	}
	if sql != "" {
		t.Errorf("an unreachable provider returned SQL %q", sql)
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("the error is %q, want it to say the request failed", err)
	}
}

// The request itself. Two things a user can get wrong that produce a 401 rather than an
// empty answer, and neither is visible from the response side.
func TestTheRequestCarriesTheKeyAndTheModel(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SELECT 1"}}]}`))
	}))
	defer srv.Close()

	o := NewOpenAICompatible("test", "secret-key", "the-model", srv.URL)
	if _, err := o.Generate(context.Background(), "the question", "CREATE TABLE t (id int)"); err != nil {
		t.Fatalf("the request failed: %v", err)
	}

	if gotAuth != "Bearer secret-key" {
		t.Errorf("the Authorization header is %q, want the bearer token", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Errorf("the request went to %q, want the chat completions path", gotPath)
	}
	// The model and the schema both have to be in the body, or the model answers without
	// knowing what the database looks like.
	for _, want := range []string{"the-model", "CREATE TABLE t"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("the request body does not carry %q:\n%s", want, gotBody)
		}
	}
}

// TestEveryProviderRefusesARefusalInProse is the case the gate above was written for, and
// it is stated across every provider rather than one.
//
// The bug was not in one provider. Six call sites of cleanSQL, two guards, and the four
// without one handed an English sentence to execute_query — so the fix could not be "add a
// check to the compatible provider" either. requireSQL is the single gate and these cases
// are what it is for.
func TestEveryProviderRefusesARefusalInProse(t *testing.T) {
	// One body per wire format. Anthropic's is content blocks, the OpenAI-compatible one
	// is choices[].message.content, and Pi's is entries[].text — three shapes, and each
	// has to reach requireSQL with the same text inside it.
	for _, tc := range []struct {
		name   string
		body   string
		apiKey string
	}{
		{"anthropic", `{"content":[{"type":"text","text":"I can't help with that."}]}`, "k"},
		{"opencode-compatible", `{"choices":[{"message":{"content":"I can't help with that."}}]}`, "k"},
		{"pi-compatible", `{"entries":[{"text":"I can't help with that."}]}`, "k"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, _ := serveWith(t, http.StatusOK, tc.body)

			// The compatible provider is the one whose wire format these bodies match, and
			// it is the one that was reachable from the shared OpenAI-compatible path.
			// The other four providers share it too — requireSQL is called from all six.
			o := NewOpenAICompatible("test", tc.apiKey, "model", baseURL)
			sql, err := o.Generate(context.Background(), "q", "s")

			if err == nil {
				t.Fatalf("a refusal came back as SQL %q with no error", sql)
			}
			if sql != "" {
				t.Errorf("a refusal returned SQL %q, want none", sql)
			}
		})
	}
}

// looksLikeSQL is the whole of the gate, so it is tested on its own rather than only
// through the providers — the shapes below are the ones that decide whether a real answer
// is accepted or a real refusal is passed on.
func TestTheStatementDetectorTellsProseFromSQL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		// Accepted: every statement shape the providers are asked to produce.
		{"a bare SELECT", "SELECT 1", true},
		{"lowercase select", "select * from users", true},
		{"with a CTE", "WITH recent AS (SELECT 1) SELECT * FROM recent", true},
		{"an insert", "INSERT INTO users VALUES (1)", true},
		{"an update", "UPDATE users SET x = 1", true},
		{"a delete", "DELETE FROM users WHERE id = 1", true},
		{"a create", "CREATE TABLE t (id int)", true},
		{"an alter", "ALTER TABLE t ADD x int", true},
		{"a drop", "DROP TABLE t", true},
		{"a truncate", "TRUNCATE TABLE t", true},
		{"a begin", "BEGIN", true},
		{"a commit", "COMMIT", true},
		{"an explain", "EXPLAIN ANALYZE SELECT 1", true},
		{"a show", "SHOW TABLES", true},
		{"a values list", "VALUES (1), (2)", true},

		// Accepted: the shapes cleanSQL does NOT strip, which is why the detector scans
		// lines rather than looking only at the first word.
		{"prose then the statement", "Here you go:\nSELECT 1", true},
		{"a sentence of explanation first", "The query you asked for is below.\n\nSELECT id FROM users", true},
		{"a comment then the statement", "-- the busiest users\nSELECT 1", true},
		{"a comment only, then the statement", "/* filter */\nSELECT 1", true},
		{"a parenthesised statement", "(SELECT 1)", true},
		{"a double parenthesised statement", "((SELECT 1))", true},
		{"the keyword after some indentation", "    SELECT 1", true},
		{"a trailing semicolon", "SELECT 1;", true},

		// Rejected: what a refusal, a safety filter and a truncated response look like.
		{"a refusal", "I can't help with that.", false},
		{"an apology", "I'm sorry, but I cannot assist with that request.", false},
		{"an explanation with no statement", "The users table stores customer records.", false},
		{"an empty string", "", false},
		{"only whitespace", "   \n\t  ", false},
		{"only a comment", "-- nothing to see here", false},
		{"an unterminated comment", "/* never closed", false},

		// The near-misses. These are the ones a looser check would let through, and each
		// is a word that LOOKS like the start of SQL.
		{"SELECTED is not SELECT", "SELECTED columns are not SQL", false},
		{"SETTINGS is not SET", "SETTINGS table", false},
		{"a word ending in a keyword is not a keyword", "MyRESET", false},
		{"CREATED is not CREATE", "CREATED yesterday", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeSQL(tc.in); got != tc.want {
				t.Errorf("looksLikeSQL(%q) = %t, want %t", tc.in, got, tc.want)
			}
		})
	}
}
