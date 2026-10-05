package nl2sql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Anthropic struct {
	apiKey  string
	model   string
	baseURL string
}

func NewAnthropic(apiKey, model string) *Anthropic {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return NewAnthropicAt(apiKey, model, anthropicDefaultBaseURL)
}

// anthropicDefaultBaseURL is the BASE, with the version path appended by the constructor —
// the same shape as piDefaultBaseURL and NewOpenAICompatible, so a test endpoint and a
// production one are combined the same way. It was briefly the full endpoint, which made
// the constructor build ".../v1/v1/messages" and every request 404.
const anthropicDefaultBaseURL = "https://api.anthropic.com"

// NewAnthropicAt is NewAnthropic with an explicit endpoint, for the reason NewPiAt exists:
// the endpoint was baked into Generate, so the six arms that handle a malformed response
// could not be reached from a test. Two of the four providers had the seam and two did not,
// which is the only reason this was worth fixing twice.
func NewAnthropicAt(apiKey, model, baseURL string) *Anthropic {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &Anthropic{apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/") + "/v1/messages"}
}

func (a *Anthropic) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system"`
	Messages  []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Generate(ctx context.Context, prompt string, schema string) (string, error) {
	system := BuildSystemPrompt(schema)

	body, _ := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: 1024,
		System:    system,
		Messages: []anthropicMsg{
			{Role: "user", Content: prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic")
	}

	sql := strings.TrimSpace(result.Content[0].Text)
	sql, err = requireSQL("anthropic", sql)
	if err != nil {
		return "", err
	}
	return sql, nil
}

// cleanSQL strips the markdown fence a model wraps its answer in.
//
// The tag is matched CASE-INSENSITIVELY and any tag is accepted, because the models do
// not agree on one: the same prompt that comes back as ```sql on one run comes back as
// ```SQL on the next, and the old exact-prefix match left "SQL\nSELECT 1;" — which the
// user reads as a syntax error from dbx rather than as a fence we failed to strip. A
// bare ``` is by far the most common answer and the old code handled it, which is
// exactly why the bug survived: the common case worked.
//
// Not handled, and pinned as known rather than fixed here: prose BEFORE the fence
// ("Here is the query:" then a fenced block). Handling that means deciding how much
// leading text to discard, and discarding the wrong amount turns a working query into
// none.
func cleanSQL(sql string) string {
	sql = strings.TrimSpace(sql)

	// A FENCE ANYWHERE means the answer is inside it, so the prose around it goes.
	//
	// This used to require the fence at position zero, while the comment above promised
	// to handle "Here is the query:" followed by a block — and "Here is the query:" is
	// the single most common shape a model returns. So the prose, the opening backticks
	// and the tag all reached the user in the ASK panel, and the SQL that came out looked
	// like a broken statement rather than like prose around a statement.
	//
	// Taking the CONTENT rather than trimming the prefix is also what makes the leading-
	// prose case work at all: there is no way to strip a prefix of unknown length without
	// knowing where it ends, and the fence is the only marker that says.
	//
	// `open >= 0` covers a fence at position zero too, so there is no separate case for
	// the bare statement.
	if open := strings.Index(sql, "```"); open >= 0 {
		rest := sql[open+3:]
		// And the closing fence is where the answer ENDS, not a suffix to trim. The
		// TrimSuffix at the bottom of this function only removed one when it happened to
		// be last, so prose after the block leaked the marker into the statement:
		// "```sql\nSELECT 1\n```\nThat is all." came out as "SELECT 1\n```\nThat is
		// all." — a visible break rather than a silent one, but the content of a fence is
		// its whole answer, and this is where that is decided.
		if closed := strings.Index(rest, "```"); closed >= 0 {
			rest = rest[:closed]
		}
		switch {
		case strings.ContainsRune(rest, '\n'):
			// ONE decision, and it used to be two that disagreed.
			//
			// The reader said "is the first line a tag" and, when the answer was no,
			// the CALLER fell back to "take everything after the first newline". That
			// fallback is right for a fence whose next line is the statement — and it is
			// exactly what ate a statement that starts on the line right after the
			// backticks, because "DELETE\nFROM t" and "sql\nSELECT 1" have the same
			// shape and only one of them is a tag.
			//
			// The consequence was a wrong statement running silently:
			//
			//	```DELETE\nFROM t   ->  "FROM t"
			//	```SELECT\n1         ->  "1"
			//
			// The app would run FROM t, report success, and delete nothing — the user
			// asked for a delete and the app said it did one.
			//
			// So the empty-first-line case is handled INSIDE the reader, where it can be
			// told apart from an unrecognised first word, and the caller has no fallback
			// left to get wrong: false means the first line is part of the statement.
			if after, ok := stripFenceTag(rest); ok {
				sql = after
			} else {
				sql = rest
			}
		default:
			// A GLUED fence with no newline: "```sqlSELECT 1" or "```SELECT 1".
			// This is ambiguous with a tag on the same line, and there is no rule
			// that separates "```sqlSELECT 1" from "```SQL SELECT 1" — both are
			// three backticks, some letters, then a space. The exact lowercase
			// prefix decides it, because that is the reading the suite already
			// pinned and the reading a model actually emits.
			//
			// The consequence, stated so it is not rediscovered: a one-line
			// fence with an UPPERCASE tag and no trailing newline keeps leaking
			// its tag. "```SQL SELECT 1 ```" gives "SQL SELECT 1". A model that
			// puts the fence on its own line — which is what every markdown
			// renderer produces and what the models return in practice — takes
			// the branch above and is clean.
			sql = strings.TrimPrefix(rest, "sql")
		}
	}

	return strings.TrimSpace(strings.TrimSuffix(sql, "```"))
}

// requireSQL is the single gate between a model's message and the editor: it cleans the
// fences off and refuses anything that is not a statement.
//
// It exists because the check was inlined in the callers, and the callers had drifted —
// six of them called cleanSQL and only TWO guarded the result. A refusal, a safety filter,
// a truncated response and a plain-English "here is what that table looks like" all come
// back as message content, and four of the six providers passed all of them straight
// through. The visible symptom is an English sentence landing in the SQL editor, and then
// in PostgreSQL.
//
// The error text is the one the two providers that did guard were already producing, so
// the message a user sees does not change.
func requireSQL(provider, raw string) (string, error) {
	sql := cleanSQL(raw)
	if sql == "" || !looksLikeSQL(sql) {
		return "", fmt.Errorf("no SQL in the response from %s", provider)
	}
	return sql, nil
}

// sqlStatementStarts are the words a statement can begin with. It is not the whole of
// SQL — it is the set that covers what these providers are asked to produce, which is
// SELECT, DML, DDL and the handful of transaction and session statements a user might
// reasonably ask for.
var sqlStatementStarts = map[string]bool{
	"SELECT": true, "WITH": true, "TABLE": true, "VALUES": true,
	"INSERT": true, "UPDATE": true, "DELETE": true, "UPSERT": true,
	"MERGE": true, "COPY": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "COMMENT": true,
	"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "SAVEPOINT": true, "START": true,
	"SET": true, "RESET": true, "SHOW": true, "EXPLAIN": true, "ANALYZE": true,
	"GRANT": true, "REVOKE": true, "VACUUM": true, "REFRESH": true, "CALL": true,
	"DO": true, "LOCK": true, "PREPARE": true, "EXECUTE": true, "DEALLOCATE": true,
}

// looksLikeSQL reports whether the cleaned message contains a statement.
//
// It scans LINE BY LINE and asks whether any line begins with a statement keyword, rather
// than looking only at the very first line. The first-line-only version rejected
// "Here you go:\nSELECT 1", which is the single most common shape a real model returns —
// and rejecting it would have traded one wrong answer for a worse one, a refusal that is
// never detected at all.
//
// A line is skipped when it starts with a comment marker or an open paren, so
// `-- filter\nSELECT 1` and `(SELECT 1)` both count. A refusal does not contain a line
// starting with SELECT anywhere, which is the whole property.
func looksLikeSQL(sql string) bool {
	for _, line := range strings.Split(sql, "\n") {
		if statementOnLine(line) {
			return true
		}
	}
	return false
}

// statementOnLine is looksLikeSQL applied to one line: skip the leading whitespace, any
// comment introducers and any open parens, then read the first word.
// isWordRune is one character of a SQL identifier: a letter, a digit or an underscore.
// Digits are included because a query may start with one after a keyword, and because the
// scan reads the first word off the front of a line that may be a continuation.
func isWordRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func statementOnLine(line string) bool {
	rest := line
	for {
		trimmed := strings.TrimLeft(rest, " \t\r")
		switch {
		case strings.HasPrefix(trimmed, "--"):
			return false
		case strings.HasPrefix(trimmed, "/*"):
			if end := strings.Index(trimmed[2:], "*/"); end >= 0 {
				rest = trimmed[2+end+2:]
				continue
			}
			return false // an unterminated comment swallows the rest of the line
		case strings.HasPrefix(trimmed, "("):
			rest = trimmed[1:]
			continue
		}

		word := trimmed
		if i := strings.IndexFunc(word, func(r rune) bool {
			return !isWordRune(r)
		}); i >= 0 {
			word = word[:i]
		}
		return sqlStatementStarts[strings.ToUpper(word)]
	}
}

// stripFenceTag removes the first line of a fenced block when that line is a language
// tag rather than the start of the statement, and says whether it did.
//
// THE FIRST LINE IS KEPT IF IT LOOKS LIKE A STATEMENT. That is the whole rule, and it is
// stated that way rather than as a list of languages because:
//
//	"```sql\nSELECT 1"    the first line is a tag   -> strip it
//	"```DELETE\nFROM t"  the first line is a verb   -> keep it
//
// have the same shape, so shape cannot tell them apart — only the content can. A list of
// language tags CAN tell them apart, and was the first version of this, but a list has to
// be maintained and it falls behind: it has to know about `postgresql`, `plpgsql` and
// `c++`, and every language it has not heard of silently costs a statement.
//
// Reading the first line with the SAME predicate that decides whether a whole message is a
// statement — statementOnLine — closes both ends at once. `sql` and `json` are not
// statements, so both are stripped. `DELETE` and `WITH` are, so both are kept. Nothing to
// maintain, and the two decisions cannot disagree because they are the same decision.
//
// The cost is a fence mis-tagged with something SQL-shaped — `with`, say — would keep its
// tag. That is the cheap direction: one stray word in front of a statement PostgreSQL will
// reject, against silently running a different statement.
func stripFenceTag(s string) (string, bool) {
	first := s
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		first = s[:nl]
	}

	// A statement — or anything a statement could legitimately start with — is kept whole.
	// That is the whole fix for the reader/caller split: when this returns false the caller
	// keeps `s` verbatim and there is no second opinion left to disagree with it.
	if statementOnLine(first) {
		return s, false
	}

	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		return s[nl+1:], true
	}
	// One line and it is not a statement: nothing is left but the closing fence.
	return "", true
}
