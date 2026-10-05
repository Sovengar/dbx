package nl2sql

// The contracts of the OpenAI-compatible provider plumbing, asserted against a real
// HTTP server rather than a stubbed client.
//
// Everything here is about one thing: dbx reads OTHER people's configuration and turns
// it into an HTTP request. That means three separate surfaces, each with its own way of
// quietly going wrong:
//
//	C1  Generate speaks the documented wire format — POST to the right path, with the
//	    right headers and body — and every failure mode is reported rather than turned
//	    into empty SQL
//	C2  the API-key header is present exactly when there is a key, and absent when
//	    there is not, because a header of "Bearer " is not the same as no header
//	C3  the returned SQL is cleaned, and a refusal is an error
//	C4  stripJSONC turns JSONC into JSON without touching what is inside a string,
//	    which is where every comment-stripper goes wrong
//	C5  the opencode config reader understands both schema versions and prefers what
//	    it documents
//	C6  the env detectors: which variable wins, and what happens with none
//	C7  the jcode reader picks the SAME provider every time, because the chosen
//	    provider's base URL is where the prompt goes
//
// The detectors read the real filesystem through the home directory, so HOME is pointed
// at a temp dir per test. Nothing here touches the network beyond the loopback server the
// test itself starts.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// capture records what a Generate call actually sent, and answers with whatever the
// test told it to.
type capture struct {
	method      string
	path        string
	header      http.Header
	body        []byte
	contentType string
}

// server answers with status and body, and records the request into got.
func server(t *testing.T, status int, body string) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		got.method = r.Method
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		got.body = payload
		got.contentType = r.Header.Get("Content-Type")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// okBody is an OpenAI-shaped success carrying the given SQL.
func okBody(sql string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + jsonQuote(sql) + `}}]}`
}

// piBody is a Pi-shaped success carrying the given text.
func piBody(text string) string {
	return `{"entries":[{"text":` + jsonQuote(text) + `}]}`
}

func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// C1 + C2 + C3: the wire format
// ---------------------------------------------------------------------------

