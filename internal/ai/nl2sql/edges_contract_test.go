package nl2sql

// Scenario: Los bordes de la deteccion de config y de la extraccion de SQL de un modelo.
//
// Three functions, three different reasons their last branch had no test:
//
//   - DetectJCodeConfig's UserHomeDir refusal. os.UserHomeDir on Unix reads $HOME and errors
//     when it is EMPTY — so a missing home is reachable, and it is a state a CI runner and a
//     sandboxed process really are in. t.Setenv is what makes it reachable at all: nothing
//     about the function's own logic can.
//   - stripFenceTag's "one line and it is not a statement" arm: a fenced block with no newline
//     inside, which is what a model emits when it has nothing but a tag to say.
//   - the openai requireSQL refusal, which the endpoint table could not reach because openai
//     was built through the compatible shape.
//
// Each is a decision with a fallback, and a fallback that never runs is a fallback nobody has
// checked is the right answer.

import (
	"strings"
	"testing"
)

// An EMPTY home, not an unset one: os.UserHomeDir errors on an empty $HOME on Unix, and
// t.Setenv("HOME", "") is the only way to reach that without unsetting a variable the rest of
// the process is using. Nothing about the function's logic can produce this state.
func TestDetectJCodeConfigWithNoHome(t *testing.T) {
	t.Setenv("HOME", "")

	provider, apiKey, model, baseURL, found := DetectJCodeConfig()

	if found {
		t.Errorf("a config was detected with no home directory: provider=%q key=%q model=%q url=%q",
			provider, apiKey, model, baseURL)
	}
	// Every value comes back empty rather than partly populated, because a caller that ignores
	// `found` and uses baseURL directly would otherwise connect to "".
	if provider != "" || apiKey != "" || model != "" || baseURL != "" {
		t.Errorf("a failed detection returned provider=%q apiKey=%q model=%q baseURL=%q",
			provider, apiKey, model, baseURL)
	}

	t.Run("and with a home the lookup runs instead of refusing", func(t *testing.T) {
		// The counterweight: a Detect that always returned not-found would satisfy the case
		// above and the feature would be silently dead. A real home does not have to yield a
		// config — it has to get PAST the home lookup, which is the arm under test.
		t.Setenv("HOME", t.TempDir())

		provider, apiKey, model, baseURL, found := DetectJCodeConfig()

		// Whether a config exists here is the developer's business; what must hold is that the
		// answer is coherent — found means values, not found means no values.
		if found {
			if provider == "" || baseURL == "" {
				t.Errorf("found=true with provider=%q baseURL=%q", provider, baseURL)
			}
			return
		}
		if provider != "" || apiKey != "" || model != "" || baseURL != "" {
			t.Errorf("found=false yet values came back: provider=%q apiKey=%q model=%q baseURL=%q",
				provider, apiKey, model, baseURL)
		}
	})
}

