package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Scanner struct {
	RootDir string
}

func NewScanner(rootDir string) *Scanner {
	return &Scanner{RootDir: rootDir}
}

func (s *Scanner) Scan() []FoundProject {
	results := s.scanWithFD()
	if len(results) == 0 {
		results = s.scanWithWalkDir()
	}
	return results
}

func (s *Scanner) scanWithFD() []FoundProject {
	cmd := exec.Command("fd", "--hidden", "--type", "f", ".dbx.toml", s.RootDir)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var projects []FoundProject
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		if p, err := s.loadProject(line); err == nil {
			projects = append(projects, *p)
		}
	}

	return projects
}

func (s *Scanner) scanWithWalkDir() []FoundProject {
	var projects []FoundProject

	filepath.WalkDir(s.RootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".dbx.toml" {
			if p, err := s.loadProject(path); err == nil {
				projects = append(projects, *p)
			}
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		return nil
	})

	return projects
}

func (s *Scanner) loadProject(path string) (*FoundProject, error) {
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)

	for connName, conn := range cfg.Connections {
		return &FoundProject{
			Name:       connName,
			Path:       dir,
			Connection: conn,
		}, nil
	}

	return nil, nil
}