// Scenario: Generate HABLA el formato que dice, y falla en vez de devolver SQL vacio.
//
// The dangerous failure for this function is not an error — it is returning "" with a
// nil error, because the caller then runs an empty query and the user sees nothing
// happen. Every branch is checked for an error, and the payload is read from the
// request the server actually received rather than from a reconstruction.
func TestGenerateSpeaksTheDocumentedWireFormat(t *testing.T) {
	t.Run("a success posts the system prompt, the question, and returns cleaned SQL", func(t *testing.T) {
		srv, got := server(t, 200, okBody("  SELECT * FROM users;\n"))

		p := NewOpenAICompatible("acme", "secret-key", "some-model", srv.URL)
		sql, err := p.Generate(t.Context(), "how many users?", "table users(id int)")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		// The SQL comes back trimmed and code-fenced-stripped, because the
		// model wraps it in prose and a ```sql fence and dbx executes it.
		if sql != "SELECT * FROM users;" {
			t.Errorf("Generate returned %q, want the cleaned statement", sql)
		}

		if got.method != "POST" {
			t.Errorf("the method is %q, want POST", got.method)
		}
		if got.path != "/chat/completions" {
			t.Errorf("the path is %q, want /chat/completions", got.path)
		}
		if got.contentType != "application/json" {
			t.Errorf("the content type is %q, want application/json", got.contentType)
		}

		// The body is the documented OpenAI shape: a model, and a system
		// message carrying the schema plus a user message carrying the
		// question, in THAT order.
		var sent struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.Unmarshal(got.body, &sent); err != nil {
			t.Fatalf("the request body is not the documented shape: %v\n%s", err, got.body)
		}
		if sent.Model != "some-model" {
			t.Errorf("the model sent is %q, want some-model", sent.Model)
		}
		if len(sent.Messages) != 2 {
			t.Fatalf("the body carries %d messages, want 2 (system then user)", len(sent.Messages))
		}
		if sent.Messages[0].Role != "system" {
			t.Errorf("the first message is %q, want the system prompt", sent.Messages[0].Role)
		}
		if !strings.Contains(sent.Messages[0].Content, "table users(id int)") {
			t.Errorf("the system prompt does not carry the schema:\n%s", sent.Messages[0].Content)
		}
		if sent.Messages[1].Role != "user" || sent.Messages[1].Content != "how many users?" {
			t.Errorf("the second message is %+v, want the user question verbatim", sent.Messages[1])
		}
		if sent.MaxTokens == 0 {
			t.Error("no max_tokens was sent")
		}
	})

	t.Run("the API key header is present with a key and ABSENT without one", func(t *testing.T) {
		// Not "Bearer " with an empty suffix. An absent header and an empty
		// one are different to a gateway, and some of them answer a blank
		// bearer with a 401 where they would have allowed an anonymous local
		// endpoint through.
		srv, got := server(t, 200, okBody("SELECT 1"))
		_, _ = NewOpenAICompatible("acme", "k", "m", srv.URL).Generate(t.Context(), "q", "")
		if got.header.Get("Authorization") != "Bearer k" {
			t.Errorf("the authorization header is %q, want %q", got.header.Get("Authorization"), "Bearer k")
		}

		srv2, got2 := server(t, 200, okBody("SELECT 1"))
		_, _ = NewOpenAICompatible("local", "", "m", srv2.URL).Generate(t.Context(), "q", "")
		if v := got2.header.Values("Authorization"); len(v) != 0 {
			t.Errorf("with no key the request still carries Authorization: %q", v)
		}
	})

	t.Run("a TRAILING SLASH on the base URL does not double the path separator", func(t *testing.T) {
		// The other provider in this same file joins with a bare "+", which is
		// the drift this test exists to catch: the two spellings of "one
		// separator" in one file is how a base URL copied with a trailing
		// slash ends up posting to "//inference".
		srv, got := server(t, 200, okBody("SELECT 1"))
		p := NewOpenAICompatible("acme", "k", "m", srv.URL+"/")
		if _, err := p.Generate(t.Context(), "q", ""); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if got.path != "/chat/completions" {
			t.Errorf("the path is %q, want /chat/completions — no doubled separator", got.path)
		}
	})

	t.Run("only the opencode-go provider gets the session header", func(t *testing.T) {
		// The header is a quirk of that one gateway. Sending it to everyone
		// leaks a value nothing else needs; not sending it to the one that
		// needs it gets a 400.
		for _, tc := range []struct {
			name string
			want bool
		}{
			{"opencode-go", true},
			{"acme", false},
			{"", false},
		} {
			srv, got := server(t, 200, okBody("SELECT 1"))
			if _, err := NewOpenAICompatible(tc.name, "k", "m", srv.URL).Generate(t.Context(), "q", ""); err != nil {
				t.Fatal(err)
			}
			has := got.header.Get("x-opencode-session") != ""
			if has != tc.want {
				t.Errorf("the provider %q sent x-opencode-session=%v, want %v", tc.name, has, tc.want)
			}
			if has && !strings.HasPrefix(got.header.Get("x-opencode-session"), "dbx-") {
				t.Errorf("the session header is %q, want it to start with dbx-", got.header.Get("x-opencode-session"))
			}
		}
	})

	t.Run("every failure is an ERROR, never an empty string with no error", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			status int
			body   string
			want   string
		}{
			{"an HTTP error carries the status and the body", 429, `{"error":"slow down"}`, "429"},
			{"a server error", 500, "boom", "500"},
			{"an unauthorized answer", 401, "no", "401"},
			{"a body that is not JSON", 200, "<html>gateway timeout</html>", "parse"},
			{"an error object inside a 200", 200, `{"error":{"message":"model not found"}}`, "model not found"},
			{"a success with no choices", 200, `{"choices":[]}`, "empty"},
			{"a success with a null choices", 200, `{"choices":null}`, "empty"},
			{"a choice with no message content", 200, `{"choices":[{"message":{"content":"  "}}]}`, ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv, _ := server(t, tc.status, tc.body)
				sql, err := NewOpenAICompatible("acme", "k", "m", srv.URL).Generate(t.Context(), "q", "")
				if err == nil {
					t.Fatalf("Generate returned %q and NO error, want a failure", sql)
				}
				if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the error is %q, want it to mention %q", err.Error(), tc.want)
				}
				if sql != "" {
					t.Errorf("Generate returned %q alongside an error, want it to return nothing", sql)
				}
			})
		}
	})

	t.Run("the SQL is cleaned: fenced, prefixed and whitespace all go", func(t *testing.T) {
		// The model is asked for SQL and answers with a markdown fence and a
		// sentence. dbx executes the answer, so anything left over is a syntax
		// error the user has to read.
		for _, tc := range []struct{ name, in, want string }{
			{"a fenced block", "```sql\nSELECT 1;\n```", "SELECT 1;"},
			{"a bare fence", "```\nSELECT 1;\n```", "SELECT 1;"},
			{"surrounding whitespace", "\n\n  SELECT 1;  \n\n", "SELECT 1;"},
			{"a fence with a language tag", "```SQL\nSELECT 1;\n```", "SELECT 1;"},
			{"no fence at all", "SELECT 1;", "SELECT 1;"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv, _ := server(t, 200, okBody(tc.in))
				sql, err := NewOpenAICompatible("acme", "k", "m", srv.URL).Generate(t.Context(), "q", "")
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if sql != tc.want {
					t.Errorf("cleaning %q gave %q, want %q", tc.in, sql, tc.want)
				}
			})
		}
	})

	t.Run("a cancelled context does not hang, it errors", func(t *testing.T) {
		// The AI call is the one thing in dbx that can take a minute. If the
		// user pressed ctrl+c in the meantime, waiting for the socket is a
		// frozen interface, not a slow one.
		srv, _ := server(t, 200, okBody("SELECT 1"))
		ctx, cancel := contextWithCancel()
		cancel()
		sql, err := NewOpenAICompatible("acme", "k", "m", srv.URL).Generate(ctx, "q", "")
		if err == nil {
			t.Errorf("Generate on a cancelled context returned %q and no error", sql)
		}
	})

	t.Run("an unusable base URL is reported, not attempted", func(t *testing.T) {
		for _, base := range []string{"://not a url", "http://[::1]:namedport", "ht tp://x"} {
			sql, err := NewOpenAICompatible("acme", "k", "m", base).Generate(t.Context(), "q", "")
			if err == nil {
				t.Errorf("the base URL %q produced %q and no error", base, sql)
			}
		}
	})

	t.Run("an unreachable host is reported", func(t *testing.T) {
		// A closed server, not a made-up host, so there is no DNS and no wait:
		// the connection refusal has to come back as an error.
		srv, _ := server(t, 200, okBody("SELECT 1"))
		dead := srv.URL
		srv.Close()
		sql, err := NewOpenAICompatible("acme", "k", "m", dead).Generate(t.Context(), "q", "")
		if err == nil {
			t.Errorf("Generate against a dead server returned %q and no error", sql)
		}
	})
}