// The fence arm with no newline. stripFenceTag returns ("", true): the tag is gone and nothing
// is left, which the caller must read as "the model said nothing" rather than as "here is your
// SQL".
func TestAFenceWithNothingInItYieldsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"a bare tag with no closing fence", "```sql", ""},
		{"a fenced block with no body", "```sql```", ""},
		{"an empty backtick run", "```", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, stripped := stripFenceTag(tc.in)

			if !stripped {
				t.Errorf("stripFenceTag(%q) said nothing was stripped; a tag is not a statement", tc.in)
			}
			if got != tc.want {
				t.Errorf("stripFenceTag(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "```") {
				t.Errorf("the tag survived: %q", got)
			}
		})
	}

	t.Run("and only the FIRST line goes — the closing fence is the caller's job", func(t *testing.T) {
		// The contract, which is narrower than the name suggests: stripFenceTag removes the
		// opening tag line and nothing else. The closing fence survives, because the caller
		// (the fence reader above it) is the one that knows both ends are present.
		//
		// My first version of this case asserted no fence survived, and the code was right:
		// a function that removed the closing fence too would eat the one that a statement
		// merely CONTAINS, and the two are not distinguishable from inside it.
		const fenced = "```sql\nSELECT 1\nFROM t\n```"

		got, stripped := stripFenceTag(fenced)

		if !stripped {
			t.Errorf("the tag on %q was not stripped", fenced)
		}
		if strings.Contains(got, "```sql") {
			t.Errorf("the OPENING tag survived: %q", got)
		}
		if !strings.Contains(got, "SELECT 1") || !strings.Contains(got, "FROM t") {
			t.Errorf("the statement was lost: %q", got)
		}
		// The closing fence is still there, which is what makes it this function's job and
		// not the reader's.
		if !strings.HasSuffix(got, "```") {
			t.Errorf("the closing fence was removed too, which is the caller's job: %q", got)
		}
	})

	t.Run("and a tag line above a one-line statement is still stripped", func(t *testing.T) {
		// The case that looks like the counterweight and is not: "one line" in the function's
		// sense means the WHOLE input is one line. A tag line plus a statement is two lines, so
		// the tag goes and the statement stays. My first version called this "keeps
		// everything" and asserted the opposite of the design.
		got, stripped := stripFenceTag("```sql\nSELECT 1```")

		if !stripped {
			t.Error("the tag was not stripped")
		}
		if strings.Contains(got, "```sql") {
			t.Errorf("the opening tag survived: %q", got)
		}
		if !strings.Contains(got, "SELECT 1") {
			t.Errorf("the statement was lost: %q", got)
		}
	})

	t.Run("and a statement on the FIRST line is never touched", func(t *testing.T) {
		// The real counterweight, and the branch that exists so a statement is never lost:
		// the first line IS a statement, so nothing is stripped and the caller keeps the input
		// verbatim.
		const oneLine = "DELETE FROM t"

		got, stripped := stripFenceTag(oneLine)

		if stripped {
			t.Errorf("a bare statement was stripped to %q; running that would fail", got)
		}
		if got != oneLine {
			t.Errorf("stripFenceTag(%q) = %q, want it unchanged", oneLine, got)
		}
	})
}

// openai's own client, not the compatible shape. The two answer to the same wire format but
// are separate files with separate structs, and the endpoint table reached the compatible one
// first — which is why this refusal went uncovered while its twin in compatible.go did not.
//
// The refusal itself is the same: a model that answered with prose is not an error to paper
// over. Returning it as SQL would make the app run whatever the prose happened to contain.
func TestOpenAIRefusesAnAnswerThatIsNotSQL(t *testing.T) {
	base := serveAt(t, 200, `{"choices":[{"message":{"content":"I am not able to help with that."}}]}`)

	p := NewOpenAIAt("test-key", "gpt-4o", base)

	_, err := p.Generate(t.Context(), "a question", "a schema")
	if err == nil {
		t.Fatal("a prose answer was accepted as SQL")
	}
	if !strings.Contains(err.Error(), "no SQL") {
		t.Errorf("the error is %q, want it to say there is no SQL in the answer", err)
	}
	if !strings.Contains(err.Error(), "openai") {
		t.Errorf("the error is %q, want it to NAME the provider — the user configured it", err)
	}

	t.Run("and the same prose through the compatible shape is refused the same way", func(t *testing.T) {
		// The counterweight that also pins the pair: the two must not drift, or the same
		// failure is reported differently depending on which provider the user configured.
		c := NewOpenAICompatible("my-proxy", "test-key", "some-model", base)

		_, err := c.Generate(t.Context(), "a question", "a schema")
		if err == nil {
			t.Fatal("a prose answer was accepted as SQL")
		}
		if !strings.Contains(err.Error(), "no SQL") {
			t.Errorf("the error is %q, want it to say there is no SQL in the answer", err)
		}
	})
}
