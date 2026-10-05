package session

// Scenario: Un logger que no debe romperse por un fichero roto, y una guarda que no se
// puede probar.
//
// The logger reads a DIRECTORY of session files it did not write and cannot control: the
// user can put anything there, a sync client can leave a half-written file, a symlink can
// point at nothing. Both loops skip what they cannot handle and carry on, because the
// alternative — a cleanup or a history read that aborts on one bad entry — makes the whole
// log inaccessible over one stray file.
//
// THE GUARD AT THE BOTTOM OF THIS FILE IS NOT COVERED, and the reason is not tidiness.
//
//	entries, _ := os.ReadDir(dir)
//	for _, entry := range entries {
//	    if entry.IsDir() { continue }
//	    _, err := entry.Info()
//	    if err != nil { continue }      // ← unreachable from a test
//
// os.ReadDir's DirEntry.Info() calls lstat, and lstat does NOT follow a symlink — so a
// dangling symlink is stat'ed successfully and never reaches the guard. Making it fail
// needs EACCES on the directory, and that fails for EVERY entry at once, so it cannot
// demonstrate "skip one, keep the rest"; it only demonstrates "skip all". The remaining
// trigger is the file disappearing between the ReadDir and the Info, which is a race no
// test can schedule without a seam in the loop.
//
// So the guard is a TOCTOU check with no reachable input, kept because removing it turns a
// benign race into a crash of the history screen, and pinned here so that its absence from
// the coverage report is a decision rather than an oversight.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// future stamps a path so it is reliably newer than any cutoff the cleanup computes, since
// writing a file gives it the current time rather than the date in its name.
func future(t *testing.T, p string) {
	t.Helper()
	at := time.Now().AddDate(1, 0, 0)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// past stamps a path so it is reliably older than a 30-day retention cutoff.
func past(t *testing.T, p string) {
	t.Helper()
	at := time.Now().AddDate(-1, 0, 0)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The cleanup loop, with the entries it must skip rather than trip over: a directory, a
// recent file, and a dangling symlink.
//
// The symlink is here because it is the entry that LOOKS like it should break something —
// it has no size, no mode that means anything and no target — and it must not. It is also,
// as the note at the top says, the proof that the guard above it is not about symlinks.
func TestCleanupRemovesOldFilesAndLeavesEverythingElse(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "2024-01-01.jsonl")
	recent := filepath.Join(dir, "2999-01-01.jsonl")
	write(t, old, "{}\n")
	write(t, recent, "{}\n")
	// Writing a file gives it the CURRENT time, so both dates have to be stamped: a fixture
	// whose file names say 2024 and whose mtime says today would survive the cleanup, and
	// the first version of this case did exactly that and reported the cleanup broken.
	past(t, old)
	future(t, recent)

	if err := os.MkdirAll(filepath.Join(dir, "a-directory.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink to nothing. lstat succeeds on it, so it is stat'ed, found to be old, and
	// then removed — which is the honest behaviour: it is an old entry in the log directory
	// and cleanup is allowed to remove it.
	if err := os.Symlink(filepath.Join(dir, "no-such-target"), filepath.Join(dir, "dangling.jsonl")); err != nil {
		t.Skipf("this filesystem will not take a symlink: %v", err)
	}

	l := &Logger{dir: dir, retention: 30}
	if err := l.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Errorf("the old file survived: %v", err)
	}
	if _, err := os.Lstat(recent); err != nil {
		t.Errorf("the recent file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a-directory.jsonl")); err != nil {
		t.Errorf("the directory was removed: %v", err)
	}

	t.Run("no retention means no cleanup at all", func(t *testing.T) {
		// retention <= 0 is the default a caller gets for "keep everything", and it must
		// not be read as "delete everything".
		keep := t.TempDir()
		keepMe := filepath.Join(keep, "ancient.jsonl")
		write(t, keepMe, "{}\n")
		past(t, keepMe)

		if err := (&Logger{dir: keep, retention: 0}).Cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		if _, err := os.Stat(keepMe); err != nil {
			t.Errorf("a retention of 0 removed %s", keepMe)
		}
	})

	t.Run("a directory that is not there is an error", func(t *testing.T) {
		if err := (&Logger{dir: filepath.Join(dir, "absent"), retention: 30}).Cleanup(); err == nil {
			t.Error("cleaning up a directory that does not exist reported no error")
		}
	})
}

// The session LIST, with the entries it must skip: a file that is not a log, and a
// directory whose name ends in .jsonl — which the suffix check alone would let through, and
// which is why the IsDir check is there and not just for tidiness.
func TestListingSessionsSkipsWhatIsNotASession(t *testing.T) {
	dir := t.TempDir()

	days := []string{"2024-03-02", "2024-03-01"}
	for _, day := range days {
		write(t, filepath.Join(dir, day+".jsonl"),
			`{"timestamp":"`+day+`T10:00:00Z","level":"query","action":"run","sql":"SELECT 1","duration_ms":1}`+"\n")
	}
	// An error entry, so the counting of errors is exercised alongside queries.
	write(t, filepath.Join(dir, "2024-03-03.jsonl"),
		`{"timestamp":"2024-03-03T10:00:00Z","level":"error","action":"run","error":"boom"}`+"\n")
	write(t, filepath.Join(dir, "notes.txt"), "hello")
	write(t, filepath.Join(dir, "2024-03-04"), "not jsonl, no suffix")
	if err := os.MkdirAll(filepath.Join(dir, "2024-03-05.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	sessions, err := (&Reader{dir: dir}).ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(sessions) != 3 {
		t.Fatalf("the lister returned %d sessions, want 3: %+v", len(sessions), sessions)
	}
	for _, si := range sessions {
		if si.Date.IsZero() {
			t.Errorf("a session came back with a zero date: %+v", si)
		}
		if si.Filename != si.Date.Format("2006-01-02")+".jsonl" {
			t.Errorf("a session's filename does not match its date: %+v", si)
		}
	}

	byDate := map[string]int{}
	for _, si := range sessions {
		byDate[si.Date.Format("2006-01-02")] = si.Queries
	}
	if byDate["2024-03-02"] != 1 || byDate["2024-03-01"] != 1 {
		t.Errorf("the query counts came back as %v", byDate)
	}
	if byDate["2024-03-03"] != 0 {
		t.Errorf("the error session counted %d queries, want 0", byDate["2024-03-03"])
	}

	t.Run("a directory that is not there is an error", func(t *testing.T) {
		if _, err := (&Reader{dir: filepath.Join(dir, "absent")}).ListSessions(); err == nil {
			t.Error("listing a directory that does not exist reported no error")
		}
	})
}
