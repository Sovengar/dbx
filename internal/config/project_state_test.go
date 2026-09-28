package config

import (
	"bytes"
	"os"
	"testing"
)

// Scenario: A state file that explicitly carries a null inactive_projects map
// still loads into a usable state. Save() on a zero-value state writes exactly
// that JSON, so the normalization on load is what keeps SetInactive from
// writing to a nil map and losing the toggle.
func TestLoadProjectState_NullInactiveProjectsYieldsUsableMap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A zero-value state marshals to {"inactive_projects":null}.
	if err := (&ProjectState{}).Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(ProjectStateFile())
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", ProjectStateFile(), err)
	}
	if !bytes.Contains(data, []byte(`"inactive_projects": null`)) {
		t.Fatalf("fixture is not a null map: %s", data)
	}

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}
	if ps == nil {
		t.Fatal("LoadProjectState returned a nil state for a valid file")
	}
	if ps.InactiveProjects == nil {
		t.Fatal("InactiveProjects is nil; SetInactive would panic on it")
	}
	if !ps.IsActive("/some/project") {
		t.Error("a project must be active by default")
	}

	ps.SetInactive("/some/project")
	if ps.IsActive("/some/project") {
		t.Error("SetInactive did not take effect on the normalized map")
	}
}

// Scenario: A state file that omits inactive_projects entirely also loads into
// a usable map.
func TestLoadProjectState_FileWithoutInactiveProjectsYieldsUsableMap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	// Valid JSON object, but no "inactive_projects" key.
	if err := os.WriteFile(ProjectStateFile(), []byte(`{"other":1}`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", ProjectStateFile(), err)
	}

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState: %v", err)
	}
	if ps == nil {
		t.Fatal("LoadProjectState returned a nil state for a valid file")
	}
	if ps.InactiveProjects == nil {
		t.Fatal("InactiveProjects is nil; SetInactive would panic on it")
	}
	if !ps.IsActive("/some/project") {
		t.Error("a project must be active by default")
	}
}

// Scenario: A corrupt state file is treated as an empty state instead of an
// error, so a bad write never blocks startup.
func TestLoadProjectState_CorruptFileLoadsAsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	if err := os.WriteFile(ProjectStateFile(), []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", ProjectStateFile(), err)
	}

	ps, err := LoadProjectState()
	if err != nil {
		t.Fatalf("LoadProjectState on a corrupt file: %v", err)
	}
	if ps == nil {
		t.Fatal("LoadProjectState returned a nil state for a corrupt file")
	}
	if !ps.IsActive("/some/project") {
		t.Error("a corrupt file must not mark any project inactive")
	}
}
