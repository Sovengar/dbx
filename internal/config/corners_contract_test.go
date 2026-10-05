package config

// Scenario: Las esquinas del registro de keybinds y del estado de proyectos.
//
// These are the two places where a value comes from OUTSIDE — a keypress, a file on disk —
// and the code has to answer for the degenerate ones. Each is two or three lines with a
// single observable, and together they are the difference between a config package that
// fails quietly and one that answers.
//
// The two that are NOT here are the ones a test cannot honestly reach, and they are named
// at the bottom rather than left as an unexplained gap.

import (
	"os"
	"path/filepath"
	"testing"
)

// An empty key is not a key. Resolve is called with whatever a keypress stringified to, and
// an empty string matching an action would make a spurious binding fire — the same failure
// mode as a modifier key carrying its bare character.
func TestAnEmptyKeyResolvesToNothing(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})

	if id, ok := r.Resolve("", ContextGrid); ok {
		t.Errorf("the empty key resolved to %q", id)
	}
	// And the premise: the registry is not empty, so the guard is what answered.
	if len(r.All()) == 0 {
		t.Fatal("the registry has no actions; the case above proves nothing")
	}
}

// KeysFor on an action nobody declared is nil, not an empty slice with a different meaning.
// A caller ranging over it and one calling len on it both get the same answer either way,
// which is why the distinction has to be pinned rather than assumed.
func TestKeysForAnUndeclaredActionIsNil(t *testing.T) {
	r := NewKeybindRegistry(KeybindingsConfig{})

	if got := r.KeysFor("no_such_action"); got != nil {
		t.Errorf("KeysFor of an undeclared action is %v, want nil", got)
	}
	// The counterweight, and the reason nil is the right answer rather than a copy: a
	// declared action's keys are a COPY, so mutating them cannot corrupt the registry.
	quit := r.KeysFor("quit")
	if len(quit) == 0 {
		t.Fatal("quit has no keys; the fixture is wrong")
	}
	quit[0] = "mangled"
	if again := r.KeysFor("quit"); again[0] == "mangled" {
		t.Error("KeysFor handed out the registry's own slice")
	}
}

// The project state file, and the three things that can be wrong with it. Two of them are
// the same answer — an empty state — and the third is an error, which is the distinction
// worth having: a file the user cannot read is a problem to report, a file that is corrupt
// is a file to start over from.
func TestTheProjectStateFileAndItsThreeFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	t.Run("no file is an empty state, not an error", func(t *testing.T) {
		// The state of a first run. Returning an error here would make every command that
		// reads it fail on a machine that has never run dbx.
		ps, err := LoadProjectState()
		if err != nil {
			t.Fatalf("a missing state file is an error: %v", err)
		}
		if len(ps.InactiveProjects) != 0 {
			t.Errorf("a missing state file produced %v", ps.InactiveProjects)
		}
		if ps.InactiveProjects == nil {
			t.Error("InactiveProjects is nil, so writing to it would panic")
		}
	})

	t.Run("a corrupt file is an empty state too", func(t *testing.T) {
		// Deliberately the SAME answer as a missing file, and deliberately not an error: a
		// half-written state file would otherwise make dbx unstartable until the user found
		// and deleted a file they have no reason to know about.
		if err := os.MkdirAll(StateDir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ProjectStateFile(), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}

		ps, err := LoadProjectState()
		if err != nil {
			t.Fatalf("a corrupt state file is an error: %v", err)
		}
		if len(ps.InactiveProjects) != 0 {
			t.Errorf("a corrupt state file produced %v", ps.InactiveProjects)
		}
		if ps.InactiveProjects == nil {
			t.Error("a corrupt state file left InactiveProjects nil")
		}
	})

	t.Run("a file that cannot be read IS an error", func(t *testing.T) {
		// Its OWN home: the subtest above left a file exactly where this one wants a
		// directory, and sharing a HOME would make this fail for the reason it is meant to
		// disprove.
		own := t.TempDir()
		t.Setenv("HOME", own)
		t.Setenv("XDG_DATA_HOME", filepath.Join(own, ".local", "share"))

		// The third case, and the one that separates the two above. A directory where the
		// file should be is unreadable in a way that is not "there is nothing here yet",
		// and reporting it is what lets a user whose settings are not being saved find out.
		if err := os.MkdirAll(ProjectStateFile(), 0o755); err != nil {
			t.Fatal(err)
		}

		if _, err := LoadProjectState(); err == nil {
			t.Error("an unreadable state file reported no error")
		}
	})

	t.Run("and a round trip survives", func(t *testing.T) {
		// The counterweight for all three: a state that saves and loads is a state file
		// working, so the three failures above are about what happens when it does not.
		ps := &ProjectState{InactiveProjects: map[string]bool{"otherdb": true}}
		if err := ps.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		back, err := LoadProjectState()
		if err != nil {
			t.Fatalf("LoadProjectState: %v", err)
		}
		if !back.InactiveProjects["otherdb"] {
			t.Errorf("the round trip lost the state: %v", back.InactiveProjects)
		}
	})
}

