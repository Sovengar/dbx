package config

// Scenario: El estado activo/inactivo de cada proyecto sobrevive a un reinicio.
//
// ProjectState is what makes the project picker remember that you switched a database
// off. The file is one JSON object under the state directory, and every method on it is
// three lines — which is exactly why the untested parts were the ones about what happens
// when the file is missing, truncated, or written by something else.
//
// The failure that matters is not a crash. It is a state file that decodes to an empty
// map: every project then reads as active, including the three you deliberately
// switched off, and there is no way to tell that the file said otherwise. So the
// corrupt-file case asserts that it is distinguishable from "no file", and the
// nil-map case is asserted separately, because a nil map and an empty map both answer
// "everything is active" and only one of them can be written back out as JSON.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// stateHome points ProjectStateFile at a temp dir by moving the state directory.
// ProjectStateFile derives its path from StateDir(), which reads the user's home
// directory, so without this a test writes to the developer's real project_state.json
// and a "load" assertion reads the developer's real projects.
func stateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return dir
}

func TestProjectStateIsActiveByDefault(t *testing.T) {
	stateHome(t)

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}
	if ps == nil {
		t.Fatal("LoadProjectState returned nil with no file on disk")
	}
	if !ps.IsActive("/any/project") {
		t.Error("a project nobody has mentioned reads as inactive")
	}

	t.Run("the map is usable straight away", func(t *testing.T) {
		// A nil map reads fine and panics on write. LoadProjectState initialises it, and
		// this is the assertion that it does: SetInactive on a nil map is a panic.
		ps.SetInactive("/p")
		if ps.IsActive("/p") {
			t.Error("the project is active right after being set inactive")
		}
	})
}

func TestProjectStateTogglesRoundTripThroughDisk(t *testing.T) {
	stateHome(t)

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}

	const project = "/home/me/dev/myapp"

	ps.SetInactive(project)
	if ps.IsActive(project) {
		t.Error("the project is active right after SetInactive")
	}
	if err := ps.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read it back with a fresh object, which is what a restart does.
	reloaded, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState after Save: %v", err)
	}
	if reloaded.IsActive(project) {
		t.Error("the project is active after a save and reload; the state did not survive")
	}

	t.Run("a project that was never mentioned is still active", func(t *testing.T) {
		if !reloaded.IsActive("/home/me/dev/other") {
			t.Error("an unrelated project reads as inactive")
		}
	})

	t.Run("SetActive puts it back", func(t *testing.T) {
		reloaded.SetActive(project)
		if !reloaded.IsActive(project) {
			t.Error("the project is inactive after SetActive")
		}
		if err := reloaded.Save(); err != nil {
			t.Fatal(err)
		}
		again, err := LoadProjectState()
		if err != nil {
			t.Fatal(err)
		}
		if !again.IsActive(project) {
			t.Error("the project is inactive after saving SetActive and reloading")
		}
	})
}

func TestProjectStateToggleReportsTheNewState(t *testing.T) {
	stateHome(t)

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}
	const project = "/p"

	if got := ps.Toggle(project); got {
		t.Error("the first Toggle reported active; the project starts active so it should report inactive")
	}
	if ps.IsActive(project) {
		t.Error("the project is active after a Toggle that reported inactive")
	}
	if got := ps.Toggle(project); !got {
		t.Error("the second Toggle reported inactive; it should report active")
	}
	if !ps.IsActive(project) {
		t.Error("the project is inactive after a Toggle that reported active")
	}
}

