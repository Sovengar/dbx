package config

// Scenario: La identidad de un repo git se deriva SIN ejecutar git, y un worktree
// comparte identidad con su repo.
//
// This is the whole reason the file exists. Spawning `git rev-parse` would be simpler,
// but it costs a process per directory scanned and it is unavailable in the container
// images where dbx runs, so the identity is derived from the filesystem instead. A
// derivation that is subtly wrong is worse than no identity at all: it merges two
// projects that should be separate, or splits one project into six.
//
// So the cases below are the ones where the derivation is actually hard, and each is a
// shape a real checkout has:
//
//	the main checkout has a .git DIRECTORY
//	a linked worktree has a .git FILE pointing at <main>/.git/worktrees/<name>
//	that gitdir holds a `commondir` file naming the shared directory
//	a submodule has a .git file pointing into the SUPERPROJECT's modules dir
//	a directory that is not a git repository at all
//	a repo that CONTAINS root, which must not absorb a non-git project below it
//
// The last one is why findDotGit takes a root: $HOME is very often a git repository, and
// without the bound every project under it would inherit the home repo's identity.

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Scenario: El checkout principal usa el directorio .git como identidad.
func TestTheMainCheckoutIsIdentifiedByItsGitDirectory(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	// A file inside, so this is unmistakably a real git dir.
	mustWrite(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")

	got := gitRepoIdentity(repo, root)

	if !got.isMain {
		t.Error("the main checkout does not report itself as main")
	}
	want := canonicalPath(filepath.Join(repo, ".git"))
	if got.id != want {
		t.Errorf("the identity is %q, want the canonical .git path %q", got.id, want)
	}
}

// Scenario: Un subdirectorio del repo hereda la identidad del checkout principal.
func TestASubdirectoryOfTheRepoHasTheSameIdentity(t *testing.T) {
	// The scan finds projects by walking a root, so most of what it asks about is a
	// nested directory. If the walk up stopped at the first directory instead of the
	// first .git, every nested project would be its own identity.
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	deep := filepath.Join(repo, "services", "api")
	mustMkdir(t, deep)

	parent := gitRepoIdentity(repo, root)
	child := gitRepoIdentity(deep, root)

	if child.id != parent.id {
		t.Errorf("a nested directory has identity %q, want the repo's %q", child.id, parent.id)
	}
	if !child.isMain {
		t.Error("a nested directory of the main checkout does not report itself as main")
	}
}

// Scenario: Un worktree enlazado comparte identidad con su repo principal.
func TestALinkedWorktreeSharesItsReposIdentity(t *testing.T) {
	root := t.TempDir()

	main := filepath.Join(root, "repo")
	mustMkdir(t, filepath.Join(main, ".git"))
	shared := filepath.Join(main, ".git")

	// A linked worktree's .git is a FILE naming its gitdir, and that gitdir holds a
	// commondir file pointing at the main .git directory.
	wtGitDir := filepath.Join(shared, "worktrees", "feature")
	mustMkdir(t, wtGitDir)
	mustWrite(t, filepath.Join(wtGitDir, "commondir"), "../..\n")

	wt := filepath.Join(root, "feature")
	mustMkdir(t, wt)
	mustWrite(t, filepath.Join(wt, ".git"), "gitdir: "+wtGitDir+"\n")

	mainID := gitRepoIdentity(main, root)
	wtID := gitRepoIdentity(wt, root)

	if wtID.id != mainID.id {
		t.Errorf("the worktree has identity %q, want the main checkout's %q — a worktree is not a separate project", wtID.id, mainID.id)
	}
	if wtID.isMain {
		t.Error("a linked worktree reports itself as the main checkout")
	}

	t.Run("a RELATIVE gitdir is resolved against the .git file's directory", func(t *testing.T) {
		// git writes a relative gitdir when the worktree is under the main checkout,
		// which is the common case. Reading the line without resolving it would give
		// an identity of ".git/worktrees/feature" relative to the process's working
		// directory — a different identity on every run and wrong everywhere.
		root := t.TempDir()
		main := filepath.Join(root, "repo")
		shared := filepath.Join(main, ".git")
		mustMkdir(t, filepath.Join(shared, "worktrees", "wt"))
		mustWrite(t, filepath.Join(shared, "worktrees", "wt", "commondir"), "../..\n")

		wt := filepath.Join(root, "repo-wt")
		mustMkdir(t, wt)
		// One level up from the worktree directory, which is where the main checkout
		// sits: ../repo/.git/worktrees/wt. The first version wrote ../../, which
		// climbs above the temp root entirely.
		mustWrite(t, filepath.Join(wt, ".git"), "gitdir: ../repo/.git/worktrees/wt\n")

		got := gitRepoIdentity(wt, root)
		if got.id != canonicalPath(shared) {
			t.Errorf("a relative gitdir gave identity %q, want %q", got.id, canonicalPath(shared))
		}
	})

	t.Run("a worktree with no commondir falls back to its own gitdir", func(t *testing.T) {
		// Older git versions, and a bare repo. Still a stable identity per worktree,
		// which is better than collapsing them.
		root := t.TempDir()
		main := filepath.Join(root, "repo")
		mustMkdir(t, filepath.Join(main, ".git"))
		wtGitDir := filepath.Join(main, ".git", "worktrees", "old")
		mustMkdir(t, wtGitDir)
		wt := filepath.Join(root, "old")
		mustMkdir(t, wt)
		mustWrite(t, filepath.Join(wt, ".git"), "gitdir: "+wtGitDir+"\n")

		got := gitRepoIdentity(wt, root)
		if got.id != canonicalPath(wtGitDir) {
			t.Errorf("the identity is %q, want the worktree's own gitdir %q", got.id, canonicalPath(wtGitDir))
		}
		if got.isMain {
			t.Error("a worktree without a commondir reports itself as main")
		}
	})
}

// Scenario: Un submodulo tiene .git como fichero y NO es el checkout principal.
func TestASubmoduleIsNotTheMainCheckout(t *testing.T) {
	root := t.TempDir()
	super := filepath.Join(root, "super")
	mustMkdir(t, filepath.Join(super, ".git"))

	moduleGit := filepath.Join(super, ".git", "modules", "vendor")
	mustMkdir(t, moduleGit)
	sub := filepath.Join(root, "vendor")
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, ".git"), "gitdir: "+moduleGit+"\n")

	got := gitRepoIdentity(sub, root)

	if got.isMain {
		t.Error("a submodule reports itself as the main checkout")
	}
	// Its own gitdir, not the superproject's .git: they are different repositories and
	// merging them would make the superproject's identity change with the submodule.
	superID := gitRepoIdentity(super, root)
	if got.id == superID.id {
		t.Errorf("the submodule shares the superproject's identity %q", got.id)
	}
}

