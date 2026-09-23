package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// errNoConnections marks a .dbx.toml that parsed but defines no [connections].
// It is a valid configuration, so callers skip it silently instead of logging.
var errNoConnections = errors.New("project config defines no connections")

type Scanner struct {
	RootDir string

	// findFiles discovers candidate .dbx.toml paths under a root directory.
	// It is a test seam: when nil, Scan falls back to the real fd/WalkDir
	// discovery. Tests inject a deterministic, fd-independent list instead.
	findFiles func(root string) []string
}

func NewScanner(rootDir string) *Scanner {
	return &Scanner{RootDir: rootDir}
}

func (s *Scanner) Scan() []FoundProject {
	state, _ := LoadProjectState()

	find := s.findFiles
	if find == nil {
		find = s.defaultFindFiles
	}

	var projects []FoundProject
	for _, path := range find(s.RootDir) {
		if p, err := s.loadProject(path); err == nil {
			projects = append(projects, *p)
		}
	}

	// Collapse copies of the same (git repo, connection name, resolved DSN)
	// before applying state, so the state always lands on the surviving path.
	projects = dedupeProjects(projects)

	for i := range projects {
		projects[i].Active = state.IsActive(projects[i].Path)
	}

	// Stable output order: the picker respects the slice order, so a
	// deterministic (Path, Name) sort keeps it reproducible across scans.
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Path != projects[j].Path {
			return projects[i].Path < projects[j].Path
		}
		return projects[i].Name < projects[j].Name
	})

	return projects
}

// defaultFindFiles returns .dbx.toml paths using fd --hidden, falling back to a
// hidden-dir-skipping WalkDir when fd is unavailable or returns nothing.
func (s *Scanner) defaultFindFiles(root string) []string {
	if paths := s.scanWithFD(root); len(paths) > 0 {
		return paths
	}
	return s.scanWithWalkDir(root)
}

func (s *Scanner) scanWithFD(root string) []string {
	cmd := exec.Command("fd", "--hidden", "--type", "f", ".dbx.toml", root)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}

	return paths
}

func (s *Scanner) scanWithWalkDir(root string) []string {
	var paths []string

	// Walk errors are handled inside the callback, which never propagates
	// them, so the returned error is always nil.
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == ".dbx.toml" {
			paths = append(paths, path)
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		return nil
	})

	return paths
}

func (s *Scanner) loadProject(path string) (*FoundProject, error) {
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		return nil, err
	}

	if len(cfg.Connections) == 0 {
		return nil, errNoConnections
	}

	// Deterministic selection: lexicographically first connection name.
	names := make([]string, 0, len(cfg.Connections))
	for name := range cfg.Connections {
		names = append(names, name)
	}
	sort.Strings(names)
	name := names[0]

	return &FoundProject{
		Name:       name,
		Path:       filepath.Dir(path),
		Connection: cfg.Connections[name],
	}, nil
}

// dedupeProjects collapses entries that share a (git repo identity, connection
// name, resolved DSN) key, keeping the stable survivor chosen by shortestPath.
// Non-git directories use their own path as identity, so they never collapse.
func dedupeProjects(projects []FoundProject) []FoundProject {
	groups := make(map[projectKey][]FoundProject, len(projects))
	order := make([]projectKey, 0, len(projects))

	for _, p := range projects {
		key := projectKey{
			repo: gitRepoIdentity(p.Path),
			name: p.Name,
			dsn:  p.Connection.GetDSN(),
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], p)
	}

	out := make([]FoundProject, 0, len(order))
	for _, key := range order {
		out = append(out, shortestPath(groups[key]))
	}
	return out
}

type projectKey struct {
	repo string
	name string
	dsn  string
}

// shortestPath picks the survivor: shortest path first, then lexicographic.
// It is independent of session state, so the result is stable.
func shortestPath(candidates []FoundProject) FoundProject {
	best := candidates[0]
	for _, c := range candidates[1:] {
		if len(c.Path) < len(best.Path) ||
			(len(c.Path) == len(best.Path) && c.Path < best.Path) {
			best = c
		}
	}
	return best
}