func TestProjectStateSurvivesAFileItCannotUse(t *testing.T) {
	t.Run("a MISSING file is not an error and not an empty answer", func(t *testing.T) {
		// The normal first-run state: no file, every project active.
		stateHome(t)
		ps, err := LoadProjectState()
		if err != nil {
			t.Fatalf("a missing state file returned the error %v; it should not", err)
		}
		if ps == nil || len(ps.InactiveProjects) != 0 {
			t.Errorf("a missing state file produced %+v, want an empty state", ps)
		}
	})

	t.Run("a CORRUPT file yields an empty state and no error", func(t *testing.T) {
		// Deliberate: a truncated file from a killed process must not stop the picker
		// from starting. The cost is that the user's inactive flags are lost, which is
		// the right trade against a database client that refuses to open.
		dir := stateHome(t)
		path := filepath.Join(dir, ".local", "state", "dbx", "project_state.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"inactive_projects": {"a": `), 0o600); err != nil {
			t.Fatal(err)
		}

		ps, err := LoadProjectState()
		if err != nil {
			t.Errorf("a corrupt state file returned the error %v; it should be swallowed", err)
		}
		if ps == nil {
			t.Fatal("a corrupt state file produced nil")
		}
		if len(ps.InactiveProjects) != 0 {
			t.Errorf("a corrupt state file produced %v, want an empty map", ps.InactiveProjects)
		}
		if !ps.IsActive("/anything") {
			t.Error("a project reads as inactive after a corrupt state file")
		}
	})

	t.Run("a file with a NULL map decodes to a usable empty map", func(t *testing.T) {
		// `{"inactive_projects": null}` is legal JSON and decodes to a nil map. A nil
		// map READS as "everything is active", which is correct, but WRITING to it
		// panics — so SetInactive on the object that came back from this file would
		// take the app down the first time the user toggles a project.
		dir := stateHome(t)
		path := filepath.Join(dir, ".local", "state", "dbx", "project_state.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"inactive_projects": null}`), 0o600); err != nil {
			t.Fatal(err)
		}

		ps, err := LoadProjectState()
		if err != nil {
			t.Fatalf("LoadProjectState: %v", err)
		}
		if ps.InactiveProjects == nil {
			t.Fatal("a null inactive_projects decoded to a nil map; writing to it would panic")
		}
		ps.SetInactive("/p")
		if ps.IsActive("/p") {
			t.Error("the project is active after SetInactive on a null-map state")
		}
		if err := ps.Save(); err != nil {
			t.Fatalf("saving a state that started as null: %v", err)
		}
	})

	t.Run("a file holding a JSON LIST is not silently accepted", func(t *testing.T) {
		// json.Unmarshal into a struct fails on a list, so this lands in the corrupt
		// path — asserted so that stays true: if the type ever changes to a slice this
		// becomes a silent reinterpretation.
		dir := stateHome(t)
		path := filepath.Join(dir, ".local", "state", "dbx", "project_state.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`["a","b"]`), 0o600); err != nil {
			t.Fatal(err)
		}

		ps, err := LoadProjectState()
		if err != nil {
			t.Fatalf("LoadProjectState: %v", err)
		}
		if len(ps.InactiveProjects) != 0 {
			t.Errorf("a JSON list decoded to %v, want an empty map", ps.InactiveProjects)
		}
	})

	t.Run("the file is written where ProjectStateFile says", func(t *testing.T) {
		// Save and LoadProjectState each recompute the path from StateDir(). If either
		// derived it differently the state would be written somewhere nothing reads,
		// and the test above would pass because both used the same wrong one.
		stateHome(t)
		ps, err := LoadProjectState()
		if err != nil {
			t.Fatal(err)
		}
		ps.SetInactive("/p")
		if err := ps.Save(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(ProjectStateFile()); err != nil {
			t.Errorf("nothing at %s: %v", ProjectStateFile(), err)
		}
	})

	t.Run("what Save writes can be decoded back", func(t *testing.T) {
		stateHome(t)
		ps := &ProjectState{InactiveProjects: map[string]bool{"/a": true, "/b": true}}
		if err := ps.Save(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(ProjectStateFile())
		if err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Inactive map[string]bool `json:"inactive_projects"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("the saved file does not decode: %s", data)
		}
		if len(probe.Inactive) != 2 || !probe.Inactive["/a"] || !probe.Inactive["/b"] {
			t.Errorf("the saved file is %s, want both projects marked inactive", data)
		}
	})
}

// Scenario: Un Save que no puede crear el directorio devuelve el error.
func TestProjectStateSaveReportsAFailure(t *testing.T) {
	// Every other method here is best-effort, so this is the one that has to be loud:
	// the caller is about to tell the user their choice was saved.
	dir := stateHome(t)
	// A file where the state directory should be: MkdirAll cannot create it.
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)
	t.Setenv("USERPROFILE", blocker)

	ps := &ProjectState{InactiveProjects: map[string]bool{"/a": true}}
	if err := ps.Save(); err == nil {
		t.Error("Save returned no error with an uncreatable state directory")
	}
}