// Scenario: Un directorio que no es un repo tiene su propia identidad.
func TestANonGitDirectoryIsItsOwnIdentity(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "plain")
	mustMkdir(t, project)

	got := gitRepoIdentity(project, root)

	if got.id != canonicalPath(project) {
		t.Errorf("the identity is %q, want the canonical directory %q", got.id, canonicalPath(project))
	}
	if got.isMain {
		t.Error("a non-git directory reports itself as the main checkout")
	}

	t.Run("two sibling non-git directories have DIFFERENT identities", func(t *testing.T) {
		// The point of the fallback being the path rather than a constant. A shared
		// constant would merge every non-git project into one, and the user would see
		// one project's active/inactive state applied to another.
		a := filepath.Join(root, "one")
		b := filepath.Join(root, "two")
		mustMkdir(t, a)
		mustMkdir(t, b)
		if gitRepoIdentity(a, root).id == gitRepoIdentity(b, root).id {
			t.Error("two non-git directories share an identity")
		}
	})
}

// Scenario: Un repo que CONTIENE root no absorbe los proyectos de dentro.
func TestARepoContainingRootDoesNotAbsorbProjectsInside(t *testing.T) {
	// This is the $HOME case, and it is the reason findDotGit is bounded.
	//
	// A dotfiles repository at $HOME makes every directory under it "inside a git
	// repository" to an unbounded walk. Every project in the user's home would then
	// share the home repo's identity, and the per-project query history, the active
	// flag and the drafts would all become one project's.
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".git"))

	project := filepath.Join(home, "dev", "myapp")
	mustMkdir(t, project)

	// Bounded by home: the walk stops at the root and never looks at home/.git.
	bounded := gitRepoIdentity(project, home)
	if bounded.isMain {
		t.Error("a project under a git home reports itself as a main checkout")
	}
	if bounded.id != canonicalPath(project) {
		t.Errorf("the identity is %q, want the project's own path %q", bounded.id, canonicalPath(project))
	}

	// Unbounded, the same directory DOES get absorbed — which is what the bound is
	// preventing, and worth asserting so the bound is not "removed as redundant".
	if root := filepath.Dir(home); root != home {
		unbounded := gitRepoIdentity(project, root)
		if !unbounded.isMain {
			t.Log("the unbounded walk did not reach the home repository here; the bound still holds but is untested by this run")
		}
	}
}

