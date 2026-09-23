package config

import (
	"os"
	"path/filepath"
	"strings"
)

// gitRepoIdentity returns a stable identity shared by a git repository and all
// of its worktrees, without spawning `git`. It is derived from the common git
// directory so that the main checkout and its worktrees collapse together.
//
// When dir (or an ancestor) is not inside a git repository, the identity is the
// canonical path of dir itself: unique per directory, so nothing collapses.
func gitRepoIdentity(dir string) string {
	canonical := canonicalPath(dir)

	gitPath, ok := findDotGit(canonical)
	if !ok {
		return canonical
	}

	// `.git` directory: the repo itself, so the common dir is that directory.
	if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
		return canonicalPath(gitPath)
	}

	// `.git` file (worktree/submodule): "gitdir: <path>".
	gitdir, ok := readGitdirFile(gitPath)
	if !ok {
		return canonicalPath(gitPath)
	}

	// A linked worktree's gitdir holds a `commondir` file with the path to the
	// shared git dir, relative to the gitdir itself.
	if common, ok := readCommonDir(gitdir); ok {
		return canonicalPath(common)
	}

	return canonicalPath(gitdir)
}

// findDotGit walks up from dir looking for a `.git` entry (file or directory).
func findDotGit(dir string) (string, bool) {
	cur := dir
	for {
		candidate := filepath.Join(cur, ".git")
		if _, err := os.Lstat(candidate); err == nil {
			return candidate, true
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// readGitdirFile parses a `.git` file of the form "gitdir: <path>". Relative
// targets are resolved against the directory containing the `.git` file.
func readGitdirFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}

	target := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return target, true
}

// readCommonDir reads `<gitdir>/commondir`, whose content is a path relative to
// gitdir, and returns it resolved. Returns false when the file is missing.
func readCommonDir(gitdir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}

	rel := strings.TrimSpace(string(data))
	if rel == "" {
		return "", false
	}
	if !filepath.IsAbs(rel) {
		rel = filepath.Join(gitdir, rel)
	}
	return rel, true
}

// canonicalPath makes a path absolute and clean, resolving symlinks when
// possible. If resolution fails (permissions, missing path) it falls back to the
// absolute clean path: a conservative identity beats a panic.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return filepath.Clean(resolved)
}
