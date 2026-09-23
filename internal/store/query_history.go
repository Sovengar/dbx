package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type QueryEntry struct {
	SQL       string    `json:"sql"`
	Timestamp time.Time `json:"timestamp"`
	Favorite  bool      `json:"favorite"`
	Name      string    `json:"name,omitempty"`
}

type QueryStore struct {
	entries  []QueryEntry
	filePath string
}

func NewQueryStore(stateDir string) *QueryStore {
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state", "dbx")
	}
	s := &QueryStore{
		filePath: filepath.Join(stateDir, "query_history.json"),
	}
	s.load()
	return s
}

func (s *QueryStore) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var entries []QueryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	s.entries = entries
}

func (s *QueryStore) Save() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o644)
}

func (s *QueryStore) Add(sql string) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return
	}
	// Deduplicate: move to end if exists
	for i, e := range s.entries {
		if strings.TrimSpace(e.SQL) == sql {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	s.entries = append(s.entries, QueryEntry{
		SQL:       sql,
		Timestamp: time.Now(),
	})
	// Cap at 500 entries
	if len(s.entries) > 500 {
		s.entries = s.entries[len(s.entries)-500:]
	}
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) All() []QueryEntry {
	result := make([]QueryEntry, len(s.entries))
	copy(result, s.entries)
	// Return in reverse chronological order
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func (s *QueryStore) Favorites() []QueryEntry {
	var result []QueryEntry
	for _, e := range s.entries {
		if e.Favorite {
			result = append(result, e)
		}
	}
	// Reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func (s *QueryStore) ToggleFavorite(idx int) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	target := all[idx]
	for i := range s.entries {
		if s.entries[i].SQL == target.SQL && s.entries[i].Timestamp.Equal(target.Timestamp) {
			s.entries[i].Favorite = !s.entries[i].Favorite
			break
		}
	}
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) SetName(idx int, name string) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	target := all[idx]
	for i := range s.entries {
		if s.entries[i].SQL == target.SQL && s.entries[i].Timestamp.Equal(target.Timestamp) {
			s.entries[i].Name = name
			break
		}
	}
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) Delete(idx int) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	target := all[idx]
	for i := range s.entries {
		if s.entries[i].SQL == target.SQL && s.entries[i].Timestamp.Equal(target.Timestamp) {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) Len() int {
	return len(s.entries)
}

// MigrateGlobalHistory copies the global query_history.json to the project directory
// and renames the global file to .bak. Only migrates if the project file doesn't exist yet.
func MigrateGlobalHistory(stateDir, projectDir string) error {
	globalPath := filepath.Join(stateDir, "query_history.json")
	projectPath := filepath.Join(projectDir, "query_history.json")

	// If project file already exists, skip migration
	if _, err := os.Stat(projectPath); err == nil {
		return nil
	}

	// If global file doesn't exist, nothing to migrate
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		return nil
	}

	// Read global file
	data, err := os.ReadFile(globalPath)
	if err != nil {
		return err
	}

	// Create project directory
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	// Write to project file
	if err := os.WriteFile(projectPath, data, 0o644); err != nil {
		return err
	}

	// Rename global file to .bak
	bakPath := globalPath + ".bak"
	if err := os.Rename(globalPath, bakPath); err != nil {
		return err
	}

	return nil
}
