package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProjectState tracks which projects are active/inactive.
// Persisted in ~/.local/state/dbx/project_state.json
type ProjectState struct {
	InactiveProjects map[string]bool `json:"inactive_projects"`
}

// ProjectStateFile returns the path to the project state file.
func ProjectStateFile() string {
	return filepath.Join(StateDir(), "project_state.json")
}

// LoadProjectState reads the project state from disk.
// Returns an empty state if the file doesn't exist.
func LoadProjectState() (*ProjectState, error) {
	ps := &ProjectState{
		InactiveProjects: make(map[string]bool),
	}

	data, err := os.ReadFile(ProjectStateFile())
	if err != nil {
		if os.IsNotExist(err) {
			return ps, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, ps); err != nil {
		return ps, nil // return empty state on corrupt file
	}

	if ps.InactiveProjects == nil {
		ps.InactiveProjects = make(map[string]bool)
	}

	return ps, nil
}

// Save persists the project state to disk.
func (ps *ProjectState) Save() error {
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ProjectStateFile(), data, 0o644)
}

// IsActive returns true if the project at the given path is active.
// Projects are active by default (not in the inactive map).
func (ps *ProjectState) IsActive(projectPath string) bool {
	return !ps.InactiveProjects[projectPath]
}

// SetActive marks a project as active (removes from inactive map).
func (ps *ProjectState) SetActive(projectPath string) {
	delete(ps.InactiveProjects, projectPath)
}

// SetInactive marks a project as inactive.
func (ps *ProjectState) SetInactive(projectPath string) {
	ps.InactiveProjects[projectPath] = true
}

// Toggle returns the new active state after toggling.
func (ps *ProjectState) Toggle(projectPath string) bool {
	if ps.InactiveProjects[projectPath] {
		delete(ps.InactiveProjects, projectPath)
		return true // now active
	}
	ps.InactiveProjects[projectPath] = true
	return false // now inactive
}
