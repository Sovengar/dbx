package nl2sql

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// providerTransport is an http.RoundTripper that answers every request from a
// canned response and records what was sent.
//
// The four providers call http.DefaultClient against hardcoded https URLs, so
// httptest.Server cannot be pointed at them. Swapping the transport on
// http.DefaultClient is the seam that reaches the real request path, headers and
// body without touching the network.
type providerTransport struct {
	status int
	body   string
	// err, when set, is returned instead of a response, which is how the
	// transport-level failure branch is reached.
	err error

	// recorded
	gotReq  *http.Request
	gotBody string
}

func (p *providerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p.gotReq = r
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		p.gotBody = string(b)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &http.Response{
		StatusCode: p.status,
		Body:       io.NopCloser(strings.NewReader(p.body)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

// withProviderTransport installs tr as the transport for the duration of the
// test and restores the previous one afterwards.
func withProviderTransport(t *testing.T, tr *providerTransport) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = prev })
}

// providerCase describes one of the four chat-completion providers. They share
// a shape, so one table exercises all of them and any divergence between them
// shows up as a failing case rather than as four separate suites.
type providerCase struct {
	name      string
	newClient func(apiKey, model string) Provider
	// wantModelDefault is the model used when the caller passes none.
	wantModelDefault string
	// wantName is what Name() reports.
	wantName string
	// wantURL is the endpoint the provider must call.
	wantURL string
	// authHeader and authPrefix are how the key is sent. Anthropic uses a
	// dedicated header with no prefix; the rest use Bearer.
	authHeader string
	authPrefix string
	// wantExtraHeaders are headers the provider must also set.
	wantExtraHeaders map[string]string
	// wantSystemRole is how the system prompt reaches the provider: as a
	// message with this role, or via a dedicated top-level field.
	wantSystemRole string
	// wantSystemInField is the top-level request field carrying the system
	// prompt, for providers that do not send it as a message.
	wantSystemInField string
	// wantMaxTokens is the token cap sent, or 0 when the provider sends none.
	wantMaxTokens float64
	// successBody builds a 200 response carrying sql in this provider's shape.
	// Anthropic is the odd one out: its text lives at content[].text, not at
	// choices[0].message.content.
	successBody func(sql string) string
}

func providerCases() []providerCase {
	bearer := map[string]string{"Content-Type": "application/json"}
	anthropicHeaders := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
	}
	// choicesBody is the shared OpenAI-compatible success shape.
	choicesBody := func(sql string) string {
		escaped, _ := json.Marshal(sql)
		return `{"choices":[{"message":{"content":` + string(escaped) + `}}]}`
	}
	return []providerCase{
		{
			name:             "openai",
			newClient:        func(k, m string) Provider { return NewOpenAI(k, m) },
			wantModelDefault: "gpt-4o",
			wantName:         "openai",
			wantURL:          "https://api.openai.com/v1/chat/completions",
			authHeader:       "Authorization",
			authPrefix:       "Bearer ",
			wantExtraHeaders: bearer,
			wantSystemRole:   "system",
			wantMaxTokens:    1024,
			successBody:      choicesBody,
		},
		{
			name:             "deepseek",
			newClient:        func(k, m string) Provider { return NewDeepSeek(k, m) },
			wantModelDefault: "deepseek-chat",
			wantName:         "deepseek",
			wantURL:          "https://api.deepseek.com/v1/chat/completions",
			authHeader:       "Authorization",
			authPrefix:       "Bearer ",
			wantExtraHeaders: bearer,
			wantSystemRole:   "system",
			wantMaxTokens:    1024,
			successBody:      choicesBody,
		},
		{
			name:             "qwen",
			newClient:        func(k, m string) Provider { return NewQwen(k, m) },
			wantModelDefault: "qwen-turbo",
			wantName:         "qwen",
			wantURL:          "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
			authHeader:       "Authorization",
			authPrefix:       "Bearer ",
			wantExtraHeaders: bearer,
			wantSystemRole:   "system",
			wantMaxTokens:    0, // qwen sends no max_tokens
			successBody:      choicesBody,
		},
		{
			name:             "anthropic",
			newClient:        func(k, m string) Provider { return NewAnthropic(k, m) },
			wantModelDefault: "claude-sonnet-4-20250514",
			wantName:         "anthropic",
			wantURL:          "https://api.anthropic.com/v1/messages",
			authHeader:       "x-api-key",
			authPrefix:       "",
			wantExtraHeaders: anthropicHeaders,
			// Anthropic does not send the system prompt as a message; it has a
			// dedicated top-level field, and its messages hold the user turn only.
			wantSystemInField: "system",
			wantMaxTokens:     1024,
			successBody: func(sql string) string {
				escaped, _ := json.Marshal(sql)
				return `{"content":[{"text":` + string(escaped) + `}]}`
			},
		},
	}
}

