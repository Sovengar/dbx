package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Scenario: canonicalPath returns an absolute, symlink-free identity.
// A relative input must still be made absolute: identity is compared across
// projects, and a relative path would compare against anything that happens to
// share the same prefix.
func TestCanonicalPath_RelativeInputBecomesAbsolute(t *testing.T) {
	root := t.TempDir()
	// Created, so this exercises the symlink-resolving path, not the fallback
	// for a target that does not exist.
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", repo, err)
	}
	t.Chdir(root)

	got := canonicalPath("repo")
	if !filepath.IsAbs(got) {
		t.Errorf("canonicalPath(%q) = %q, want an absolute path", "repo", got)
	}
	// Compare against a canonicalized root: the result has been through
	// EvalSymlinks, so a symlinked temp root would otherwise never match.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", root, err)
	}
	if want := filepath.Join(resolvedRoot, "repo"); got != want {
		t.Errorf("canonicalPath(%q) = %q, want %q", "repo", got, want)
	}
}

// Scenario: canonicalPath falls back to the absolute path when the target does
// not exist. EvalSymlinks fails on a missing path, and the fallback must stay
// absolute instead of degrading to an empty/relative string.
func TestCanonicalPath_MissingPathFallsBackToAbsolute(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "does", "not", "exist")

	got := canonicalPath(missing)
	if got != missing {
		t.Errorf("canonicalPath(%q) = %q, want the absolute path %q", missing, got, missing)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("canonicalPath(%q) = %q, want an absolute path", missing, got)
	}
}

// Scenario: canonicalPath resolves symlinks, so two paths to the same directory
// share one identity and the scanner can collapse their projects.
func TestCanonicalPath_ResolvesSymlinkToSameIdentity(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mkGitRepo(t, real)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink(%q, %q): %v", real, link, err)
	}

	if a, b := canonicalPath(real), canonicalPath(link); a != b {
		t.Errorf("canonicalPath(%q) = %q and canonicalPath(%q) = %q, want the same identity", real, a, link, b)
	}
}