// Scenario: El proveedor Pi habla SU formato, no el de OpenAI.
//
// Pi is a different API with a different request shape. Reusing the OpenAI path for it
// is the kind of copy that looks right and 400s, so the body is checked field by field.
func TestPiSpeaksItsOwnWireFormat(t *testing.T) {
	t.Run("a success posts a config, two context entries, and returns cleaned text", func(t *testing.T) {
		srv, got := server(t, 200, piBody("```sql\nSELECT 2;\n```"))

		p := newPiAt(srv.URL, "pi-key", "")
		if p.Name() != "pi" {
			t.Errorf("Name is %q, want pi", p.Name())
		}
		// An empty model means the documented default, and the DEFAULT has to
		// be the one sent as the config field — an empty config field is a 400.
		sql, err := p.Generate(t.Context(), "count them", "table users(id int)")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if sql != "SELECT 2;" {
			t.Errorf("Generate returned %q, want the cleaned statement", sql)
		}

		if got.path != "/inference" {
			t.Errorf("the path is %q, want /inference", got.path)
		}
		if got.header.Get("Authorization") != "Bearer pi-key" {
			t.Errorf("the authorization header is %q", got.header.Get("Authorization"))
		}
		// The key is sent UNCONDITIONALLY for Pi, unlike the OpenAI path. That
		// is what the code does; pinned so a change to it is a decision.
		if got.contentType != "application/json" {
			t.Errorf("the content type is %q", got.contentType)
		}

		var sent struct {
			Model   string `json:"model"`
			Config  string `json:"config"`
			Context []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"context"`
		}
		if err := json.Unmarshal(got.body, &sent); err != nil {
			t.Fatalf("the request body is not the documented shape: %v\n%s", err, got.body)
		}
		if sent.Config != "inflection_3_pi" {
			t.Errorf("the config sent is %q, want the default model with no model given", sent.Config)
		}
		if len(sent.Context) != 2 {
			t.Fatalf("the body carries %d context entries, want 2", len(sent.Context))
		}
		if sent.Context[0].Type != "Instruction" || sent.Context[1].Type != "Human" {
			t.Errorf("the context types are %q and %q, want Instruction then Human",
				sent.Context[0].Type, sent.Context[1].Type)
		}
		if !strings.Contains(sent.Context[0].Text, "table users(id int)") {
			t.Errorf("the instruction does not carry the schema:\n%s", sent.Context[0].Text)
		}
		if sent.Context[1].Text != "count them" {
			t.Errorf("the human turn is %q, want the question verbatim", sent.Context[1].Text)
		}
	})

	t.Run("an explicit model is sent as given", func(t *testing.T) {
		srv, got := server(t, 200, piBody("SELECT 1"))
		if _, err := newPiAt(srv.URL, "k", "inflection_3_pi_large").Generate(t.Context(), "q", ""); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !strings.Contains(string(got.body), "inflection_3_pi_large") {
			t.Errorf("the body does not carry the model:\n%s", got.body)
		}
	})

	t.Run("every failure is an error, never an empty string with no error", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			status int
			body   string
			want   string
		}{
			{"an HTTP error", 429, `{"error":"slow down"}`, "429"},
			{"a body that is not JSON", 200, "<html>nope</html>", "parse"},
			{"an error object inside a 200", 200, `{"error":{"message":"bad config"}}`, "bad config"},
			{"a success with no entries", 200, `{"entries":[]}`, "empty"},
			{"a success with null entries", 200, `{"entries":null}`, "empty"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv, _ := server(t, tc.status, tc.body)
				sql, err := newPiAt(srv.URL, "k", "").Generate(t.Context(), "q", "")
				if err == nil {
					t.Fatalf("Generate returned %q and NO error, want a failure", sql)
				}
				if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the error is %q, want it to mention %q", err.Error(), tc.want)
				}
				if sql != "" {
					t.Errorf("Generate returned %q alongside an error", sql)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// C4: stripJSONC
// ---------------------------------------------------------------------------

// Scenario: stripJSONC quita los comentarios SIN tocar lo que esta dentro de una cadena.
//
// This is the whole job, and the whole hazard: a "//" inside a string is not a comment,
// and an unescaped quote inside a comment is not a string. Get either backwards and the
// result is not merely a failed parse — it is a parse that SUCCEEDS on the wrong
// document, which is how a config silently points dbx at another host.
func TestStripJSONCConvertsCommentsWithoutTouchingStrings(t *testing.T) {
	// Every case is checked by PARSING the result, not by string equality: the
	// contract is that the output is the JSON the author meant, and a case that
	// produces different bytes for the same document is still fine.
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain JSON is untouched", `{"a":1}`, `{"a":1}`},
		{"a line comment goes", "{\"a\":1} // trailing", `{"a":1}`},
		{"a line comment between tokens goes", "{\n  // who\n  \"a\": 1\n}", "{\n  \"a\": 1\n}"},
		{"a block comment goes", `{"a":1} /* trailing */`, `{"a":1}`},
		{"a block comment between tokens goes", "{ /* x */ \"a\": 1 }", "{ \"a\": 1 }"},
		{"an unterminated block comment runs to the end", `{"a":1} /* oops`, `{"a":1}`},
		{"a trailing comma before an object brace goes", `{"a":1,}`, `{"a":1}`},
		{"a trailing comma before an array brace goes", `[1,2,]`, `[1,2]`},
		{"a trailing comma followed by a comment still goes", "{\"a\":1, // last\n}", `{"a":1}`},
		{"a trailing comma followed by a BLOCK comment still goes", "{\"a\":1, /* last */ }", `{"a":1}`},
		{"a comma NOT before a closer stays", `{"a":1,"b":2}`, `{"a":1,"b":2}`},
		{"a double slash inside a string is KEPT", `{"url":"https://x.io//y"}`, `{"url":"https://x.io//y"}`},
		{"a line comment marker inside a string is KEPT", `{"note":"// not a comment"}`, `{"note":"// not a comment"}`},
		{"a block comment marker inside a string is KEPT", `{"note":"/* nor this */"}`, `{"note":"/* nor this */"}`},
		{"an escaped quote does not end the string", `{"note":"a\"//b"}`, `{"note":"a\"//b"}`},
		{"an escaped backslash does not escape the next quote", `{"note":"a\\"}`, `{"note":"a\\"}`},
		{"a string holding a brace", `{"k":"}"}`, `{"k":"}"}`},
		{"a string holding a comma then a brace", `{"k":",}"}`, `{"k":",}"}`},
		{"an empty document", "", ""},
		{"only a comment", "// nothing here", ""},
		{"only an unterminated block comment", "/* nothing", ""},
		{"a slash that is neither", `{"a":"x/y"}`, `{"a":"x/y"}`},
		{"a lone slash at the very end", `{"a":1}/`, `{"a":1}/`},
		{"a slash at the very end of a line comment", "// x/", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(stripJSONC([]byte(tc.in)))
			// Compared by PARSED VALUE, not byte for byte. The stripper removes
			// the comment and leaves the whitespace that surrounded it, so
			// `{"a":1} // c` becomes `{"a":1} ` with a trailing space — correct,
			// and not what a byte comparison against `{"a":1}` expects. My first
			// version compared bytes and reported seven passing cases as failures.
			if sameJSON(t, got, tc.want) {
				return
			}
			// Not JSON on either side (fragments, empty, bare text), so the only
			// thing left to compare is the text with runs of whitespace collapsed.
			if collapse(got) == collapse(tc.want) {
				return
			}
			t.Errorf("stripJSONC(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		})
	}

	t.Run("a comment marker does not swallow a string's closing quote", func(t *testing.T) {
		// The reverse hazard, stated as its own case because it is the one that
		// corrupts a value rather than losing a comment: a line comment whose
		// text contains a quote must not let the scanner resume INSIDE it.
		in := "{\n  // it's fine\n  \"a\": 1\n}"
		out := stripJSONC([]byte(in))
		if !json.Valid(out) {
			t.Errorf("stripJSONC(%q) = %q, which is not JSON", in, out)
		}
		// The first version of this fixture was two documents joined by a
		// newline, which is not JSON by construction and made the assertion fail
		// for the right reason in the wrong way.
		if !strings.Contains(string(out), `"a": 1`) {
			t.Errorf("stripJSONC(%q) = %q, want the statement to survive", in, out)
		}
	})
}

// ---------------------------------------------------------------------------
// C5: the opencode config reader
// ---------------------------------------------------------------------------

// Scenario: El lector de opencode entiende LAS DOS versiones y busca donde dice.
//
// Two schemas, two spellings, two file names — and this is a user's own agent config,
// which is full of comments. Getting it wrong does not crash: it returns "" and dbx
// quietly uses the default endpoint, so the only symptom is that the AI talks to a
// different host than the user configured.
func TestTheOpenCodeConfigReaderUnderstandsBothSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"v2 settings", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://v2"}}}}`, "http://v2"},
		{"v1 options", `{"provider":{"opencode-go":{"options":{"baseURL":"http://v1"}}}}`, "http://v1"},
		{"v2 under the plain opencode name", `{"providers":{"opencode":{"settings":{"baseURL":"http://plain"}}}}`, "http://plain"},
		{"v2 under the openai name", `{"providers":{"openai":{"settings":{"baseURL":"http://oai"}}}}`, "http://oai"},
		{"with comments", "{ // hi\n\"providers\":{\"opencode-go\":{\"settings\":{\"baseURL\":\"http://c\"}}},\n}", "http://c"},
		{"with a trailing comma", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://tc",}}},}`, "http://tc"},
		{"an empty base URL is not a match", `{"providers":{"opencode-go":{"settings":{}}}}`, ""},
		{"an unrelated provider is ignored", `{"providers":{"ollama":{"settings":{"baseURL":"http://no"}}}}`, ""},
		{"v2 wins over v1 when both are present",
			`{"providers":{"opencode-go":{"settings":{"baseURL":"http://v2"}}},"provider":{"opencode-go":{"options":{"baseURL":"http://v1"}}}}`,
			"http://v2"},
		{"the more specific name wins",
			`{"providers":{"openai":{"settings":{"baseURL":"http://oai"}},"opencode-go":{"settings":{"baseURL":"http://go"}}}}`,
			"http://go"},
		{"a provider with an empty base URL does not shadow a named one",
			`{"providers":{"opencode-go":{"settings":{}},"openai":{"settings":{"baseURL":"http://oai"}}}}`,
			"http://oai"},
		{"not JSON at all", `this is not json`, ""},
		{"JSON with the key in the wrong place", `{"settings":{"baseURL":"http://top"}}`, ""},
		{"empty", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseURLFromOpenCodeConfig([]byte(tc.in)); got != tc.want {
				t.Errorf("baseURLFromOpenCodeConfig(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Scenario: El base URL se busca en .jsonc primero y en .json despues.
//
// v2 writes jsonc and v1 wrote json, so a machine that went through the upgrade has
// both. Which one should win is a decision, and this asserts it: the newer file.
func TestDetectOpenCodeBaseURLPrefersTheNewerFile(t *testing.T) {
	write := func(t *testing.T, home, name, body string) {
		t.Helper()
		dir := filepath.Join(home, ".config", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("jsonc wins when both exist", func(t *testing.T) {
		home := t.TempDir()
		write(t, home, "opencode.jsonc", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://new"}}}}`)
		write(t, home, "opencode.json", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://old"}}}}`)
		if got := detectOpenCodeBaseURL(home); got != "http://new" {
			t.Errorf("with both files present the reader chose %q, want the jsonc one", got)
		}
	})

	t.Run("json is used when there is no jsonc", func(t *testing.T) {
		home := t.TempDir()
		write(t, home, "opencode.json", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://old"}}}}`)
		if got := detectOpenCodeBaseURL(home); got != "http://old" {
			t.Errorf("the reader chose %q, want the json one", got)
		}
	})

	t.Run("a jsonc with no match FALLS THROUGH to the json", func(t *testing.T) {
		// It does, because the loop `continue`s past a file that yields nothing
		// rather than stopping at the first one it can read. My first version
		// asserted the opposite, reasoning that a newer file should shadow an
		// older one; the test caught that the code is more forgiving than that,
		// which is the better behaviour — a v2 config mentioning only other
		// providers leaves a working v1 base URL usable instead of losing it.
		home := t.TempDir()
		write(t, home, "opencode.jsonc", `{"providers":{"someone-else":{}}}`)
		write(t, home, "opencode.json", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://old"}}}}`)
		if got := detectOpenCodeBaseURL(home); got != "http://old" {
			t.Errorf("the reader chose %q, want the json one to be reached", got)
		}
	})

	t.Run("a jsonc that does not parse also falls through", func(t *testing.T) {
		// Same reason: a half-written newer file must not lose a base URL the
		// older one still holds.
		home := t.TempDir()
		write(t, home, "opencode.jsonc", `{ this is not json`)
		write(t, home, "opencode.json", `{"providers":{"opencode-go":{"settings":{"baseURL":"http://old"}}}}`)
		if got := detectOpenCodeBaseURL(home); got != "http://old" {
			t.Errorf("the reader chose %q, want the json one to be reached", got)
		}
	})

	t.Run("neither file means no base URL", func(t *testing.T) {
		if got := detectOpenCodeBaseURL(t.TempDir()); got != "" {
			t.Errorf("an empty home gave %q, want nothing", got)
		}
	})
}

// ---------------------------------------------------------------------------
// C6: the env detectors
// ---------------------------------------------------------------------------

// Scenario: Los detectores de entorno: cual variable gana, y que pasa si no hay ninguna.
//
// The precedence is a real decision with real consequences — two variables naming the
// same provider, and only one of them can win — so it is written down rather than
// inferred from which if comes first in the source.

// clearProviderEnv blanks every variable any detector reads, so a test never inherits the
// developer's own shell. Without this the suite passes on one machine and fails on
// another, which is the worst kind of green.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OPENCODE_API_KEY", "ZEN_API_KEY",
		"NOUS_API_KEY", "HERMES_API_KEY",
		"INFLECTION_API_KEY",
	} {
		t.Setenv(name, "")
	}
}

func TestDetectOpenCodeConfigPrecedence(t *testing.T) {
	t.Run("ZEN_API_KEY supplies the key AND the default endpoint", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ZEN_API_KEY", "zen-key")

		key, model, base, found := DetectOpenCodeConfig()
		if !found {
			t.Fatal("ZEN_API_KEY was not detected")
		}
		if key != "zen-key" {
			t.Errorf("the key is %q", key)
		}
		if base != defaultOpenCodeBaseURL {
			t.Errorf("the base URL is %q, want the default %q", base, defaultOpenCodeBaseURL)
		}
		if model != "mimo-v2.5" {
			t.Errorf("the model is %q, want the default", model)
		}
	})

	t.Run("OPENCODE_API_KEY supplies the key and gets the default endpoint", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("OPENCODE_API_KEY", "opencode-key")

		key, _, base, found := DetectOpenCodeConfig()
		if !found {
			t.Fatal("OPENCODE_API_KEY was not detected")
		}
		if key != "opencode-key" {
			t.Errorf("the key is %q", key)
		}
		// The endpoint comes from the default rather than from the variable,
		// because the variable names a key and not a host.
		if base != defaultOpenCodeBaseURL {
			t.Errorf("the base URL is %q, want the default", base)
		}
	})

	t.Run("ZEN_API_KEY wins over OPENCODE_API_KEY", func(t *testing.T) {
		// Pinned because the two name the same provider and there is no way to
		// tell from the config which one the user meant.
		clearProviderEnv(t)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("OPENCODE_API_KEY", "first")
		t.Setenv("ZEN_API_KEY", "second")

		key, _, _, _ := DetectOpenCodeConfig()
		if key != "second" {
			t.Errorf("the key is %q, want the ZEN one to win", key)
		}
	})

	t.Run("auth.json is read when no variable supplied a key", func(t *testing.T) {
		clearProviderEnv(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".local", "share", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"opencode-go":{"type":"api","key":"from-auth"}}`
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		key, _, base, found := DetectOpenCodeConfig()
		if !found {
			t.Fatal("auth.json was not detected")
		}
		if key != "from-auth" {
			t.Errorf("the key is %q, want from-auth", key)
		}
		if base != defaultOpenCodeBaseURL {
			t.Errorf("the base URL is %q, want the default", base)
		}
	})

	t.Run("auth.json does NOT override a variable that already supplied a key", func(t *testing.T) {
		clearProviderEnv(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("ZEN_API_KEY", "from-env")
		dir := filepath.Join(home, ".local", "share", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"opencode-go":{"type":"api","key":"from-auth"}}`
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		key, _, _, _ := DetectOpenCodeConfig()
		if key != "from-env" {
			t.Errorf("the key is %q, want the environment to win over the file", key)
		}
	})

	t.Run("auth.json entries for a different provider are ignored", func(t *testing.T) {
		clearProviderEnv(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".local", "share", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{
			`{"anthropic":{"type":"api","key":"nope"}}`,
			`{"opencode-go":{"type":"api","key":""}}`,
			`not json at all`,
			`{"opencode-go":{"type":"api"}}`,
		} {
			if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			key, _, _, found := DetectOpenCodeConfig()
			if found {
				t.Errorf("auth.json %q produced the key %q, want nothing detected", body, key)
			}
		}
	})

	t.Run("no key anywhere means not found, and the defaults still come back", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("HOME", t.TempDir())

		key, model, base, found := DetectOpenCodeConfig()
		if found {
			t.Errorf("an empty home detected the key %q", key)
		}
		if key != "" {
			t.Errorf("the key is %q, want empty", key)
		}
		// The endpoint and model are filled in regardless, because the caller
		// uses them to show which provider is selected even before a key exists.
		if base != defaultOpenCodeBaseURL {
			t.Errorf("the base URL is %q, want the default even with no key", base)
		}
		if model != "mimo-v2.5" {
			t.Errorf("the model is %q, want the default even with no key", model)
		}
	})

	t.Run("a configured base URL overrides the default, key or no key", func(t *testing.T) {
		clearProviderEnv(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".config", "opencode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"providers":{"opencode-go":{"settings":{"baseURL":"http://self-hosted"}}}}`
		if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, base, _ := DetectOpenCodeConfig()
		if base != "http://self-hosted" {
			t.Errorf("the base URL is %q, want the configured one to win over the default", base)
		}
	})
}

func TestDetectHermesConfigPrecedence(t *testing.T) {
	t.Run("NOUS_API_KEY alone", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("NOUS_API_KEY", "nous-key")
		key, model, base, found := DetectHermesConfig()
		if !found {
			t.Fatal("not detected")
		}
		if key != "nous-key" {
			t.Errorf("the key is %q, want nous-key", key)
		}
		// Hermes has no variable of its own, so NOUS is the way in and the
		// endpoint and model come from the detector.
		if base == "" || model == "" {
			t.Errorf("the base URL %q and model %q must be filled in when found", base, model)
		}
	})

	t.Run("HERMES_API_KEY supplies the key", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("HERMES_API_KEY", "hermes-key")
		key, model, base, found := DetectHermesConfig()
		if !found {
			t.Fatal("not detected")
		}
		if key != "hermes-key" {
			t.Errorf("the key is %q, want the HERMES one", key)
		}
		if base != "https://inference-api.nousresearch.com/v1" {
			t.Errorf("the base URL is %q", base)
		}
		if model != "Hermes-4-70B" {
			t.Errorf("the model is %q", model)
		}
	})

	t.Run("HERMES_API_KEY wins over NOUS_API_KEY", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("NOUS_API_KEY", "nous")
		t.Setenv("HERMES_API_KEY", "hermes")
		key, _, _, _ := DetectHermesConfig()
		if key != "hermes" {
			t.Errorf("the key is %q, want the provider-named variable to win", key)
		}
	})

	t.Run("neither variable means nothing at all, not even defaults", func(t *testing.T) {
		// Note the asymmetry with opencode, deliberately asserted: hermes and
		// pi return EARLY, so they do not hand back a base URL and a model for
		// a provider that has no key. A caller showing "which provider is
		// selected" has to handle the two shapes differently.
		clearProviderEnv(t)
		key, model, base, found := DetectHermesConfig()
		if found {
			t.Errorf("found=%v with the key %q", found, key)
		}
		if model != "" || base != "" {
			t.Errorf("the detector returned the model %q and base %q with no key, want them empty", model, base)
		}
	})
}

func TestDetectPiConfigPrecedence(t *testing.T) {
	t.Run("INFLECTION_API_KEY alone", func(t *testing.T) {
		clearProviderEnv(t)
		t.Setenv("INFLECTION_API_KEY", "inflection-key")
		key, model, base, found := DetectPiConfig()
		if !found {
			t.Fatal("not detected")
		}
		if key != "inflection-key" {
			t.Errorf("the key is %q, want inflection-key", key)
		}
		if base == "" || model == "" {
			t.Errorf("the base URL %q and model %q must be filled in when found", base, model)
		}
	})

	t.Run("no variable means nothing at all", func(t *testing.T) {
		clearProviderEnv(t)
		key, model, base, found := DetectPiConfig()
		if found {
			t.Errorf("found=%v with the key %q", found, key)
		}
		if model != "" || base != "" {
			t.Errorf("the detector returned the model %q and base %q with no key, want them empty", model, base)
		}
	})
}

// ---------------------------------------------------------------------------
// C7: the jcode reader
// ---------------------------------------------------------------------------

// Scenario: El lector de jcode elige SIEMPRE el mismo proveedor.
//
// The config decodes into a map, and Go randomises map iteration, so the choice used to
// change from call to call: forty reads of one file with three keyed providers gave zzz
// 28 times, aaa 6, mmm 6. The chosen provider's base URL is where the prompt is POSTed,
// so the AI could talk to a different host on every launch with nothing changed on disk.
//
// The fix is alphabetical order. Which of several keyed providers a user "meant" is not
// knowable from the file, so the contract is only that the answer is REPRODUCIBLE — and
// an arbitrary-but-fixed order beats an arbitrary-and-moving one, because it can be
// asserted and because a user who does not like it can rename their providers.
func TestTheJCodeReaderPicksTheSameProviderEveryTime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three keyed providers, deliberately NOT in alphabetical order in the
	// file, so an implementation that happened to preserve document order would
	// pass and a randomised one would not.
	body := `{
	  "providers": {
	    "zzz": {"api_key": "K-ZZZ", "base_url": "http://z.example/v1"},
	    "aaa": {"api_key": "K-AAA", "base_url": "http://a.example/v1"},
	    "mmm": {"api_key": "K-MMM", "base_url": "http://m.example/v1"}
	  },
	  "model": "some/model"
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	first, key, model, base, found := DetectJCodeConfig()
	if !found {
		t.Fatal("not detected")
	}
	t.Logf("chosen: provider=%q base=%q", first, base)

	for i := range 200 {
		provider, apiKey, m, b, f := DetectJCodeConfig()
		if provider != first || apiKey != key || m != model || b != base || f != found {
			t.Fatalf("read %d chose %q/%q/%q, want %q/%q/%q — the choice must not move",
				i, provider, apiKey, b, first, key, base)
		}
	}

	// Alphabetical, and the key, URL and model travel together. The model is a
	// SINGLE global value while the base URL is per provider, so a provider
	// chosen at random meant the global model POSTed to a random host.
	if first != "aaa" {
		t.Errorf("the chosen provider is %q, want the alphabetically first keyed one", first)
	}
	if key != "K-AAA" || base != "http://a.example/v1" {
		t.Errorf("the key %q and base %q do not belong to the provider %q", key, base, first)
	}
	if model != "some/model" {
		t.Errorf("the model is %q, want the file's global one", model)
	}
}

func TestTheJCodeReaderSkipsProvidersWithoutAKey(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		wantFound bool
		wantProv  string
		wantBase  string
		wantModel string
	}{
		{
			name:      "the first keyed provider alphabetically",
			body:      `{"providers":{"b-keyed":{"api_key":"B","base_url":"http://b"},"a-bare":{}},"model":"M"}`,
			wantFound: true, wantProv: "b-keyed", wantBase: "http://b", wantModel: "M",
		},
		{
			name:      "one keyed provider among unkeyed ones",
			body:      `{"providers":{"zzz":{"api_key":"Z","base_url":"http://z"},"a":{},"m":{}},"model":"M"}`,
			wantFound: true, wantProv: "zzz", wantBase: "http://z", wantModel: "M",
		},
		{
			name:      "an EMPTY key is not a key",
			body:      `{"providers":{"a":{"api_key":""},"z":{"api_key":"Z","base_url":"http://z"}},"model":"M"}`,
			wantFound: true, wantProv: "z", wantBase: "http://z", wantModel: "M",
		},
		{
			// The model is a SEPARATE global value, so it is read whenever the
			// file parses — including when nothing is usable to send it. Asserted
			// because a caller has to handle that shape: a model, no provider.
			name:      "no provider has a key",
			body:      `{"providers":{"a":{},"z":{"base_url":"http://z"}},"model":"M"}`,
			wantFound: false, wantProv: "", wantBase: "", wantModel: "M",
		},
		{
			name:      "no providers at all",
			body:      `{"model":"M"}`,
			wantFound: false, wantModel: "M",
		},
		{
			name:      "a provider with a key and NO base URL is still usable",
			body:      `{"providers":{"a":{"api_key":"A"}},"model":"M"}`,
			wantFound: true, wantProv: "a", wantBase: "", wantModel: "M",
		},
		{
			name:      "a keyed provider with NO model in the file",
			body:      `{"providers":{"a":{"api_key":"A"}}}`,
			wantFound: true, wantProv: "a", wantBase: "", wantModel: "",
		},
		{
			name:      "not JSON",
			body:      `{ this is not json`,
			wantFound: false, wantModel: "",
		},
		{
			name:      "an empty document",
			body:      ``,
			wantFound: false, wantModel: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, ".jcode")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}

			provider, key, model, base, found := DetectJCodeConfig()
			if found != tc.wantFound {
				t.Fatalf("found=%v with the provider %q and key %q, want found=%v", found, provider, key, tc.wantFound)
			}
			if found {
				if provider != tc.wantProv {
					t.Errorf("the provider is %q, want %q", provider, tc.wantProv)
				}
				if found && key == "" {
					t.Errorf("the provider %q was chosen but the key is empty", provider)
				}
			}
			if base != tc.wantBase {
				t.Errorf("the base URL is %q, want %q", base, tc.wantBase)
			}
			// The model is read from the file even when NO provider was usable,
			// because it is a separate global value. Asserted because it is the
			// shape a caller has to handle: a model with nothing to send it.
			if model != tc.wantModel {
				t.Errorf("the model is %q, want %q", model, tc.wantModel)
			}
		})
	}
}

func TestTheJCodeReaderWithNoHomeOrNoFile(t *testing.T) {
	t.Run("no config file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		provider, _, model, _, found := DetectJCodeConfig()
		if found {
			t.Errorf("found=%v with the provider %q", found, provider)
		}
		if model != "" {
			t.Errorf("the model is %q with no file, want empty", model)
		}
	})
}

// contextWithCancel is a context already cancelled, for the "the user pressed ctrl+c"
// case. Named so the call site reads as what it is.
func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// newPiAt is a Pi provider pointed at a local server.
//
// It exists because PiProvider's base URL is a CONSTANT baked in by NewPi, with no
// parameter and no setter, while its sibling OpenAICompatible takes one in its
// constructor. That asymmetry is why this function's body was unreachable before: the
// only way to exercise it was to talk to Inflection for real. The seam is the
// unexported field, which a same-package test can write — no production signature
// changes, and the hardcoded host stays the default for the one real caller.
//
// If Pi ever grows a second endpoint, the fix is a constructor parameter rather than
// this, and the asymmetry above is the reason to.
func newPiAt(baseURL, apiKey, model string) *PiProvider {
	p := NewPi(apiKey, model)
	p.baseURL = baseURL
	return p
}

// sameJSON reports whether two documents parse to the same value. It reports FALSE when
// either does not parse, so the caller falls through to a textual comparison.
func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// collapse reduces runs of whitespace to a single space, for comparing documents that
// are not JSON at all.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