// Scenario: Cada provider devuelve el SQL que le llega en la respuesta.
func TestProvider_GenerateReturnsTheSQL(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: 200, body: tc.successBody("SELECT id FROM users")}
			withProviderTransport(t, tr)

			got, err := tc.newClient("secret-key", "my-model").Generate(context.Background(), "list users", "")
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if want := "SELECT id FROM users"; got != want {
				t.Errorf("Generate = %q, want %q", got, want)
			}
		})
	}
}

// Scenario: Cada provider va a su propio endpoint y manda la clave en su
// cabecera.
//
// The endpoint and the auth header are the two things that most easily get
// crossed between providers, and a wrong one fails at runtime against the real
// API with an opaque 401.
func TestProvider_RequestShape(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: 200, body: tc.successBody("SELECT 1")}
			withProviderTransport(t, tr)

			if _, err := tc.newClient("secret-key", "my-model").Generate(context.Background(), "p", "s"); err != nil {
				t.Fatalf("Generate: %v", err)
			}

			req := tr.gotReq
			if req == nil {
				t.Fatal("no request was sent")
			}
			if req.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", req.Method)
			}
			if got := req.URL.String(); got != tc.wantURL {
				t.Errorf("URL = %q, want %q", got, tc.wantURL)
			}
			if got, want := req.Header.Get(tc.authHeader), tc.authPrefix+"secret-key"; got != want {
				t.Errorf("%s = %q, want %q", tc.authHeader, got, want)
			}
			for k, v := range tc.wantExtraHeaders {
				if got := req.Header.Get(k); got != v {
					t.Errorf("header %s = %q, want %q", k, got, v)
				}
			}
			// The body must be valid JSON carrying the model and the prompt.
			var sent map[string]any
			if err := json.Unmarshal([]byte(tr.gotBody), &sent); err != nil {
				t.Fatalf("request body is not JSON: %v\n%s", err, tr.gotBody)
			}
			if got := sent["model"]; got != "my-model" {
				t.Errorf("model = %v, want %q", got, "my-model")
			}
			// The system prompt must be present, in whichever place this provider
			// puts it. A schema that never reaches the model means it invents
			// tables, so its absence is a silent failure.
			schemaSystem := BuildSystemPrompt("s")
			if tc.wantSystemInField != "" {
				if got, _ := sent[tc.wantSystemInField].(string); got != schemaSystem {
					t.Errorf("%s = %.40q, want the built system prompt", tc.wantSystemInField, got)
				}
			} else {
				msgs, _ := sent["messages"].([]any)
				found := false
				for _, m := range msgs {
					mm, _ := m.(map[string]any)
					if mm["role"] == tc.wantSystemRole && mm["content"] == schemaSystem {
						found = true
					}
				}
				if !found {
					t.Errorf("no %q message carrying the system prompt in %s", tc.wantSystemRole, tr.gotBody)
				}
			}
			// The user prompt must be present as a user turn.
			if !strings.Contains(tr.gotBody, `"p"`) {
				t.Errorf("the prompt is missing from the request body: %s", tr.gotBody)
			}
			// The token cap, when the provider sends one.
			if tc.wantMaxTokens != 0 {
				if got := sent["max_tokens"]; got != tc.wantMaxTokens {
					t.Errorf("max_tokens = %v, want %v", got, tc.wantMaxTokens)
				}
			}
		})
	}
}