// A .git FILE rather than a directory — the worktree case, and the only shape that goes
// through readGitdirFile. A missing one is the ordinary answer, and it has to be an answer
// rather than a panic, because the scanner walks whatever the filesystem has.
func TestAGitfileThatCannotBeReadIsNotARepo(t *testing.T) {
	if _, ok := readGitdirFile(filepath.Join(t.TempDir(), "absent")); ok {
		t.Error("a missing .git file was read as a repository")
	}
}

// LoadProjectConfig on a path that is not there. The error is returned unwrapped on
// purpose, so a caller can test it with errors.Is(err, fs.ErrNotExist) and offer to create
// the file — which is the whole reason this is not a bare fmt.Errorf.
func TestLoadingAProjectConfigThatIsNotThere(t *testing.T) {
	cfg, err := LoadProjectConfig(filepath.Join(t.TempDir(), "absent.toml"))
	if err == nil {
		t.Fatal("loading a missing config reported no error")
	}
	if cfg != nil {
		t.Errorf("loading a missing config also produced %+v", cfg)
	}
	if !os.IsNotExist(err) {
		t.Errorf("the error is %v, want one a caller can recognise as \"not found\"", err)
	}
}

// The two guards in repoid.go and canonicalPath that a test cannot honestly reach.
//
//	findDotGit's `parent == cur` is a loop terminator, and it cannot fire. Reaching it means
//	  passing the guard above, which returns unless `cur != root && pathWithin(cur, root)`
//	  — so cur is a proper descendant of root, and filepath.Dir returns its own argument for
//	  exactly one path, the filesystem root. The terminator guards a case the guard above
//	  already handles.
//
//	canonicalPath's `filepath.Abs` error needs os.Getwd to fail, which means deleting the
//	  process's working directory. That is a real state and it is not reachable from a test
//	  without syscalls that would outlive the test.
//
// Neither is written off as untested-and-therefore-fine: the first is a terminator whose
// removal would hang the scanner if the invariant it relies on were ever broken, so it
// stays. Both are recorded here so that a reader who sees them uncovered knows it was a
// decision and not an oversight.
func TestTheTwoGuardsThatCannotBeReached(t *testing.T) {
	t.Run("filepath.Dir is its own argument only at the filesystem root", func(t *testing.T) {
		// The premise the terminator relies on, asserted rather than assumed.
		if got := filepath.Dir("/"); got != "/" {
			t.Errorf("filepath.Dir(/) is %q, so the walk's terminator is not the only one", got)
		}
		if got := filepath.Dir("/a"); got == "/a" {
			t.Error("a path below the root has no parent, so the walk could run away")
		}
	})

	t.Run("and a real repository still resolves", func(t *testing.T) {
		// The counterweight: the walk the terminator belongs to still terminates, and does
		// the right thing, on the path where the other guard returns.
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(dir, "a", "b", "c")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		// root is where the walk STOPS, and it stops BEFORE inspecting it — so the .git
		// has to be at or above root, not at it. Passing root as the directory holding .git
		// refuses that very directory, which the first version of this case did and which
		// reported the walk broken rather than the fixture.
		want := filepath.Join(dir, ".git")
		if got, ok := findDotGit(nested, filepath.Dir(dir)); !ok || got != want {
			t.Errorf("findDotGit(%q, %q) = (%q, %t), want %q", nested, filepath.Dir(dir), got, ok, want)
		}
		// And the stop-at-root behaviour, which is the guard that makes the terminator
		// unreachable: a .git ABOVE root must not be found, or a monorepo scan would walk
		// out of the project it was asked about.
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(outside, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		inner := filepath.Join(outside, "inner")
		if err := os.MkdirAll(inner, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, ok := findDotGit(inner, inner); ok {
			t.Errorf("findDotGit found %q with no .git under the root it was given", got)
		}
	})
}

// digitRange compresses a run of same-prefix keys into "f1-f9", and every way of NOT being
// such a run has to be answered. The three below are the ones a hand-built registry produces
// and the default one does not — which is exactly why they went untested: the real registry
// always hands over f1..f9 in order.
//
// The tie-break in the scanner's sort is here for the same reason: two projects with the
// same Path can only come from two config files in one directory, and the real scanner finds
// one file per directory.
func TestDigitRangeRefusesEveryShapeThatIsNotARun(t *testing.T) {
	// A run is a CONTIGUOUS span: the number of keys has to equal the span it covers, which
	// is what the check at the end enforces. So two keys only form a run when they are
	// adjacent digits, and the first version of these cases used f1,f3 — which is a gap, and
	// which the contiguity check refuses, so it reported both bound-widening arms
	// unreachable when neither had been tried.
	t.Run("a descending pair widens the low bound", func(t *testing.T) {
		prefix, lo, hi, ok := digitRange([]string{"f2", "f1"})
		if !ok || prefix != "f" || lo != 1 || hi != 2 {
			t.Errorf("digitRange([f2 f1]) = (%q, %d, %d, %t), want (f, 1, 2, true)", prefix, lo, hi, ok)
		}
	})

	t.Run("an ascending pair widens the high bound", func(t *testing.T) {
		prefix, lo, hi, ok := digitRange([]string{"f1", "f2"})
		if !ok || prefix != "f" || lo != 1 || hi != 2 {
			t.Errorf("digitRange([f1 f2]) = (%q, %d, %d, %t), want (f, 1, 2, true)", prefix, lo, hi, ok)
		}
	})

	t.Run("a gap is not a run", func(t *testing.T) {
		// The contiguity check. f1,f3 would compress to "f1-f3" and claim F2 is bound to go
		// to page 2, which it is not — a key that silently does the wrong thing.
		if _, _, _, ok := digitRange([]string{"f1", "f3"}); ok {
			t.Error("f1 and f3 were compressed into a run, so F2 would be advertised")
		}
		if _, _, _, ok := digitRange([]string{"f1", "f2", "f4"}); ok {
			t.Error("f1, f2 and f4 were compressed into a run")
		}
	})

	t.Run("and a different prefix is not a run either", func(t *testing.T) {
		if _, _, _, ok := digitRange([]string{"f1", "g2"}); ok {
			t.Error("f1 and g2 were compressed into one run")
		}
	})

	t.Run("a single key is a run of one", func(t *testing.T) {
		// The counterweight: every refusal above would be satisfied by a function that
		// always refuses.
		if prefix, lo, hi, ok := digitRange([]string{"f7"}); !ok || prefix != "f" || lo != 7 || hi != 7 {
			t.Errorf("digitRange([f7]) = (%q, %d, %d, %t)", prefix, lo, hi, ok)
		}
		if _, _, _, ok := digitRange(nil); ok {
			t.Error("no keys at all is a run")
		}
	})
}

// The scanner's sort tie-breaks on Name when two projects share a Path, which happens when
// one directory holds two config files. Without the tie-break the order of two projects in
// the picker would depend on the order the filesystem returned them in.
func TestTheScannerOrdersTwoProjectsInOneDirectoryByName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	dir := t.TempDir()
	// Two files, one directory, two connection names. loadProject takes the
	// lexicographically first connection per FILE, so the names come from the files — and
	// the paths are handed over in the opposite order, so a sort that did nothing would fail.
	for _, c := range []struct{ file, conn string }{{"a.toml", "zeta"}, {"b.toml", "alpha"}} {
		body := "[connections." + c.conn + "]\ndriver = \"postgres\"\ndsn = \"postgres://" + c.conn + "@localhost/db\"\n"
		if err := os.WriteFile(filepath.Join(dir, c.file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := NewScanner(dir)
	s.findFiles = func(string) []string {
		return []string{filepath.Join(dir, "b.toml"), filepath.Join(dir, "a.toml")}
	}

	projects := s.Scan()
	if len(projects) != 2 {
		t.Fatalf("the scanner found %d projects, want both config files", len(projects))
	}
	// The premise of the tie-break: both projects really do share a Path.
	if projects[0].Path != projects[1].Path {
		t.Fatalf("the two projects are in different directories (%q, %q), so the tie-break is not what ordered them",
			projects[0].Path, projects[1].Path)
	}
	if projects[0].Name != "alpha" || projects[1].Name != "zeta" {
		t.Errorf("the projects came back as %q and %q, want alpha before zeta",
			projects[0].Name, projects[1].Name)
	}
}
