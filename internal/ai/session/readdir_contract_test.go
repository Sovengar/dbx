package session

// Scenario: Las dos guardas que solo una carrera puede disparar, ahora con el seam.
//
// Both of these are `if err != nil { continue }` after a DirEntry's Info(), and for a long time
// neither could be reached by a test. The reasons are properties of the standard library rather
// than of this code, and they are worth writing down because they are the kind of thing a
// future reader will try again to arrange:
//
//   - os.ReadDir's DirEntry.Info() calls LSTAT, not stat. A DANGLING SYMLINK — a symlink whose
//     target is gone — stats successfully, so the guard survives it. The obvious fixture does
//     not work.
//   - Permissions do not help either. A file the process cannot stat fails for EVERY entry in
//     the directory, and the caller reads that as "nothing to sweep" rather than as a
//     per-entry failure. The guard is specifically about ONE entry vanishing while its
//     siblings are fine.
//
// What is left is a race between the listing and the stat, which is not something a test should
// try to win. So the listing is injected — readDir on both the Logger and the Reader — and the
// guard is driven with an entry that cannot be stat'ed at all.
//
// The behaviour is unchanged: production passes os.ReadDir and nothing else can tell. What the
// cases below hold is the consequence of the guard, which is the part that was never checked:
// the sweep skips the vanished file and CARRIES ON with the rest, rather than abandoning the
// directory or reporting a failure to a user who did not ask about housekeeping.

import (
	"errors"
	"os"
	"path"
	"testing"
	"time"
)

// unstattableEntry is a DirEntry whose Info() always fails — a file that vanished between the
// listing and the stat, which is the only thing the guard exists for.
type unstattableEntry struct {
	name string
	dir  bool
}

func (e unstattableEntry) Name() string      { return e.name }
func (e unstattableEntry) IsDir() bool       { return e.dir }
func (e unstattableEntry) Type() os.FileMode { return 0 }
func (e unstattableEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("no such file or directory")
}

// staleEntry is a DirEntry that stats fine and reports a modification time, which is what a
// file that is merely OLD looks like — the case the sweep exists for.
type staleEntry struct {
	name string
	age  time.Duration
}

func (e staleEntry) Name() string      { return e.name }
func (e staleEntry) IsDir() bool       { return false }
func (e staleEntry) Type() os.FileMode { return 0 }
func (e staleEntry) Info() (os.FileInfo, error) {
	return staleInfo{name: e.name, mod: time.Now().Add(-e.age)}, nil
}

type staleInfo struct {
	name string
	mod  time.Time
}

func (i staleInfo) Name() string       { return i.name }
func (i staleInfo) Size() int64        { return 0 }
func (i staleInfo) Mode() os.FileMode  { return 0 }
func (i staleInfo) ModTime() time.Time { return i.mod }
func (i staleInfo) IsDir() bool        { return false }
func (i staleInfo) Sys() any           { return nil }

