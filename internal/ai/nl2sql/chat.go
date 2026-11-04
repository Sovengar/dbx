package nl2sql

import (
	"context"
	"fmt"
	"time"

	"github.com/sovengar/agentbridge"
)

// chat sends one prompt to an OpenAI-compatible endpoint through the shared
// agentbridge invoker and validates the SQL it answered with.
//
// The request shape, the response parsing, the HTTP client and the error
// vocabulary are not dbx's: four providers used to carry a private copy of
// that ladder, and every fix had to be applied four times. What stays local is
// the provider's identity (endpoint, key, extra headers) and the SQL contract
// (the system prompt and requireSQL).
func chat(ctx context.Context, name string, inv agentbridge.APIInvoker, headers map[string]string, prompt, schema string) (string, error) {
	inv.System = BuildSystemPrompt(schema)
	if len(headers) > 0 {
		inv.Headers = headers
	}
	res, err := inv.Invoke(ctx, agentbridge.Request{Prompt: prompt})
	if err != nil {
		// The provider name is what turns "empty response" into an answer the
		// user can act on; the shared invoker cannot know which vendor it was.
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return requireSQL(name, res.Output)
}

// opencodeHeaders are the headers OpenCode Go requires: it rejects a request
// that carries no session id.
func opencodeHeaders() map[string]string {
	return map[string]string{"x-opencode-session": fmt.Sprintf("dbx-%d", time.Now().UnixNano())}
}
