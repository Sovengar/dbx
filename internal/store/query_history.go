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

// defaultMaxEntries is how many queries are kept when the caller states no limit. It was
// a literal 500 inside Add, so ui.history_size had nowhere to go: the knob was declared
// with a default of 100, and the number actually used was 500, and the two never met.
const defaultMaxEntries = 500

type QueryStore struct {
	entries    []QueryEntry
	filePath   string
	maxEntries int
}

// NewQueryStore builds a store with the default cap. Callers that have a configured
// limit use NewQueryStoreLimited.
func NewQueryStore(stateDir string) *QueryStore {
	return NewQueryStoreLimited(stateDir, defaultMaxEntries)
}

// NewQueryStoreLimited builds a store that keeps at most maxEntries queries, dropping the
// OLDEST. A limit of zero or less means the default rather than "keep nothing", so a
// zero-valued config cannot silently erase a user's history.
func NewQueryStoreLimited(stateDir string, maxEntries int) *QueryStore {
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	if stateDir == "" {
		// The same rule config.StateDir applies, and the reason it is spelled out
		// here rather than imported: this file had its own copy of the expression,
		// WITH the same discarded error, so an empty directory resolved to
		// ".local/state/dbx" relative to the working directory. That was the
		// third copy of one path in this repository, the other two being
		// config.StateDir and the default for ui.state_dir.
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			stateDir = filepath.Join(os.TempDir(), "dbx", "state")
		} else {
			stateDir = filepath.Join(home, ".local", "state", "dbx")
		}
	}
	s := &QueryStore{
		filePath:   filepath.Join(stateDir, "query_history.json"),
		maxEntries: maxEntries,
	}
	s.load()
	return s
}

// MaxEntries is the cap currently in force.
func (s *QueryStore) MaxEntries() int { return s.maxEntries }

func (s *QueryStore) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var entries []QueryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	// Trim ON LOAD as well as on Add. Only trimming on Add would leave a file
	// written under a larger limit full until the user happened to run enough
	// queries to push the oldest out — so lowering ui.history_size would appear
	// to do nothing until the cap was crossed the other way.
	s.entries = trimOldest(entries, s.maxEntries)
}

// trimOldest keeps at most n entries, dropping from the front. The store is oldest-first,
// so the entries to lose are at the head.
func trimOldest(entries []QueryEntry, n int) []QueryEntry {
	if n > 0 && len(entries) > n {
		return entries[len(entries)-n:]
	}
	return entries
}

func (s *QueryStore) Save() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// No error arm on the marshal, and there used to be one. QueryEntry is two strings, a bool
	// and a time.Time — all of which encoding/json can encode, and time.Time has its own
	// MarshalJSON. A value that would fail cannot be produced by Add, SetName or
	// ToggleFavorite, which are the only three that put anything in the list.
	//
	// Which is the shape of this codebase's other marshal guards: the same three lines,
	// copied into each file, none of them ever asked whether the type could fail. The one in
	// cli/context.go and the one in ai/context/schema.go have been removed for the same reason.
	data, _ := json.MarshalIndent(s.entries, "", "  ")
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
	s.entries = trimOldest(s.entries, s.maxEntries)
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

// indexOf finds the stored entry that a display entry refers to. Entries are matched on
// SQL AND timestamp rather than on position, because a caller showing a FILTERED or
// FAVORITES-only list has no index into the store that means anything: those lists are
// subsequences of All(), so row i of one is row j of the other for some j >= i, and j is
// not i as soon as anything has been filtered out.
func (s *QueryStore) indexOf(target QueryEntry) int {
	for i := range s.entries {
		if s.entries[i].SQL == target.SQL && s.entries[i].Timestamp.Equal(target.Timestamp) {
			return i
		}
	}
	return -1
}

func (s *QueryStore) ToggleFavorite(idx int) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	s.ToggleFavoriteEntry(all[idx])
}

// ToggleFavoriteEntry flips the favorite flag of one specific query. This is the form a UI
// should call: it takes the entry the user is looking at, not a position, so a filtered or
// favorites-only list cannot make it act on a different query.
func (s *QueryStore) ToggleFavoriteEntry(target QueryEntry) {
	i := s.indexOf(target)
	if i < 0 {
		return
	}
	s.entries[i].Favorite = !s.entries[i].Favorite
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) SetName(idx int, name string) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	if i := s.indexOf(all[idx]); i >= 0 {
		s.entries[i].Name = name
	}
	// Best-effort persistence: the in-memory state is already updated.
	_ = s.Save()
}

func (s *QueryStore) Delete(idx int) {
	all := s.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	s.DeleteEntry(all[idx])
}

// DeleteEntry removes one specific query. Same reasoning as ToggleFavoriteEntry, and the
// stakes are higher: an index that names the wrong row does not merely mislabel a star,
// it destroys a query the user never chose.
func (s *QueryStore) DeleteEntry(target QueryEntry) {
	i := s.indexOf(target)
	if i < 0 {
		return
	}
	s.entries = append(s.entries[:i], s.entries[i+1:]...)
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
