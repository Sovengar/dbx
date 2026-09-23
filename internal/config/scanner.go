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

	projects := s.discoverProjects()

	// Collapse copies of the same (git repo, connection name, driver, resolved
	// DSN, ssh tunnel) before applying state, so the state always lands on the
	// surviving path.
	projects = dedupeProjects(projects, s.RootDir)

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

// discoverProjects returns the loaded projects. A test seam (findFiles) takes
// precedence; otherwise fd is tried first and, when it yields no loadable
// project, the WalkDir fallback runs. The fallback triggers on loaded results,
// not on raw paths, so fd returning only unparseable files still falls back.
func (s *Scanner) discoverProjects() []FoundProject {
	if s.findFiles != nil {
		return s.loadPaths(s.findFiles(s.RootDir))
	}

	projects := s.loadPaths(s.scanWithFD(s.RootDir))
	if len(projects) == 0 {
		projects = s.loadPaths(s.scanWithWalkDir(s.RootDir))
	}
	return projects
}

func (s *Scanner) loadPaths(paths []string) []FoundProject {
	var projects []FoundProject
	for _, path := range paths {
		if p, err := s.loadProject(path); err == nil {
			projects = append(projects, *p)
		}
	}
	return projects
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
// name, driver, resolved DSN, ssh tunnel) key, keeping one survivor per group.
// Non-git directories use their own path as identity, so they never collapse.
// root bounds the git search so an enclosing repo does not absorb non-git
// projects.
func dedupeProjects(projects []FoundProject, root string) []FoundProject {
	groups := make(map[projectKey][]dedupCandidate, len(projects))
	order := make([]projectKey, 0, len(projects))

	// Canonicalize the root once instead of once per project.
	canonicalRoot := canonicalPath(root)

	for _, p := range projects {
		repo := gitRepoIdentity(p.Path, canonicalRoot)
		key := projectKey{
			repo:      repo.id,
			name:      p.Name,
			driver:    p.Connection.Driver,
			dsn:       p.Connection.GetDSN(),
			sshTunnel: p.Connection.SSHTunnel,
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], dedupCandidate{project: p, isMain: repo.isMain})
	}

	out := make([]FoundProject, 0, len(order))
	for _, key := range order {
		out = append(out, survivor(groups[key]))
	}
	return out
}

type projectKey struct {
	repo      string
	name      string
	driver    string
	dsn       string
	sshTunnel string
}

// dedupCandidate pairs a project with whether its directory is the main git
// checkout (as opposed to a linked worktree or a non-git directory).
type dedupCandidate struct {
	project FoundProject
	isMain  bool
}

// survivor picks the entry that represents the real connection: the main
// checkout wins over linked worktrees regardless of path length; otherwise the
// shortest path, then lexicographic. It is independent of session state, so the
// result is stable.
func survivor(candidates []dedupCandidate) FoundProject {
	best := candidates[0]
	for _, c := range candidates[1:] {
		if isBetterSurvivor(c, best) {
			best = c
		}
	}
	return best.project
}

func isBetterSurvivor(a, b dedupCandidate) bool {
	if a.isMain != b.isMain {
		return a.isMain
	}
	if len(a.project.Path) != len(b.project.Path) {
		return len(a.project.Path) < len(b.project.Path)
	}
	return a.project.Path < b.project.Path
}
