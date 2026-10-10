package cli

import (
	"testing"
)

// The pipe command was silently dropped from the root in a refactor while the
// docs, the tests of its handler and the installed binary's help kept telling
// different stories; this pins the registration itself, not the handler.
func TestPipeCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"pipe"})
	if err != nil {
		t.Fatalf("rootCmd.Find(pipe): %v", err)
	}
	if cmd == nil || cmd.Name() != "pipe" {
		t.Fatalf("pipe command not registered, got %v", cmd)
	}
	for _, flag := range []string{"connection", "json"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("pipe command lost its --%s flag", flag)
		}
	}
}