// Scenario: Sin modelo explícito se usa el modelo por defecto del provider.
func TestProvider_DefaultModel(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: 200, body: tc.successBody("SELECT 1")}
			withProviderTransport(t, tr)

			if _, err := tc.newClient("k", "").Generate(context.Background(), "p", ""); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			var sent map[string]any
			if err := json.Unmarshal([]byte(tr.gotBody), &sent); err != nil {
				t.Fatalf("request body is not JSON: %v", err)
			}
			if got := sent["model"]; got != tc.wantModelDefault {
				t.Errorf("model = %v, want the default %q", got, tc.wantModelDefault)
			}
		})
	}
}

// Scenario: El nombre de cada provider es el suyo, para los logs y la config.
func TestProvider_Name(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.newClient("k", "m").Name(); got != tc.wantName {
				t.Errorf("Name() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// Scenario: Un estado distinto de 200 se convierte en un error que incluye el
// código y el cuerpo.
//
// The body matters: it is where the provider explains the failure, and dropping
// it turns a fixable 401 into an unactionable "request failed".
func TestProvider_NonOKStatusIsAnError(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: http.StatusUnauthorized, body: "invalid api key"}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err == nil {
				t.Fatal("Generate returned no error for a 401")
			}
			if got != "" {
				t.Errorf("Generate = %q, want the empty string alongside the error", got)
			}
			if !strings.Contains(err.Error(), "401") {
				t.Errorf("error = %q, want it to carry the status code", err)
			}
			if !strings.Contains(err.Error(), "invalid api key") {
				t.Errorf("error = %q, want it to carry the response body", err)
			}
		})
	}
}

