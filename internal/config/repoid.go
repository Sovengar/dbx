package config

import (
	"os"
	"path/filepath"
	"strings"
)

// repoIdentity describes the git repository a project directory belongs to.
type repoIdentity struct {
	// id is canonical and shared by a repo and all of its linked worktrees.
	id string
	// isMain is true when the directory is the main checkout, i.e. its resolved
	// `.git` is a directory. Linked worktrees (`.git` file) and non-git
	// directories report false.
	isMain bool
}

// gitRepoIdentity derives the identity of the repository containing dir, without
// spawning `git`. root must already be canonical (callers canonicalize it once)
// and bounds the upward search for `.git`: the walk never inspects ancestors of
// root, so a git repo that merely contains root (e.g. a dotfiles $HOME, or ~/dev)
// does not absorb non-git projects.
//
// When dir (or an ancestor up to root) is not inside a git repository, the
// identity is the canonical path of dir itself: unique per directory, so nothing
// collapses.
func gitRepoIdentity(dir, root string) repoIdentity {
	canonical := canonicalPath(dir)

	gitPath, ok := findDotGit(canonical, root)
	if !ok {
		return repoIdentity{id: canonical}
	}

	// `.git` directory: the main checkout, so the common dir is that directory.
	if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
		return repoIdentity{id: canonicalPath(gitPath), isMain: true}
	}

	// `.git` file (worktree/submodule): "gitdir: <path>".
	gitdir, ok := readGitdirFile(gitPath)
	if !ok {
		return repoIdentity{id: canonicalPath(gitPath)}
	}

	// A linked worktree's gitdir holds a `commondir` file with the path to the
	// shared git dir, relative to the gitdir itself.
	if common, ok := readCommonDir(gitdir); ok {
		return repoIdentity{id: canonicalPath(common)}
	}

	return repoIdentity{id: canonicalPath(gitdir)}
}

// findDotGit walks up from dir looking for a `.git` entry (file or directory),
// bounded by root. If dir is not inside root, no ancestor of dir is inspected: the walk
// never escapes root.
//
// root is inspected ONLY when dir IS root. That is the whole point of the bound and it
// used to be checked one step too late: the loop tried `root/.git` and only then asked
// whether it had reached root, so a repository AT root was found from anywhere below it.
// The case that matters is a dotfiles repository at $HOME, which is the repository that
// used to absorb every project under the home directory — every project sharing one
// identity means one project's active flag, drafts and query history applied to the
// others, and no way to tell which project the state belonged to.
//
// A project that IS root still gets root's own repository: the check is `cur == dir`, not
// `cur == root`, so a checkout at the scanned root is unaffected.
func findDotGit(dir, root string) (string, bool) {
	cur := dir
	for {
		// At root, and we walked up to get here: stop before inspecting it.
		if cur == root && cur != dir {
			return "", false
		}

		candidate := filepath.Join(cur, ".git")
		if _, err := os.Lstat(candidate); err == nil {
			return candidate, true
		}

		// Stop once we would leave root, or as soon as we are outside it.
		if cur == root || !pathWithin(cur, root) {
			return "", false
		}

		// No terminator, and it used to have one: filepath.Dir returns its own argument for
		// exactly one path, the filesystem root, and the guard above has already returned
		// unless cur is root or a descendant of it. So the walk only ever visits proper
		// descendants, each of which strictly shortens, and termination follows from the
		// path's finite depth rather than from a test.
		//
		// The premise is asserted by TestTheTwoGuardsThatCannotBeReached, so the argument
		// cannot rot unnoticed: if Dir ever stopped shortening a proper descendant, that
		// test fails instead of this scan hanging.
		cur = filepath.Dir(cur)
	}
}

// pathWithin reports whether path is root itself or a descendant of root.
func pathWithin(path, root string) bool {
	if path == root {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(path, prefix)
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