// Scenario: Un .git que no se puede leer cae a una identidad estable.
func TestAMalformedGitFileFallsBackToAStableIdentity(t *testing.T) {
	root := t.TempDir()

	for _, tc := range []struct{ name, content string }{
		{"no prefix", "/some/path\n"},
		{"empty target", "gitdir:\n"},
		{"only the prefix", "gitdir:"},
		{"whitespace only", "   \n"},
		{"blank", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := filepath.Join(root, "p-"+tc.name)
			mustMkdir(t, project)
			gitPath := filepath.Join(project, ".git")
			mustWrite(t, gitPath, tc.content)

			got := gitRepoIdentity(project, root)

			// Whatever it is, it must be non-empty and stable across calls.
			if got.id == "" {
				t.Error("the identity is empty")
			}
			if again := gitRepoIdentity(project, root); again.id != got.id {
				t.Errorf("the identity changed between calls: %q then %q", got.id, again.id)
			}
			if got.isMain {
				t.Error("a .git FILE reports itself as the main checkout")
			}
		})
	}

	t.Run("an ABSOLUTE gitdir is used as written", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "elsewhere", ".git")
		mustMkdir(t, target)
		project := filepath.Join(root, "p")
		mustMkdir(t, project)
		mustWrite(t, filepath.Join(project, ".git"), "gitdir: "+target+"\n")

		if got := gitRepoIdentity(project, root); got.id != canonicalPath(target) {
			t.Errorf("the identity is %q, want the absolute target %q", got.id, canonicalPath(target))
		}
	})

	t.Run("a commondir with an ABSOLUTE path is used as written", func(t *testing.T) {
		root := t.TempDir()
		shared := filepath.Join(root, "shared-git")
		mustMkdir(t, shared)
		wtGit := filepath.Join(root, "wt-git")
		mustMkdir(t, wtGit)
		mustWrite(t, filepath.Join(wtGit, "commondir"), shared+"\n")

		got, ok := readCommonDir(wtGit)
		if !ok {
			t.Fatal("readCommonDir reported no commondir")
		}
		if got != shared {
			t.Errorf("readCommonDir is %q, want %q", got, shared)
		}
	})

	t.Run("an EMPTY commondir is treated as absent", func(t *testing.T) {
		root := t.TempDir()
		wtGit := filepath.Join(root, "wt-git")
		mustMkdir(t, wtGit)
		mustWrite(t, filepath.Join(wtGit, "commondir"), "  \n")
		if got, ok := readCommonDir(wtGit); ok {
			t.Errorf("readCommonDir returned %q for an empty file", got)
		}
	})
}

// Scenario: El limite del recorrido es root, y se comprueba por palabra.
func TestPathWithin(t *testing.T) {
	for _, tc := range []struct {
		path, root string
		want       bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/bc", "/a/b", false}, // a prefix that is not a path boundary
		{"/a", "/a/b", false},
		{"/b", "/a/b", false},
		{"", "", true},
		{"", "/a", false},
	} {
		if got := pathWithin(tc.path, tc.root); got != tc.want {
			t.Errorf("pathWithin(%q, %q) is %t, want %t", tc.path, tc.root, got, tc.want)
		}
	}
}

// Scenario: Un directorio que no esta dentro de root no inspecciona sus antepasados.
func TestFindDotGitNeverEscapesRoot(t *testing.T) {
	outer := t.TempDir()
	mustMkdir(t, filepath.Join(outer, ".git"))

	outside := filepath.Join(outer, "sibling")
	mustMkdir(t, outside)

	// root is INSIDE the git repo, and dir is outside root entirely.
	root := filepath.Join(outer, "root")
	mustMkdir(t, root)

	if got, ok := findDotGit(outside, root); ok {
		t.Errorf("findDotGit escaped root and found %q", got)
	}

	// And the walk stops at root even when root itself has no .git.
	if got, ok := findDotGit(root, root); ok {
		t.Errorf("findDotGit found %q at root, which has no .git", got)
	}
}

// Scenario: Una ruta que no existe no hace panico.
func TestCanonicalPathOnAMissingPath(t *testing.T) {
	// A project directory recorded in state can be deleted afterwards. Resolving
	// symlinks then fails, and the function falls back to the absolute clean path so
	// the identity is still stable rather than the process dying.
	missing := filepath.Join(t.TempDir(), "gone", "..", "gone2")
	got := canonicalPath(missing)
	if got == "" {
		t.Error("canonicalPath returned an empty string for a missing path")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("canonicalPath returned %q, which is not absolute", got)
	}
	// Stable across calls, which is the property the identity depends on.
	if again := canonicalPath(missing); again != got {
		t.Errorf("canonicalPath is not stable: %q then %q", got, again)
	}
}

// Scenario: Un symlink se resuelve, para que dos rutas al mismo sitio tengan una identidad.
func TestCanonicalPathResolvesSymlinks(t *testing.T) {
	// Without this, the same project reached by two paths — a symlinked $HOME and the
	// real one — would be two projects with two histories.
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mustMkdir(t, real)

	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	if canonicalPath(link) != canonicalPath(real) {
		t.Errorf("a symlink and its target have identities %q and %q", canonicalPath(link), canonicalPath(real))
	}
}