// Scenario: Un error de error object en un 200 también es un error.
//
// A provider can answer 200 with an error payload, and treating that as success
// would hand the user an empty SQL string to run.
func TestProvider_ErrorPayloadOnAnOKStatus(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"error":{"message":"quota exceeded","type":"rate_limit"}}`
			tr := &providerTransport{status: 200, body: body}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err == nil {
				t.Fatal("Generate returned no error for an error payload")
			}
			if got != "" {
				t.Errorf("Generate = %q, want the empty string alongside the error", got)
			}
			if !strings.Contains(err.Error(), "quota exceeded") {
				t.Errorf("error = %q, want the provider's own message", err)
			}
		})
	}
}

// Scenario: Una respuesta 200 sin contenido es un error, no un SQL vacío.
//
// Returning "" with no error would make the app run an empty statement, which
// is a worse failure than a clear message.
func TestProvider_EmptyContentIsAnError(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Valid JSON, valid status, but no choices and no content blocks.
			tr := &providerTransport{status: 200, body: `{"choices":[]}`}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err == nil {
				t.Fatal("Generate returned no error for an empty response")
			}
			if got != "" {
				t.Errorf("Generate = %q, want the empty string", got)
			}
			if !strings.Contains(err.Error(), "empty") {
				t.Errorf("error = %q, want it to say the response was empty", err)
			}
		})
	}
}

// Scenario: Un cuerpo que no es JSON se reporta como error de parseo.
func TestProvider_UnparseableBodyIsAnError(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: 200, body: "not json at all"}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err == nil {
				t.Fatal("Generate returned no error for a non-JSON body")
			}
			if got != "" {
				t.Errorf("Generate = %q, want the empty string", got)
			}
			if !strings.Contains(err.Error(), "parse") {
				t.Errorf("error = %q, want it to name the parse failure", err)
			}
		})
	}
}

// Scenario: Un fallo de transporte se reporta como error, no como SQL vacío.
func TestProvider_TransportFailureIsAnError(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{err: errors.New("dial tcp: connection refused")}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err == nil {
				t.Fatal("Generate returned no error for a transport failure")
			}
			if got != "" {
				t.Errorf("Generate = %q, want the empty string", got)
			}
			if !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("error = %q, want it to wrap the transport error", err)
			}
		})
	}
}

// Scenario: La petición lleva el contexto del llamador, para que un ASK
// cancelado aborte contra la API.
//
// What the provider must guarantee is that it does not detach from the
// caller's context, which http.NewRequestWithContext provides. Cancellation
// itself is enforced by the transport, so a stub cannot observe it: the
// assertion is that the context travels on the request, not that the stub
// honours it.
func TestProvider_RequestCarriesTheCallerContext(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			tr := &providerTransport{status: 200, body: tc.successBody("SELECT 1")}
			withProviderTransport(t, tr)

			type ctxKey struct{}
			ctx := context.WithValue(context.Background(), ctxKey{}, "carried")
			if _, err := tc.newClient("k", "m").Generate(ctx, "p", ""); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if tr.gotReq == nil {
				t.Fatal("no request was sent")
			}
			if got := tr.gotReq.Context().Value(ctxKey{}); got != "carried" {
				t.Errorf("the request context does not carry the caller's values: %v", got)
			}
		})
	}
}

// Scenario: El SQL llega limpio, sin la valla de código que los modelos añaden.
func TestProvider_CodeFencesAreStripped(t *testing.T) {
	for _, tc := range providerCases() {
		t.Run(tc.name, func(t *testing.T) {
			fenced := "```sql\nSELECT id FROM users\n```"
			tr := &providerTransport{status: 200, body: tc.successBody(fenced)}
			withProviderTransport(t, tr)

			got, err := tc.newClient("k", "m").Generate(context.Background(), "p", "")
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if want := "SELECT id FROM users"; got != want {
				t.Errorf("Generate = %q, want %q (the fences must be stripped)", got, want)
			}
		})
	}
}

// Scenario: cleanSQL quita la valla con o sin etiqueta de lenguaje, y recorta los
// espacios de los bordes.
//
// A model that emits a bare ``` fence, or ```SQL in caps, must still produce
// runnable SQL. The order matters: the ```sql prefix has to go before the bare
// ``` one, or the first TrimPrefix would not match.
func TestCleanSQL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"```sql\nSELECT 1\n```", "SELECT 1"},
		{"```sqlSELECT 1```", "SELECT 1"},
		{"```\nSELECT 1\n```", "SELECT 1"},
		{"```SELECT 1```", "SELECT 1"},
		{"  SELECT 1  ", "SELECT 1"},
		{"\n\nSELECT 1\n\n", "SELECT 1"},
		{"SELECT 1", "SELECT 1"},
		{"", ""},
		{"```sql\nSELECT 1;\nSELECT 2;\n```", "SELECT 1;\nSELECT 2;"},
	} {
		if got := cleanSQL(tc.in); got != tc.want {
			t.Errorf("cleanSQL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Scenario: Una valla con la palabra SQL en mayúsculas deja el resto de la
// sentencia dentro del SQL.
//
// KNOWN DEFECT, pinned so a fix is a deliberate change.
//
// cleanSQL strips the literal "```sql" prefix and the bare "```" prefix, both
// lowercase, then the trailing "```". A model that emits "```SQL" therefore
// gets the bare-fence strip, which removes the backticks and leaves the word
// "SQL" glued to the front of the statement. The result parses as a column
// alias, so the app shows a plausible-looking error about a missing relation
// rather than "the model sent a fence".
//
// The fix is to compare the language tag case-insensitively, or to strip any
// leading fence plus optional language word. That is a production change, so it
// is not made here; the actual behaviour is asserted instead, and the case
// below flips to the intended value when it is fixed.
func TestCleanSQL_UppercaseLanguageTagLeaksIntoTheSQL(t *testing.T) {
	for _, in := range []string{
		"```SQL\nSELECT 1\n```",
		"```SQL SELECT 1 ```",
		"```Sql\nSELECT 1\n```",
	} {
		got := cleanSQL(in)
		if got == in {
			t.Errorf("cleanSQL(%q) = %q, want the backticks at least removed", in, got)
		}
		// The defect: the language word survives and is no longer a fence.
		if !strings.HasPrefix(got, "SQL") && !strings.HasPrefix(got, "Sql") {
			t.Errorf("cleanSQL(%q) = %q, expected the leaked language word per the KNOWN DEFECT note", in, got)
		}
	}

	// The intended behaviour, for whoever fixes it: no language word at all.
	if got := cleanSQL("```SQL\nSELECT 1\n```"); got == "SELECT 1" {
		t.Log("cleanSQL now handles an uppercase language tag; flip the assertions above")
	}
}
