package bordered

import (
	"strings"
	"testing"
)

func TestWrapLinePreservesStyles(t *testing.T) {
	green := "\033[32m"
	reset := "\033[0m"
	styled := green + "hello world foo bar baz" + reset

	chunks := wrapLine(styled, 10)

	for i, c := range chunks {
		t.Logf("chunk %d: %q", i, c)
	}

	for i, c := range chunks {
		if !strings.HasPrefix(c, green) {
			t.Errorf("chunk %d missing style prefix: %q", i, c)
		}
		if !strings.HasSuffix(c, reset) {
			t.Errorf("chunk %d missing reset suffix: %q", i, c)
		}
	}
}

func TestWrapLinePlainText(t *testing.T) {
	chunks := wrapLine("hello world foo bar", 10)
	for i, c := range chunks {
		t.Logf("chunk %d: %q", i, c)
	}
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
	// Plain text should not have ANSI codes
	for i, c := range chunks {
		if strings.Contains(c, "\033") {
			t.Errorf("chunk %d has unexpected ANSI codes: %q", i, c)
		}
	}
}

func TestWrapLineMultiStyle(t *testing.T) {
	green := "\033[32m"
	red := "\033[31m"
	reset := "\033[0m"
	// green "hello world" then red "foo bar baz"
	styled := green + "hello world" + reset + red + " foo bar baz" + reset

	chunks := wrapLine(styled, 10)

	for i, c := range chunks {
		t.Logf("chunk %d: %q", i, c)
	}

	// First chunk should be green, second should be red
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], green) {
		t.Errorf("chunk 0 should start with green: %q", chunks[0])
	}
	// Second chunk starts with "d " from "world" + " foo bar" — still green
	// Find where red starts
	for i, c := range chunks {
		if strings.Contains(c, "foo") || strings.Contains(c, "bar") {
			if !strings.HasPrefix(c, red) {
				t.Errorf("chunk %d with 'foo'/'bar' should start with red: %q", i, c)
			}
			break
		}
	}
}