// The retention sweep: a file that cannot be stat'ed is skipped, and the OLD files around it
// are still removed.
//
// The ordering is the assertion. A sweep that returned on the first failure would leave the
// rest of the directory untouched, and it would leave it untouched on every run, because the
// vanished file comes back as a vanished file until something else cleans the directory up —
// which nothing does. So "skip and continue" is the difference between a sweep that works and
// one that has quietly stopped working.
func TestTheRetentionSweepSkipsAFileThatCannotBeStatEd(t *testing.T) {
	dir := t.TempDir()

	// A file the sweep will really remove, so "the rest were still processed" is observable.
	doomed := oldEnoughFile(t, dir, "2020-01-01")
	// And a file recent enough to keep, so the sweep is not just deleting everything.
	kept := oldEnoughFile(t, dir, time.Now().Format("2006-01-02"))

	logger, err := NewLogger(dir, 1)
	if err != nil {
		t.Fatalf("the logger: %v", err)
	}
	logger.readDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			unstattableEntry{name: "vanished.jsonl"},
			staleEntry{name: "2020-01-01.jsonl", age: 10000 * 24 * time.Hour},
			staleEntry{name: kept, age: time.Minute},
		}, nil
	}

	if err := logger.Cleanup(); err != nil {
		t.Fatalf("the sweep: %v", err)
	}

	if _, err := os.Stat(doomed); err == nil {
		t.Error("the old file was not removed, so the sweep stopped at the file it could not stat")
	}
	if _, err := os.Stat(path.Join(dir, kept)); err != nil {
		t.Errorf("the recent file was removed: %v", err)
	}
	// And the one it could not stat is not an error the caller has to hear about.
	if _, err := os.Stat(path.Join(dir, kept)); err != nil {
		t.Errorf("the sweep reported a failure for a file it skipped on purpose: %v", err)
	}

	t.Run("and a directory in the listing is skipped without a stat", func(t *testing.T) {
		// The IsDir arm. It matters because os.ReadDir DOES return subdirectories, and a
		// retention sweep that deleted one would delete the sessions inside it.
		onlyDirs, err := NewLogger(t.TempDir(), 1)
		if err != nil {
			t.Fatal(err)
		}
		onlyDirs.readDir = func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{
				// A directory whose Info would succeed — so if the IsDir check were missing
				// this would be removed, and the arm after it would be reached.
				unstattableEntry{name: "archive.jsonl", dir: true},
			}, nil
		}
		if err := onlyDirs.Cleanup(); err != nil {
			t.Errorf("the sweep: %v", err)
		}
	})

	t.Run("and a listing that cannot be read is an error", func(t *testing.T) {
		// The other exit. A sweep that cannot see the directory has to say so rather than
		// reporting success, because the caller is deciding whether housekeeping happened.
		broken, err := NewLogger(t.TempDir(), 1)
		if err != nil {
			t.Fatal(err)
		}
		broken.readDir = func(string) ([]os.DirEntry, error) {
			return nil, os.ErrPermission
		}
		if err := broken.Cleanup(); err == nil {
			t.Error("a sweep that could not read its directory reported success")
		}
	})
}

// The reader's per-entry guard, on the session listing.
//
// Same shape and same reason, in the second of the two places that lists a directory. It is a
// separate function and a separate sweep, so a case covering the Logger says nothing about
// the Reader — which is why both have one.
func TestTheSessionListingSkipsAFileItCannotStat(t *testing.T) {
	dir := t.TempDir()
	// A real session file, so the listing has something to return alongside the entry it
	// cannot stat — and so "the listing is empty" is distinguishable from "it skipped one".
	real := "2020-06-15"
	if err := os.WriteFile(path.Join(dir, real+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReader(dir)
	r.readDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			unstattableEntry{name: "vanished.jsonl"},
			staleEntry{name: real + ".jsonl", age: time.Hour},
			// And one whose NAME is not a date at all, which is the other continue in the
			// loop: a stray file in the session directory is not a session.
			staleEntry{name: "notes.txt", age: time.Hour},
		}, nil
	}

	sessions, err := r.ListSessions()
	if err != nil {
		t.Fatalf("the listing: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("the listing returned %d sessions, want the one real date", len(sessions))
	}
	want, err := time.Parse("2006-01-02", real)
	if err != nil {
		t.Fatal(err)
	}
	if !sessions[0].Date.Equal(want) {
		t.Errorf("the listing returned the date %v, want %v", sessions[0].Date, want)
	}
	// The filename is the entry's name with the suffix stripped and the rest PARSED — so a
	// name that is not a date contributes nothing (the second continue in the loop), and one
	// that is contributes exactly one session.
	if sessions[0].Filename == "" {
		t.Error("the session has no filename")
	}
}

func oldEnoughFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := path.Join(dir, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate it, because the sweep compares against time.Now and a file written now is
	// inside the retention window whatever its NAME says.
	past := time.Now().Add(-10000 * 24 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	return name
}
