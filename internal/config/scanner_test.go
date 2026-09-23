package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- helpers -------------------------------------------------------------

// scannerFor builds a Scanner whose discovery seam returns the given paths,
// decoupling the tests from fd and from filesystem walk order.
func scannerFor(root string, paths ...string) *Scanner {
	s := NewScanner(root)
	injected := append([]string(nil), paths...)
	s.findFiles = func(string) []string { return injected }
	return s
}

// writeConnections writes a .dbx.toml at dir with the given connection
// name->DSN pairs.
func writeConnections(t *testing.T, dir string, conns map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}

	var b strings.Builder
	for name, dsn := range conns {
		fmt.Fprintf(&b, "[connections.%q]\ndriver = \"postgres\"\ndsn = %q\n\n", name, dsn)
	}
	path := filepath.Join(dir, ".dbx.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// writeNoConnectionsProject writes a .dbx.toml without any [connections].
func writeNoConnectionsProject(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	path := filepath.Join(dir, ".dbx.toml")
	if err := os.WriteFile(path, []byte("[other]\nkey = \"value\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func dbxPath(dir string) string { return filepath.Join(dir, ".dbx.toml") }

// mkGitRepo simulates a git repository: a directory containing a `.git` dir.
func mkGitRepo(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Join(repoDir, ".git"), err)
	}
}

// mkGitWorktree simulates a linked worktree: `.git` is a file pointing at
// <main>/.git/worktrees/<name>, which in turn holds a `commondir` file with the
// path (relative to the gitdir) of the shared git dir.
func mkGitWorktree(t *testing.T, mainRepo, wtDir, wtName string) {
	t.Helper()
	commonGitDir := filepath.Join(mainRepo, ".git")
	wtGitDir := filepath.Join(commonGitDir, "worktrees", wtName)
	if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", wtGitDir, err)
	}

	commonRel, err := filepath.Rel(wtGitDir, commonGitDir)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte(commonRel+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(commondir): %v", err)
	}

	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", wtDir, err)
	}
	gitRel, err := filepath.Rel(wtDir, wtGitDir)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: "+gitRel+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git): %v", err)
	}
}

// --- Problem 1: crash on files without connections -----------------------

func TestScanner_NoConnectionsDoesNotPanicAndSkips(t *testing.T) {
	root := t.TempDir()
	emptyDir := filepath.Join(root, "empty")
	validDir := filepath.Join(root, "valid")
	writeNoConnectionsProject(t, emptyDir)
	writeConnections(t, validDir, map[string]string{"main": "postgres://localhost/db"})

	s := scannerFor(root, dbxPath(emptyDir), dbxPath(validDir))
	got := s.Scan()

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Name != "main" {
		t.Fatalf("name = %q, want main", got[0].Name)
	}
	if got[0].Path != validDir {
		t.Fatalf("path = %q, want %q", got[0].Path, validDir)
	}
}

func TestScanner_OnlyNoConnectionsIsEmpty(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeNoConnectionsProject(t, a)
	writeNoConnectionsProject(t, b)

	s := scannerFor(root, dbxPath(a), dbxPath(b))
	if got := s.Scan(); len(got) != 0 {
		t.Fatalf("len = %d, want 0 (%+v)", len(got), got)
	}
}

// --- Problem 2: deterministic connection selection -----------------------

func TestScanner_MultipleConnectionsPicksLexicographicallyFirst(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	writeConnections(t, dir, map[string]string{
		"zeta":  "postgres://localhost/z",
		"alpha": "postgres://localhost/a",
	})

	s := scannerFor(root, dbxPath(dir))
	for i := 0; i < 5; i++ {
		got := s.Scan()
		if len(got) != 1 {
			t.Fatalf("scan %d: len = %d, want 1", i, len(got))
		}
		if got[0].Name != "alpha" {
			t.Fatalf("scan %d: name = %q, want alpha", i, got[0].Name)
		}
	}
}

// --- Problem 3: deduplication --------------------------------------------

func TestScanner_DedupSameRepoAndConnection(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	// Arbitrary discovery order must not affect the outcome.
	s := scannerFor(root, dbxPath(b), dbxPath(a))
	got := s.Scan()

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Path != a {
		t.Fatalf("survivor path = %q, want shortest %q", got[0].Path, a)
	}
}

func TestScanner_SameRepoDifferentDSNKept(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/a"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/b"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestScanner_DifferentConnectionNameKept(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnections(t, a, map[string]string{"one": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"two": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestScanner_WorktreeCollapsesToMainRepo(t *testing.T) {
	root := t.TempDir()
	mainRepo := filepath.Join(root, "repo")
	mkGitRepo(t, mainRepo)
	wt := filepath.Join(root, "worktree")
	mkGitWorktree(t, mainRepo, wt, "wt1")

	// The main repo's project lives at the repo root (shortest path).
	writeConnections(t, mainRepo, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, wt, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(wt), dbxPath(mainRepo)).Scan()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Path != mainRepo {
		t.Fatalf("survivor = %q, want main repo %q", got[0].Path, mainRepo)
	}
}

func TestScanner_NonGitDoesNotDeduplicate(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("non-git dirs must not collapse: len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestScanner_DSNComparedAfterEnvExpansion(t *testing.T) {
	t.Setenv("DBX_DSN", "postgres://env/db")

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnections(t, a, map[string]string{"main": "${env:DBX_DSN}"})
	writeConnections(t, b, map[string]string{"main": "postgres://env/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 1 {
		t.Fatalf("resolved DSNs are equal, len = %d, want 1 (%+v)", len(got), got)
	}
}

// --- Determinism of output ------------------------------------------------

func TestScanner_OutputOrderIsStable(t *testing.T) {
	root := t.TempDir()
	var dirs []string
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		dir := filepath.Join(root, name)
		writeConnections(t, dir, map[string]string{"main": "postgres://localhost/" + name})
		dirs = append(dirs, dir)
	}

	// Two scans with discovery in different orders must agree.
	first := scannerFor(root, dbxPath(dirs[0]), dbxPath(dirs[1]), dbxPath(dirs[2])).Scan()
	second := scannerFor(root, dbxPath(dirs[2]), dbxPath(dirs[0]), dbxPath(dirs[1])).Scan()

	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("lens = %d,%d want 3,3", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Fatalf("order differs at %d: %q vs %q", i, first[i].Path, second[i].Path)
		}
	}

	want := append([]string(nil), dirs...)
	sort.Strings(want)
	for i, dir := range want {
		if first[i].Path != dir {
			t.Fatalf("position %d = %q, want %q (sorted)", i, first[i].Path, dir)
		}
	}
}

// --- Active/inactive state ------------------------------------------------

func TestScanner_StateAppliesAfterDedup(t *testing.T) {
	// Isolate the state file (~/.local/state/dbx/...) inside a temp HOME.
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	s := scannerFor(root, dbxPath(a), dbxPath(b))
	got := s.Scan()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if !got[0].Active {
		t.Fatal("survivor should start active")
	}
	survivor := got[0].Path

	state, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}
	state.SetInactive(survivor)
	if err := state.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got = s.Scan()
	if len(got) != 1 {
		t.Fatalf("after toggle: len = %d, want 1", len(got))
	}
	if got[0].Active {
		t.Fatalf("survivor %q should be inactive after toggle", got[0].Path)
	}
}
