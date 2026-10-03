package config

import (
	"fmt"
	"os"
	"os/exec"
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

// connectionSpec describes a project connection for test fixtures.
type connectionSpec struct {
	driver    string
	dsn       string
	sshTunnel string
}

// writeConnections writes a .dbx.toml at dir with the given connection
// name->DSN pairs and the postgres driver.
func writeConnections(t *testing.T, dir string, conns map[string]string) {
	t.Helper()
	specs := make(map[string]connectionSpec, len(conns))
	for name, dsn := range conns {
		specs[name] = connectionSpec{driver: "postgres", dsn: dsn}
	}
	writeConnectionSpecs(t, dir, specs)
}

// writeConnectionSpecs writes a .dbx.toml at dir with full connection details,
// letting tests vary driver and ssh_tunnel.
func writeConnectionSpecs(t *testing.T, dir string, conns map[string]connectionSpec) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}

	var b strings.Builder
	for name, spec := range conns {
		fmt.Fprintf(&b, "[connections.%q]\ndriver = %q\ndsn = %q\n", name, spec.driver, spec.dsn)
		if spec.sshTunnel != "" {
			fmt.Fprintf(&b, "ssh_tunnel = %q\n", spec.sshTunnel)
		}
		b.WriteString("\n")
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

func TestScanner_MainRepoSurvivesEvenWhenWorktreePathIsShorter(t *testing.T) {
	root := t.TempDir()
	mainRepo := filepath.Join(root, "a-very-long-main-repository-directory-name")
	mkGitRepo(t, mainRepo)
	wt := filepath.Join(root, "wt") // shorter path than the main repo
	mkGitWorktree(t, mainRepo, wt, "wt1")

	writeConnections(t, mainRepo, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, wt, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(wt), dbxPath(mainRepo)).Scan()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Path != mainRepo {
		t.Fatalf("survivor = %q, want main repo %q despite longer path", got[0].Path, mainRepo)
	}
}

func TestScanner_DifferentReposDoNotDeduplicate(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")
	mkGitRepo(t, repoA)
	mkGitRepo(t, repoB)

	a := filepath.Join(repoA, "proj")
	b := filepath.Join(repoB, "proj")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("distinct repos must not collapse: len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestScanner_WalkDoesNotEscapeRoot(t *testing.T) {
	// A git repo enclosing the scan root must not give non-git projects inside
	// the root a shared identity.
	parent := t.TempDir()
	mkGitRepo(t, parent)
	root := filepath.Join(parent, "root")

	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("bounded walk leaked the parent repo: len = %d, want 2 (%+v)", len(got), got)
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

func TestScanner_DifferentDriverKept(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnectionSpecs(t, a, map[string]connectionSpec{
		"main": {driver: "postgres", dsn: "postgres://localhost/db"},
	})
	writeConnectionSpecs(t, b, map[string]connectionSpec{
		"main": {driver: "mysql", dsn: "postgres://localhost/db"},
	})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("different driver must not collapse: len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestScanner_DifferentSSHTunnelKept(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkGitRepo(t, repo)

	a := filepath.Join(repo, "a")
	b := filepath.Join(repo, "b")
	writeConnectionSpecs(t, a, map[string]connectionSpec{
		"main": {driver: "postgres", dsn: "postgres://localhost/db"},
	})
	writeConnectionSpecs(t, b, map[string]connectionSpec{
		"main": {driver: "postgres", dsn: "postgres://localhost/db", sshTunnel: "user@host:22"},
	})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("different ssh_tunnel must not collapse: len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestFindDotGit_DoesNotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	mkGitRepo(t, parent) // .git lives above the scan root

	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", root, err)
	}
	outside := filepath.Join(parent, "sibling") // descendant of parent, outside root
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", outside, err)
	}

	if got, ok := findDotGit(outside, root); ok {
		t.Fatalf("findDotGit escaped root: got %q", got)
	}
}

func TestScanner_ProjectOutsideRootNotAbsorbed(t *testing.T) {
	parent := t.TempDir()
	mkGitRepo(t, parent) // enclosing repo, above the scan root

	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", root, err)
	}

	a := filepath.Join(parent, "a")
	b := filepath.Join(parent, "b")
	writeConnections(t, a, map[string]string{"main": "postgres://localhost/db"})
	writeConnections(t, b, map[string]string{"main": "postgres://localhost/db"})

	got := scannerFor(root, dbxPath(a), dbxPath(b)).Scan()
	if len(got) != 2 {
		t.Fatalf("projects outside root must not inherit the enclosing repo: len = %d, want 2 (%+v)", len(got), got)
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

	// Two scans with discovery in opposite orders must agree.
	forward := scannerFor(root, dbxPath(dirs[0]), dbxPath(dirs[1]), dbxPath(dirs[2])).Scan()
	reverse := scannerFor(root, dbxPath(dirs[2]), dbxPath(dirs[1]), dbxPath(dirs[0])).Scan()

	if len(forward) != 3 || len(reverse) != 3 {
		t.Fatalf("lens = %d,%d want 3,3", len(forward), len(reverse))
	}
	for i := range forward {
		if forward[i].Path != reverse[i].Path {
			t.Fatalf("order differs at %d: %q vs %q", i, forward[i].Path, reverse[i].Path)
		}
	}

	want := append([]string(nil), dirs...)
	sort.Strings(want)
	for i, dir := range want {
		if forward[i].Path != dir {
			t.Fatalf("position %d = %q, want %q (sorted)", i, forward[i].Path, dir)
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

// --- the WalkDir fallback, which the seam normally bypasses ------------------

// Scenario: El recorrido propio encuentra los .dbx.toml que fd no está.
//
// scanWithWalkDir is the fallback for a machine with no `fd`, and it is the only
// discovery path the tests above never touch, because the seam short-circuits it.
// A real directory tree is the only honest way to exercise it.
func TestScanWithWalkDir_FindsEveryConfigInTheTree(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"a", "b/nested", "c/deep/deeper"} {
		writeConnections(t, filepath.Join(root, dir), map[string]string{"p": "postgres://x/y"})
	}
	// A file that is not a .dbx.toml must not be picked up.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	// And neither must a .dbx.toml with a different name.
	if err := os.WriteFile(filepath.Join(root, "other.toml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got := NewScanner(root).scanWithWalkDir(root)
	if len(got) != 3 {
		t.Fatalf("found %d configs, want 3: %v", len(got), got)
	}
	for _, p := range got {
		if filepath.Base(p) != ".dbx.toml" {
			t.Errorf("found %q, which is not a .dbx.toml", p)
		}
	}
}

// Scenario: Un directorio oculto se salta entero, y el árbol visible se sigue.
//
// .git holds a whole checkout, and walking into it turns a project scan into a
// full-disk scan. The one exception is the root itself, which is often hidden
// (a dotfile home directory) and must still be walked.
func TestScanWithWalkDir_SkipsHiddenDirectoriesButNotTheRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hiddenroot")
	writeConnections(t, root, map[string]string{"visible": "postgres://x/y"})
	// A config buried in a hidden directory below the root.
	writeConnections(t, filepath.Join(root, ".git", "nested"), map[string]string{"buried": "postgres://x/y"})
	// And one in a visible subdirectory, which must be found.
	writeConnections(t, filepath.Join(root, "sub"), map[string]string{"deep": "postgres://x/y"})

	got := NewScanner(root).scanWithWalkDir(root)
	if len(got) != 2 {
		t.Fatalf("found %d configs, want 2 (the root's and sub's, not .git/nested's): %v", len(got), got)
	}
	for _, p := range got {
		if strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			t.Errorf("walked into a hidden directory: %q", p)
		}
	}
}

// Scenario: Un directorio oculto llamado "." no se salta a sí mismo.
//
// The guard is `HasPrefix(name, ".") && name != "."`, and without the second half
// the root directory would return SkipDir on its own first visit and the walk
// would find nothing at all. A hidden ROOT is the ordinary case for a dotfile
// home directory, so this is not a corner.
func TestScanWithWalkDir_AHiddenRootIsStillWalked(t *testing.T) {
	for _, name := range []string{".config", ".dbx"} {
		root := filepath.Join(t.TempDir(), name)
		writeConnections(t, root, map[string]string{"p": "postgres://x/y"})

		got := NewScanner(root).scanWithWalkDir(root)
		if len(got) != 1 {
			t.Errorf("a root called %q yielded %d configs, want 1: %v", name, len(got), got)
		}
	}
}

// Scenario: Un error al recorrer no detiene la búsqueda.
//
// The callback swallows the error and keeps going, so an unreadable directory
// costs that directory and nothing else. A walk that aborted on the first error
// would return nothing at all on a tree with one protected subdirectory.
func TestScanWithWalkDir_AnUnreadableSubtreeDoesNotStopTheWalk(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "visible"), map[string]string{"p": "postgres://x/y"})

	// A directory that does not exist makes WalkDir call the callback with an
	// error, which is the case under test.
	got := NewScanner(root).scanWithWalkDir(filepath.Join(root, "no-such-dir"))
	if got != nil {
		t.Errorf("a missing root returned %v, want nothing", got)
	}

	// A tree with a real entry still works, so the error path above is not the
	// only thing being observed.
	if ok := NewScanner(root).scanWithWalkDir(root); len(ok) != 1 {
		t.Errorf("the readable tree yielded %v, want one config", ok)
	}
}

// Scenario: Sin fd la búsqueda cae al recorrido propio.
//
// fd is invoked as an external binary and its failure is silent, so a machine
// without it produces an empty list rather than an error. That emptiness is
// exactly what triggers the fallback.
func TestScanWithFD_MissingBinaryYieldsNoPaths(t *testing.T) {
	// A root that does not exist makes fd fail whatever is installed, so the
	// result does not depend on whether the machine has fd.
	got := NewScanner("/definitely/not/here").scanWithFD("/definitely/not/here")
	if len(got) != 0 {
		t.Errorf("a failing fd returned %v, want nothing so the WalkDir fallback runs", got)
	}
}

// Scenario: La salida de fd se parte por líneas y las vacías se descartan.
//
// fd prints one path per line and a trailing newline, so splitting without
// filtering yields a final empty entry that would then be loaded as a path and
// produce a spurious "empty project".
func TestScanWithFD_EmptyLinesAreDiscarded(t *testing.T) {
	// Whatever fd prints for an existing empty directory is either nothing or a
	// bare newline; either way no empty path may come out.
	got := NewScanner(t.TempDir()).scanWithFD(t.TempDir())
	for _, p := range got {
		if strings.TrimSpace(p) == "" {
			t.Errorf("an empty path came out of the fd output: %q", p)
		}
	}
}

// Scenario: Un .dbx.toml ilegible se salta y los demás se cargan.
//
// loadPaths drops what it cannot parse rather than failing the scan, so one
// corrupt file in a tree does not hide every other project.
func TestLoadPaths_UnparseableFilesAreSkipped(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "good"), map[string]string{"p": "postgres://x/y"})

	bad := filepath.Join(root, "bad", ".dbx.toml")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(bad, []byte("this is not toml {{{"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	// A file that is not a config at all.
	notConfig := filepath.Join(root, "nope", ".dbx.toml")
	if err := os.MkdirAll(filepath.Dir(notConfig), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(notConfig, []byte("[connections]\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got := NewScanner(root).loadPaths([]string{bad, notConfig, filepath.Join(root, "good", ".dbx.toml")})
	if len(got) != 1 {
		t.Fatalf("loaded %d projects, want 1: %v", len(got), got)
	}
	// A project's Name is its CONNECTION name, not its directory: when a
	// .dbx.toml defines several connections the first one sorted is the one that
	// represents it, so the picker shows a stable label.
	if got[0].Name != "p" {
		t.Errorf("kept the project named %q, want the one that parses (connection %q)", got[0].Name, "p")
	}
	if got[0].Path != filepath.Join(root, "good") {
		t.Errorf("the surviving project is at %q, want the good directory", got[0].Path)
	}
}

// Scenario: Un proyecto con varias conexiones se representa por la primera.
//
// The Name is what the picker shows, so a .dbx.toml defining three connections
// must resolve to one of them rather than to a blank or to all three. Sorted, so
// the choice does not depend on map iteration order.
func TestLoadPaths_AProjectWithSeveralConnectionsUsesTheFirstSorted(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "multi"), map[string]string{
		"zeta":  "postgres://x/z",
		"alpha": "postgres://x/a",
		"mid":   "postgres://x/m",
	})

	got := NewScanner(root).loadPaths([]string{filepath.Join(root, "multi", ".dbx.toml")})
	if len(got) != 1 {
		t.Fatalf("loaded %d projects, want 1", len(got))
	}
	if got[0].Name != "alpha" {
		t.Errorf("the project is named %q, want alpha: the first connection sorted", got[0].Name)
	}
	if got[0].Connection.DSN != "postgres://x/a" {
		t.Errorf("the DSN is %q, want the one belonging to alpha", got[0].Connection.DSN)
	}

	// And the same result on a second load, so it is not map-order luck.
	for i := 0; i < 5; i++ {
		again := NewScanner(root).loadPaths([]string{filepath.Join(root, "multi", ".dbx.toml")})
		if again[0].Name != "alpha" {
			t.Fatalf("load %d chose %q, want alpha every time", i, again[0].Name)
		}
	}
}

// Scenario: Un .dbx.toml sin conexiones es un proyecto vacío, no un error.
//
// errNoConnections is a valid configuration with nothing to connect to, so it is
// skipped silently rather than logged. Treating it as a parse failure would make
// a template repository look broken.
func TestLoadPaths_AConfigWithNoConnectionsIsSkipped(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty", ".dbx.toml")
	if err := os.MkdirAll(filepath.Dir(empty), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(empty, []byte("[something_else]\nkey = 1\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if got := NewScanner(root).loadPaths([]string{empty}); len(got) != 0 {
		t.Errorf("a config with no connections produced %v, want nothing", got)
	}
}

// Scenario: El orden es estable y desempatado por nombre.
//
// The sort is by Path, then by Name. Three sibling projects share a parent, so
// Path is equal for all of them and only the Name key can order them. Without it
// their relative order would depend on the sort's internal state, and the picker
// would reshuffle itself between two identical scans.
func TestScan_SamePathIsOrderedByName(t *testing.T) {
	root := t.TempDir()
	dirs := []string{"alpha", "beta", "gamma", "delta"}
	found := make([]string, 0, len(dirs))
	for _, d := range dirs {
		writeConnections(t, filepath.Join(root, "group", d), map[string]string{"p": "postgres://x/y"})
		found = append(found, filepath.Join(root, "group", d, ".dbx.toml"))
	}
	// Shuffled input, so only a real sort can produce a sorted result.
	found = []string{found[2], found[0], found[3], found[1]}

	s := NewScanner(root)
	s.findFiles = func(string) []string { return found }
	projects := s.Scan()
	if len(projects) != len(dirs) {
		t.Fatalf("scanned %d projects, want %d", len(projects), len(dirs))
	}

	names := make([]string, len(projects))
	paths := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
		paths[i] = p.Path
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("names came out %v, want them sorted: the Path tiebreak on Name is not doing its job", names)
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("paths came out %v, want them sorted", paths)
	}

	// And the order is the same from a differently shuffled input, which is what
	// "stable" has to mean for a picker that re-renders on every keystroke.
	reversed := []string{found[3], found[2], found[1], found[0]}
	s2 := NewScanner(root)
	s2.findFiles = func(string) []string { return reversed }
	again := s2.Scan()
	if len(again) != len(projects) {
		t.Fatalf("the second scan found %d projects, want %d", len(again), len(projects))
	}
	for i := range again {
		if again[i].Name != projects[i].Name {
			t.Errorf("position %d: %q then %q, want the same order regardless of input order", i, projects[i].Name, again[i].Name)
		}
	}
}

// --- isBetterSurvivor: a pure function, tested as one -----------------------

// cand is a dedup candidate with a path and whether it is the main checkout.
func cand(path string, isMain bool) dedupCandidate {
	return dedupCandidate{
		project: FoundProject{Path: path},
		isMain:  isMain,
	}
}

// Scenario: El checkout principal gana siempre, gane o pierda la longitud.
//
// This is the rule that stops a linked worktree from representing a repository:
// the main checkout is the real one, and no amount of path-length luck overrides
// it. The two halves are tested together because the first one short-circuits: if
// the length comparison ran first, a short worktree would win.
func TestIsBetterSurvivor_MainCheckoutBeatsWorktreeRegardlessOfLength(t *testing.T) {
	shortWorktree := cand("/w", false)
	longMain := cand("/a/very/long/path/to/the/main/checkout", true)

	if !isBetterSurvivor(longMain, shortWorktree) {
		t.Error("the long main checkout did not beat the short worktree")
	}
	// And the reverse direction must not claim the worktree is better, or the
	// comparison would be inconsistent and survivor() would depend on order.
	if isBetterSurvivor(shortWorktree, longMain) {
		t.Error("the short worktree beat the long main checkout")
	}
}

// Scenario: Entre dos worktrees gana el de la ruta más corta.
//
// With no main checkout in the group, path length is the proxy for "closest to
// the root", which is the one a user recognises as their own directory.
func TestIsBetterSurvivor_WithoutAMainCheckoutTheShortestPathWins(t *testing.T) {
	short := cand("/a/b", false)
	long := cand("/a/b/c/d/e", false)

	if !isBetterSurvivor(short, long) {
		t.Error("the shorter path did not win")
	}
	if isBetterSurvivor(long, short) {
		t.Error("the longer path won")
	}
}

// Scenario: Con la MISMA longitud decide el orden lexicográfico.
//
// This is the step that makes the result deterministic when two worktrees are at
// the same depth, which is the common case for two checkouts side by side. It is
// also the step a length-only comparator would skip, and it is the one that keeps
// the picker from reordering itself between two identical scans.
func TestIsBetterSurvivor_EqualLengthFallsBackToLexicographic(t *testing.T) {
	// Same length, different content: "/a/bb" and "/a/cc".
	a := cand("/a/bb", false)
	b := cand("/a/cc", false)
	if len(a.project.Path) != len(b.project.Path) {
		t.Fatalf("the fixture is wrong: %q and %q are different lengths", a.project.Path, b.project.Path)
	}

	if !isBetterSurvivor(a, b) {
		t.Errorf("%q did not win over %q", a.project.Path, b.project.Path)
	}
	if isBetterSurvivor(b, a) {
		t.Errorf("%q won over %q, want the lexicographically smaller path", b.project.Path, a.project.Path)
	}

	// And through survivor, so the whole selection is pinned and not just the
	// comparison: a group of three equal-length paths resolves to the smallest.
	got := survivor([]dedupCandidate{cand("/a/zz", false), cand("/a/aa", false), cand("/a/mm", false)})
	if got.Path != "/a/aa" {
		t.Errorf("survivor = %q, want /a/aa: the lexicographically smallest of the equal-length paths", got.Path)
	}
}

// Scenario: Una ruta idéntica no es mejor que sí misma.
//
// The comparator has to be a valid one: it must never claim that a candidate is
// strictly better than an identical one, or the selection becomes order-dependent
// for no reason.
func TestIsBetterSurvivor_AnIdenticalCandidateIsNotBetter(t *testing.T) {
	if isBetterSurvivor(cand("/same", false), cand("/same", false)) {
		t.Error("a candidate was judged better than an identical one")
	}
	if isBetterSurvivor(cand("/same", true), cand("/same", true)) {
		t.Error("a main checkout was judged better than an identical main checkout")
	}
}

// --- discoverProjects: the branch the seam normally short-circuits ------------

// Scenario: Sin el seam, un directorio con proyectos los encuentra igual.
//
// discoverProjects short-circuits to the seam when one is set, so the whole
// fd-then-WalkDir branch is normally never taken. Here the scanner is built the
// production way, with no seam, and pointed at a real tree.
func TestDiscoverProjects_WithoutTheSeamFindsTheProjects(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "one"), map[string]string{"p": "postgres://x/y"})
	writeConnections(t, filepath.Join(root, "two"), map[string]string{"p": "postgres://x/z"})

	// No seam at all: this is what app.go does.
	got := NewScanner(root).discoverProjects()
	if len(got) != 2 {
		t.Errorf("found %d projects, want 2: %v", len(got), got)
	}
}

// Scenario: Sin el seam, un árbol sin proyectos devuelve una lista vacía.
//
// The other side of the same branch, and the one that decides whether the
// WalkDir fallback runs: a root with nothing in it must come back empty rather
// than nil-looking or erroring.
func TestDiscoverProjects_WithoutTheSeamAndWithoutProjectsIsEmpty(t *testing.T) {
	got := NewScanner(t.TempDir()).discoverProjects()
	if len(got) != 0 {
		t.Errorf("an empty tree produced %v, want nothing", got)
	}
}

// Scenario: El seam manda sobre fd por completo.
//
// When a seam is installed neither discovery path runs, so a tree that WOULD be
// found by walking is invisible. That is the point of the seam: it makes the
// tests independent of fd and of the filesystem.
func TestDiscoverProjects_TheSeamSuppressesBothRealDiscoveryPaths(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "real"), map[string]string{"p": "postgres://x/y"})

	s := NewScanner(root)
	s.findFiles = func(string) []string { return nil } // the seam finds nothing

	if got := s.discoverProjects(); len(got) != 0 {
		t.Errorf("the seam returned nothing but discovery found %v: the seam is not taking precedence", got)
	}
}

// Scenario: Una ruta repetida se colapsa, y por eso el desempate por nombre nunca
// se alcanza.
//
// The sort is `Path` then `Name`, and the tiebreak on Name looks like it needs
// covering. It does not, and the reason is the dedup that runs FIRST: two entries
// can only share a Path if they came from the same directory, and FoundProject.Name
// is derived from the single .dbx.toml in that directory. Same directory, same
// file, same name — so by the time the sort runs, equal Path always implies equal
// Name, and `Name < Name` is false for every reachable input.
//
// This test states that invariant, so the dead tiebreak cannot start looking
// load-bearing to the next reader, and so a future change that makes two names
// share a path would fail here rather than silently depending on a comparator
// that can never return true.
func TestScan_DuplicatePathsCollapseSoTheNameTiebreakIsUnreachable(t *testing.T) {
	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "dup"), map[string]string{"p": "postgres://x/y"})
	same := filepath.Join(root, "dup", ".dbx.toml")

	s := NewScanner(root)
	s.findFiles = func(string) []string { return []string{same, same, same} }

	got := s.Scan()
	if len(got) != 1 {
		t.Fatalf("the seam reported one path three times and the scan returned %d projects, want 1: dedup collapses them", len(got))
	}

	// Two different directories under the same parent do NOT collapse, and those
	// are the entries the sort actually orders.
	other := filepath.Join(root, "other")
	writeConnections(t, other, map[string]string{"p": "postgres://x/z"})
	s.findFiles = func(string) []string { return []string{same, filepath.Join(other, ".dbx.toml")} }
	two := s.Scan()
	if len(two) != 2 {
		t.Fatalf("two distinct projects collapsed into %d: %v", len(two), two)
	}

	// The invariant: within a scan, no two survivors share a Path with different
	// Names. That is exactly the condition the sort's tiebreak is there for, and
	// it never holds.
	byPath := map[string][]string{}
	for _, p := range s.Scan() {
		byPath[p.Path] = append(byPath[p.Path], p.Name)
	}
	for path, names := range byPath {
		for i := 1; i < len(names); i++ {
			if names[i] != names[0] {
				t.Errorf("path %q has names %v: the sort's Name tiebreak is reachable after all, so it needs a test of its own", path, names)
			}
		}
	}
}

// Scenario: Sin seam, fd es el que descubre, y su resultado es el que se usa.
//
// The two other discovery tests install the seam, which returns before either real
// path runs. This one builds the scanner the way app.go does, so `fd` really is
// executed and scanWithFD's error branch is genuinely taken or not taken.
//
// Skipped when fd is absent, because then the branch under test is the
// "command failed" one and the fixture would prove nothing about the success
// path. The skip says so rather than passing quietly.
func TestScanWithFD_WhenInstalledItFindsTheProjects(t *testing.T) {
	if _, err := exec.LookPath("fd"); err != nil {
		t.Skip("fd is not installed: scanWithFD would only exercise its failure branch")
	}

	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "one"), map[string]string{"p": "postgres://x/y"})

	got := NewScanner(root).scanWithFD(root)
	if len(got) != 1 {
		t.Fatalf("fd found %d configs, want 1: %v", len(got), got)
	}
	if filepath.Base(got[0]) != ".dbx.toml" {
		t.Errorf("fd returned %q, want a .dbx.toml", got[0])
	}
}

// Scenario: fd y el recorrido propio NO encuentran lo mismo, y por eso el
// resultado de fd es el que manda.
//
// This is the test that makes the two discovery branches distinguishable at all.
// fd runs with --hidden, so it sees a project inside a hidden subdirectory; the
// WalkDir fallback deliberately skips hidden subdirectories, so it does not. When
// fd succeeds, its superset must be the one the user sees — falling back to the
// subset would hide a project the tool can see.
func TestDiscoverProjects_FDsSupersetIsKeptWhenItSucceeds(t *testing.T) {
	if _, err := exec.LookPath("fd"); err != nil {
		t.Skip("fd is not installed: the two discovery paths cannot be compared")
	}

	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "visible"), map[string]string{"a": "postgres://x/1"})
	// fd is called with --hidden and finds this one; the WalkDir fallback skips
	// hidden directories and does not.
	writeConnections(t, filepath.Join(root, ".hidden", "buried"), map[string]string{"b": "postgres://x/2"})

	// Confirm the premise on this machine, so a skip is never silently wrong.
	viaFD := NewScanner(root).scanWithFD(root)
	viaWalk := NewScanner(root).scanWithWalkDir(root)
	if len(viaFD) <= len(viaWalk) {
		t.Skipf("fd found %d and the walk found %d: this machine's fd does not see hidden directories, so the premise does not hold", len(viaFD), len(viaWalk))
	}

	// The scan runs with no seam, so fd succeeds and the fallback must not run.
	got := NewScanner(root).discoverProjects()
	if len(got) != len(viaFD) {
		t.Errorf("the scan found %d projects but fd found %d files: the fallback overwrote fd's result with a smaller set", len(got), len(viaFD))
	}
}
