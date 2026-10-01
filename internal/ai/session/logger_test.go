package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// datedName is the filename the Logger writes to today. The Logger derives it from
// time.Now() and there is no seam to inject a clock, so every assertion about which
// file was written is stated in terms of today rather than a fixed date.
func datedName() string {
	return time.Now().Format("2006-01-02") + ".jsonl"
}

// newTestLogger gives a Logger in a fresh temp dir, plus that dir.
func newTestLogger(t *testing.T, retentionDays int) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	l, err := NewLogger(dir, retentionDays)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, dir
}

// readLines reads a log file as raw lines, so a test can talk about the JSONL shape
// rather than about decoded structs.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	trimmed := strings.TrimSuffix(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// decodeLine decodes one JSONL line back into a LogEntry.
func decodeLine(t *testing.T, line string) LogEntry {
	t.Helper()
	var e LogEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("line is not a LogEntry: %v\n%s", err, line)
	}
	return e
}

// onlyAsRoot skips a test that depends on a file NOT being readable or removable.
// Root bypasses permission bits entirely, so the assertion would pass for the wrong
// reason there — the operation would succeed and the test would prove nothing.
func onlyAsRoot(t *testing.T, what string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skipf("running as root: %s would succeed regardless of its mode bits, so this case would prove nothing", what)
	}
}

// --- NewLogger --------------------------------------------------------------

// Scenario: El logger crea su directorio, incluidos los que faltan.
//
// The session dir is usually configured to somewhere that does not exist yet — a
// fresh checkout, a new machine — and the logger is the only thing that creates it.
// A nested path has to work too, because MkdirAll and not Mkdir is what makes that
// true, and a test that only used one level would not tell them apart.
func TestNewLogger_CreatesTheDirectoryItNeeds(t *testing.T) {
	for _, tc := range []struct {
		name string
		dir  func(base string) string
	}{
		{"a single missing level", func(b string) string { return filepath.Join(b, "sessions") }},
		{"several missing levels", func(b string) string { return filepath.Join(b, "a", "b", "c", "sessions") }},
		{"a directory that already exists", func(b string) string {
			d := filepath.Join(b, "sessions")
			if err := os.MkdirAll(d, 0755); err != nil {
				t.Fatal(err)
			}
			return d
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir(t.TempDir())
			l, err := NewLogger(dir, 7)
			if err != nil {
				t.Fatalf("NewLogger(%q) = %v, want it to create the directory", dir, err)
			}
			defer l.Close()

			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("the directory does not exist after NewLogger: %v", err)
			}
			if !info.IsDir() {
				t.Fatalf("%q is not a directory", dir)
			}
		})
	}
}

// Scenario: Si el directorio no se puede crear, el error lo dice.
//
// A logger with no file is worse than no logger: every call would fail later, at a
// point where the caller is holding a database cursor. Failing at construction says
// which path could not be created.
//
// A regular FILE where the directory should go is the trigger: MkdirAll cannot
// create it, and no permission bits are involved, so this holds for any user.
func TestNewLogger_FailsWhenThePathCannotBeADirectory(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("i am a file"), 0644); err != nil {
		t.Fatal(err)
	}

	l, err := NewLogger(blocker, 7)
	if err == nil {
		_ = l.Close()
		t.Fatal("NewLogger succeeded with a regular file where the directory should be")
	}
	if l != nil {
		t.Errorf("NewLogger returned a logger (%+v) alongside its error, so a caller ignoring the error would write into the void", l)
	}
	// The message names what failed, because the path is the only useful thing in
	// it and the user has to go and look at their config.
	if !strings.Contains(err.Error(), "session dir") {
		t.Errorf("the error is %q, want it to name the session dir", err)
	}
}

// Scenario: El archivo de hoy se abre para AÑADIR, no para pisar.
//
// The log is the only record of what the AI session did. Two loggers on the same
// day — which is what happens if the app is restarted, or if two windows run — must
// both write, and neither may truncate what the other wrote. O_APPEND is the whole
// reason this holds, and truncating instead is silent data loss.
func TestNewLogger_AppendsToTodaysFileAndNeverTruncates(t *testing.T) {
	dir := t.TempDir()

	first, err := NewLogger(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.LogQuery("SELECT 1", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// A second logger for the same day.
	second, err := NewLogger(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.LogQuery("SELECT 2", time.Millisecond, 1); err != nil {
		t.Fatal(err)
	}

	// Both survive, and the file is the dated one and nothing else.
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		var got []string
		for _, n := range names {
			got = append(got, n.Name())
		}
		t.Fatalf("the session dir holds %v, want only today's file", got)
	}
	if want := datedName(); names[0].Name() != want {
		t.Errorf("the logger wrote %q, want %q", names[0].Name(), want)
	}

	lines := readLines(t, filepath.Join(dir, datedName()))
	if len(lines) != 2 {
		t.Fatalf("the log holds %d entries, want 2: the second logger truncated the first's", len(lines))
	}
	if got := decodeLine(t, lines[0]).SQL; got != "SELECT 1" {
		t.Errorf("the first entry is %q, want the first logger's", got)
	}
	if got := decodeLine(t, lines[1]).SQL; got != "SELECT 2" {
		t.Errorf("the second entry is %q, want the second logger's", got)
	}
}

// --- Log --------------------------------------------------------------------

// Scenario: El timestamp lo pone el logger, no quien llama.
//
// The caller cannot know when the entry was actually written, and a hand-built
// timestamp from a caller is exactly the sort of thing that arrives as the zero time
// or as a stale value. The logger overwrites it, so the log's clock is one clock.
func TestLog_OverwritesTheCallersTimestamp(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	stale := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := l.Log(LogEntry{Level: LogSchema, Action: "schema", Timestamp: stale}); err != nil {
		t.Fatal(err)
	}

	entry := decodeLine(t, readLines(t, filepath.Join(dir, datedName()))[0])
	if entry.Timestamp.Equal(stale) {
		t.Error("the log kept the timestamp the caller supplied, so the entry claims to be from 1999")
	}
	// It has to be roughly now. Generous bounds: this is a clock assertion, not a
	// timing one, and a slow machine must not fail it.
	delta := time.Since(entry.Timestamp)
	if delta < -time.Minute || delta > time.Minute {
		t.Errorf("the stamped timestamp is %v away from now, want it to be the moment of writing", delta)
	}
}

// Scenario: Una entrada por línea, y el archivo es JSONL.
//
// The format is a contract with the replay path and with anything else that reads
// the log: one JSON object per line, so a reader can stream it and a truncated last
// line loses one entry instead of the file. A pretty-printed object would break
// every line-oriented reader.
func TestLog_WritesOneJSONObjectPerLine(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	const n = 5
	for i := range n {
		if err := l.LogQuery(fmt.Sprintf("SELECT %d", i), time.Millisecond, i); err != nil {
			t.Fatal(err)
		}
	}

	lines := readLines(t, filepath.Join(dir, datedName()))
	if len(lines) != n {
		t.Fatalf("the file has %d lines for %d entries, want one each", len(lines), n)
	}
	for i, line := range lines {
		if strings.ContainsAny(line, "\n") {
			t.Errorf("line %d contains a newline, so the file is not one object per line: %q", i, line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "{") != true {
			t.Errorf("line %d does not start an object: %q", i, line)
		}
		// And the order is the order it was written in, which for a single
		// goroutine is the call order.
		if got, want := decodeLine(t, line).SQL, fmt.Sprintf("SELECT %d", i); got != want {
			t.Errorf("line %d is %q, want %q: the log is out of order", i, got, want)
		}
	}
}

// --- the four log helpers ---------------------------------------------------

// Scenario: Cada helper deja su propio nivel, acción y carga.
//
// These four are the only things that write to the log, and each is a different
// question a user asks of it: what did I run, what broke, what did I connect to,
// what did it look at. The level is what the reader counts by, so a wrong level is
// a wrong count rather than a cosmetic problem.
func TestLogHelpers_EachOneSetsItsOwnLevelAndPayload(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	if err := l.LogQuery("SELECT 1", 1500*time.Millisecond, 42); err != nil {
		t.Fatal(err)
	}
	if err := l.LogError("SELECT 2", errors.New("relation does not exist")); err != nil {
		t.Fatal(err)
	}
	if err := l.LogConnect("mydb"); err != nil {
		t.Fatal(err)
	}
	if err := l.LogSchema("users"); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, filepath.Join(dir, datedName()))
	if len(lines) != 4 {
		t.Fatalf("the log holds %d entries, want 4", len(lines))
	}
	entries := make([]LogEntry, len(lines))
	for i, line := range lines {
		entries[i] = decodeLine(t, line)
	}

	if e := entries[0]; e.Level != LogQuery || e.Action != "execute" || e.SQL != "SELECT 1" || e.Duration != 1500 || e.Rows != 42 {
		t.Errorf("LogQuery wrote %+v", e)
	}
	if e := entries[1]; e.Level != LogError || e.Action != "execute" || e.SQL != "SELECT 2" || e.Error != "relation does not exist" {
		t.Errorf("LogError wrote %+v", e)
	}
	// A connect and a schema carry their subject in Metadata, because there is no
	// field for "what it was about" and inventing one per level would be worse.
	if e := entries[2]; e.Level != LogConnect || e.Action != "connect" || e.Metadata["database"] != "mydb" {
		t.Errorf("LogConnect wrote %+v", e)
	}
	if e := entries[3]; e.Level != LogSchema || e.Action != "schema" || e.Metadata["table"] != "users" {
		t.Errorf("LogSchema wrote %+v", e)
	}
}

// Scenario: La duración se guarda en milisegundos enteros, y se redondea hacia cero.
//
// The field is `duration_ms` and it is an int64, so a sub-millisecond duration is
// zero and 1.5ms is 1. That is lossy on purpose — the log is read by a person
// deciding whether a query was slow, and 0.5ms is not slow — but the rounding has to
// be toward zero rather than to nearest, or a burst of fast queries all read as 1ms
// and the number stops meaning anything.
func TestLogQuery_ConvertsDurationToWholeMilliseconds(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     int64
	}{
		{"a whole millisecond", time.Millisecond, 1},
		{"exactly one nanosecond rounds to zero", time.Nanosecond, 0},
		{"just under a millisecond rounds to zero", 999 * time.Microsecond, 0},
		{"just over a millisecond rounds to one", 1001 * time.Microsecond, 1},
		{"a microsecond rounds to zero", time.Microsecond, 0},
		{"a second and a half", 1500 * time.Millisecond, 1500},
		{"a minute", time.Minute, 60000},
		{"an hour", time.Hour, 3600000},
		{"zero", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := l.LogQuery("SELECT 1", tc.duration, 0); err != nil {
				t.Fatal(err)
			}
			lines := readLines(t, filepath.Join(dir, datedName()))
			entry := decodeLine(t, lines[len(lines)-1])
			if entry.Duration != tc.want {
				t.Errorf("LogQuery with %v wrote duration_ms %d, want %d", tc.duration, entry.Duration, tc.want)
			}
		})
	}
}

// Scenario: Las claves vacías no aparecen en el JSON.
//
// `omitempty` on SQL, Duration, Rows, Error and Metadata is what keeps a connect
// entry from carrying `"rows":0` and a query entry from carrying `"error":""`.
// That is not tidiness: a reader counting errors by the presence of the key would
// count every query if the key were always there, and a log that grows a key per
// entry is harder to read by eye.
//
// Asserted on the raw line, because the whole point is the shape of the bytes and
// decoding into a struct would hide it.
func TestLog_EmptyFieldsAreOmittedFromTheJSON(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	if err := l.LogConnect("mydb"); err != nil {
		t.Fatal(err)
	}
	if err := l.LogQuery("SELECT 1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := l.LogError("SELECT 2", errors.New("boom")); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, filepath.Join(dir, datedName()))

	t.Run("a connect entry carries no query fields at all", func(t *testing.T) {
		raw := lines[0]
		for _, absent := range []string{`"sql"`, `"duration_ms"`, `"rows"`, `"error"`} {
			if strings.Contains(raw, absent) {
				t.Errorf("the connect entry carries %s, which it has no value for: %s", absent, raw)
			}
		}
		// And the ones it does have.
		for _, present := range []string{`"level":"connect"`, `"action":"connect"`, `"metadata"`, `"database"`} {
			if !strings.Contains(raw, present) {
				t.Errorf("the connect entry is missing %s: %s", present, raw)
			}
		}
	})

	t.Run("a query with no rows omits rows, and keeps a zero duration absent", func(t *testing.T) {
		raw := lines[1]
		if strings.Contains(raw, `"rows"`) {
			t.Errorf("a query with no rows wrote a rows key: %s", raw)
		}
		if strings.Contains(raw, `"duration_ms"`) {
			t.Errorf("a zero-millisecond query wrote a duration key: %s", raw)
		}
		if strings.Contains(raw, `"error"`) {
			t.Errorf("a successful query wrote an error key: %s", raw)
		}
		if !strings.Contains(raw, `"level":"query"`) {
			t.Errorf("the query entry is missing its level: %s", raw)
		}
	})

	t.Run("an error entry carries the message and no rows", func(t *testing.T) {
		raw := lines[2]
		if !strings.Contains(raw, `"error":"boom"`) {
			t.Errorf("the error entry is missing its message: %s", raw)
		}
		if strings.Contains(raw, `"rows"`) {
			t.Errorf("an error entry wrote a rows key: %s", raw)
		}
	})
}

// Scenario: Un error nil revienta, y es mejor que una línea vacía.
//
// KNOWN BEHAVIOUR, pinned because the alternative is worse and because nothing in
// the signature says it.
//
// LogError takes an `error` and calls `err.Error()` on it with no nil check. A nil
// error therefore panics rather than writing a blank message, and the caller sees a
// stack trace instead of a log line that says nothing. The panic is the right
// outcome: a nil error at that call site is a bug, and a blank log line would hide
// it. What is pinned is that the behaviour is deliberate rather than an oversight
// nobody looked at.
func TestLogError_APanicsOnANilError(t *testing.T) {
	l, _ := newTestLogger(t, 7)

	defer func() {
		if recover() == nil {
			t.Error("LogError(nil) returned normally; it is recorded as panicking so that a nil error at the call site is loud")
		}
	}()
	_ = l.LogError("SELECT 1", nil)
}

// --- Close ------------------------------------------------------------------

// Scenario: Cerrar un logger sin archivo no revienta.
//
// `Close` guards on a nil file, which is reachable: a `Logger` built as a zero value
// rather than through NewLogger has one, and a caller that ignores NewLogger's error
// — which the tests above show produces a nil *Logger*, so the guard is also what
// keeps that from being a second panic — ends up here.
//
// The guard is only worth anything if it returns cleanly, which is what this says.
func TestClose_ANilFileIsNotAPanic(t *testing.T) {
	l := &Logger{}
	if err := l.Close(); err != nil {
		t.Errorf("Close on a zero Logger = %v, want nil: there is no file to close and that is not a failure", err)
	}
}

// Scenario: Cerrar dos veces avisa, y la primera es la que cuenta.
//
// A double close is a real mistake — it means something closed a logger it does not
// own — and the second one has to say so. Silently succeeding would let a use-after
// close look healthy while every later write fails.
func TestClose_TheSecondCloseReportsAnError(t *testing.T) {
	l, _ := newTestLogger(t, 7)

	if err := l.Close(); err != nil {
		t.Fatalf("the first Close = %v, want nil", err)
	}
	if err := l.Close(); err == nil {
		t.Error("the second Close returned nil; a use-after-close would look healthy")
	}
}

// --- Cleanup ----------------------------------------------------------------

// Scenario: Una retención de cero o menos no borra nada.
//
// Retention is configuration, and a user who sets it to 0 meaning "no limit" must
// not get a logger that deletes their entire history on the first cleanup. Zero and
// negative both mean off, and the guard has to catch both — a `< 0` check alone would
// let 0 through and 0 is the value people actually write.
func TestCleanup_ARetentionOfZeroOrLessDeletesNothing(t *testing.T) {
	for _, retention := range []int{0, -1, -30} {
		t.Run(fmt.Sprintf("retention=%d", retention), func(t *testing.T) {
			l, dir := newTestLogger(t, retention)
			now := time.Now()
			old := writeFile(t, dir, "ancient.jsonl", "{}\n", now.AddDate(0, 0, -400))
			recent := writeFile(t, dir, "yesterday.jsonl", "{}\n", now.Add(-time.Hour))

			if err := l.Cleanup(); err != nil {
				t.Fatalf("Cleanup with retention %d = %v, want it to be a no-op", retention, err)
			}
			for _, f := range []string{old, recent} {
				if _, err := os.Stat(f); err != nil {
					t.Errorf("retention %d deleted %s; it is recorded as deleting nothing", retention, filepath.Base(f))
				}
			}
		})
	}
}

// Scenario: Cleanup borra lo viejo y deja lo nuevo.
//
// The retention window is counted in days from now. What matters is that a file
// inside the window survives and one outside it does not: the sweep is what makes
// "keep the last N days" true, and a sweep that deleted a day early would throw away
// logs the user still considers current.
//
// There is deliberately NO case for a file sitting exactly on the cutoff. The cutoff
// is computed from time.Now() at the moment Cleanup runs, and the fixture is stamped
// from a time.Now() taken a moment earlier, so "exactly at the boundary" is a
// difference of microseconds and not a case a test can set up without racing the
// clock. The two cases here are a day clear on each side, which is what the window
// actually promises.
func TestCleanup_DeletesWhatIsOlderThanTheWindowAndKeepsTheRest(t *testing.T) {
	l, dir := newTestLogger(t, 7)
	now := time.Now()

	// Named after their age so a failure says which one went wrong.
	ancient := writeFile(t, dir, "0001-01-01.jsonl", "{}\n", now.AddDate(0, 0, -400))
	justInside := writeFile(t, dir, "0001-01-02.jsonl", "{}\n", now.AddDate(0, 0, -6))
	justOutside := writeFile(t, dir, "0001-01-03.jsonl", "{}\n", now.AddDate(0, 0, -8))
	today := writeFile(t, dir, "0001-01-04.jsonl", "{}\n", now)

	if err := l.Cleanup(); err != nil {
		t.Fatalf("Cleanup = %v", err)
	}

	if _, err := os.Stat(ancient); !os.IsNotExist(err) {
		t.Errorf("a file 400 days old survived a 7 day retention")
	}
	if _, err := os.Stat(justOutside); !os.IsNotExist(err) {
		t.Errorf("a file 8 days old survived a 7 day retention")
	}
	for _, f := range []string{justInside, today} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s was deleted, but it is inside the 7 day window", filepath.Base(f))
		}
	}
}

// Scenario: Cleanup borra cualquier archivo viejo, no solo los .jsonl.
//
// KNOWN BEHAVIOUR, pinned because it is a real hazard and not an oversight.
//
// The retention sweep has no extension check, so it deletes anything in the session
// dir older than the window: a `.gitkeep`, a lock file, a README the user dropped
// there. Cleanup is best-effort by design — the code says so where it ignores the
// error from Remove — so it will not ask.
//
// The mitigation is the directory: it is the app's own, created by NewLogger, and
// nothing else is supposed to live in it. That is a convention rather than an
// enforced boundary, so it is worth having on record.
func TestCleanup_DeletesEveryOldFileWhatextensionItHas(t *testing.T) {
	l, dir := newTestLogger(t, 7)
	old := time.Now().AddDate(0, 0, -400)

	keep := []string{
		"2020-01-01.jsonl",
		"notes.txt",
		".gitkeep",
		"README.md",
		"session.log",
		"archive.jsonl.gz", // still ends in .gz, so a suffix check would skip it
	}
	var paths []string
	for _, name := range keep {
		paths = append(paths, writeFile(t, dir, name, "x\n", old))
	}

	if err := l.Cleanup(); err != nil {
		t.Fatalf("Cleanup = %v", err)
	}

	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived; the sweep is recorded as having no extension check at all", filepath.Base(p))
		}
	}
}

// Scenario: Cleanup no toca los directorios, por viejos que estén.
//
// A subdirectory in the session dir is skipped rather than removed. Removing a
// directory with `os.Remove` only works when it is empty, so the guard is what keeps
// a nested tree from being half-deleted: the files inside go and the directory stays
// as an empty shell.
func TestCleanup_LeavesDirectoriesAlone(t *testing.T) {
	l, dir := newTestLogger(t, 7)

	old := time.Now().AddDate(0, 0, -400)
	sub := filepath.Join(dir, "ancient-dir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	// Backdate the directory itself; mtime is what the sweep reads.
	backdate(t, sub, old)
	inside := writeFile(t, sub, "2020-01-01.jsonl", "{}\n", old)

	if err := l.Cleanup(); err != nil {
		t.Fatalf("Cleanup = %v", err)
	}

	if _, err := os.Stat(sub); err != nil {
		t.Errorf("the subdirectory was removed: %v", err)
	}
	if _, err := os.Stat(inside); err != nil {
		t.Errorf("the file inside the subdirectory was removed, so the sweep descended into a directory it is recorded as skipping")
	}
}

// Scenario: Un archivo que no se puede borrar no convierte la limpieza en un fallo.
//
// The sweep is best-effort: the error from Remove is discarded and the loop carries
// on. That is the right choice for a housekeeping task — the alternative is a
// logger whose Cleanup returns an error every time one file turns out to be locked,
// which callers would learn to ignore, taking the real failures with it.
//
// Making the DIRECTORY read-only is how every removal fails at once. Making one
// file undeletable while its neighbours stay deletable is not possible here, and
// that is a fact about the platform rather than about the test: on Linux the
// permission to unlink a name comes from the containing directory, not from the
// file, so a file inside a writable directory can always be removed whatever its own
// mode says. Making one entry fail needs chattr +i or a mount point, neither of which
// belongs in a unit test.
//
// So the case here is the coarse one, and it is the one worth having: nothing is
// removable, the sweep runs to the end, and it reports success.
func TestCleanup_ARemovalFailureIsNotReportedAsAFailure(t *testing.T) {
	onlyAsRoot(t, "removing a file from a read-only directory")

	l, dir := newTestLogger(t, 7)
	old := time.Now().AddDate(0, 0, -400)
	var paths []string
	for _, name := range []string{"0000-a.jsonl", "0001-b.jsonl", "0002-c.jsonl", "0003-d.jsonl"} {
		paths = append(paths, writeFile(t, dir, name, "{}\n", old))
	}

	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	if err := l.Cleanup(); err != nil {
		t.Errorf("Cleanup = %v, want nil: a file it could not remove is not a failure of the task", err)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed from a read-only directory, so the test is not testing what it says", filepath.Base(p))
		}
	}
}

// Scenario: Un directorio de sesión que no existe se reporta, no se ignora.
//
// A missing directory is a configuration problem the user has to know about, and
// returning nil would make a typo in the config look like a successful cleanup. The
// error is passed through unwrapped, which is worth pinning because a wrapped one
// would have to be unwrapped by the caller to be useful.
func TestCleanup_ReportsAMissingDirectory(t *testing.T) {
	l, err := NewLogger(t.TempDir(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	missing := filepath.Join(t.TempDir(), "never-created")
	l.dir = missing

	got := l.Cleanup()
	if got == nil {
		t.Fatal("Cleanup on a missing directory returned nil; a typo in the config would look like a clean run")
	}
	if !errors.Is(got, os.ErrNotExist) {
		t.Errorf("Cleanup = %v, want it to wrap os.ErrNotExist so a caller can test for it", got)
	}
}

// --- writing fixtures -------------------------------------------------------

// writeFile creates a file with a given mtime, backdated so the retention sweep has
// something to act on. Named after the date because the sweep does not read
// filenames.
func writeFile(t *testing.T, dir, name, content string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	backdate(t, path, modTime)
	return path
}

// backdate sets a path's mtime. The sweep reads ModTime, so a fixture written "now"
// is inside every retention window and proves nothing.
func backdate(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

// --- the file-open failure --------------------------------------------------

// Scenario: Si el archivo de hoy es un directorio, el constructor falla.
//
// The dated file is opened with OpenFile, and a directory sitting where the log goes
// makes that fail. The constructor reports it rather than handing back a logger whose
// every write will fail later, while the caller is holding a database cursor.
//
// A directory is the trigger because it needs no permission bits, so this holds for
// any user including root.
func TestNewLogger_FailsWhenTodaysFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, datedName()), 0755); err != nil {
		t.Fatal(err)
	}

	l, err := NewLogger(dir, 7)
	if err == nil {
		_ = l.Close()
		t.Fatal("NewLogger succeeded with a directory where today's log file should be")
	}
	if l != nil {
		t.Errorf("NewLogger returned a logger (%+v) alongside its error", l)
	}
	if !strings.Contains(err.Error(), "session file") {
		t.Errorf("the error is %q, want it to name the session file", err)
	}
}

// --- ListSessions -----------------------------------------------------------

// entryJSON builds one log line with a chosen level and a chosen timestamp, so a
// test can control the file's contents without going through the Logger and its
// time.Now().
func entryJSON(level LogLevel, ts time.Time) string {
	e := LogEntry{Timestamp: ts, Level: level, Action: "execute", SQL: "SELECT 1"}
	b, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

// dated writes a dated log file with the given lines, and returns its name.
func dated(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0644); err != nil {
		t.Fatal(err)
	}
	return name + ".jsonl"
}

// brokenLink creates a symlink to a path that does not exist, under the given name.
// This is how a ReadFile failure is provoked without permission bits: the name is in
// the directory, it is not a directory itself, and following it fails. It works as
// root, unlike a mode-bit trick.
func brokenLink(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.Symlink(filepath.Join(dir, "no-such-target"), path); err != nil {
		t.Fatal(err)
	}
	return path
}

// Scenario: Solo se listan los .jsonl, y solo los que llevan una fecha.
//
// The session list is a menu, so anything in the directory that is not a session
// would show up as a row the user cannot open. Three filters apply, and each has its
// own reason: a subdirectory is not a session, a file that does not end in .jsonl is
// not the log format, and a name whose stem is not a date is not a session this code
// wrote — so it is skipped rather than listed with a zero date, which would render
// as the year 1.
func TestListSessions_ListsOnlyDatedJsonlFiles(t *testing.T) {
	dir := t.TempDir()

	dated(t, dir, "2024-03-01", entryJSON(LogQuery, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)))
	dated(t, dir, "2024-03-02", entryJSON(LogQuery, time.Date(2024, 3, 2, 10, 0, 0, 0, time.UTC)))

	// Filtered out by extension.
	for _, name := range []string{"notes.txt", "README.md", "session.json", "2024-03-03.jsonl.gz", "2024-03-04.jsonl.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Filtered out by extension even though the stem is a valid date.
	if err := os.WriteFile(filepath.Join(dir, "2024-03-05.json"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Filtered out by not being a date at all.
	for _, name := range []string{"not-a-date.jsonl", "20240101.jsonl", "2024-13-01.jsonl", ".jsonl", "2024-03-06.jsonl.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Filtered out by being a directory, even with a perfect name.
	if err := os.Mkdir(filepath.Join(dir, "2024-03-07.jsonl"), 0755); err != nil {
		t.Fatal(err)
	}

	r := NewReader(dir)
	sessions, err := r.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 2 {
		var got []string
		for _, si := range sessions {
			got = append(got, si.Filename)
		}
		t.Fatalf("ListSessions returned %d sessions %v, want the two dated logs", len(sessions), got)
	}
	for i, want := range []string{"2024-03-01.jsonl", "2024-03-02.jsonl"} {
		if sessions[i].Filename != want {
			t.Errorf("session %d is %q, want %q", i, sessions[i].Filename, want)
		}
	}
}

// Scenario: La sesión cuenta sus entradas, sus consultas y sus errores.
//
// These three counts are the whole reason to list sessions: a user scanning a month
// of work wants to know which days ran queries and which days broke. Each count is
// driven by the entry's LEVEL, not by its action or by its content, so a connect or
// a schema entry is neither a query nor an error and must not be counted as either.
func TestListSessions_CountsEntriesQueriesAndErrors(t *testing.T) {
	dir := t.TempDir()

	// One day of queries, one of errors, one that mixes everything.
	dated(t, dir, "2024-03-01",
		entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)),
		entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 1, 0, 0, time.UTC)),
		entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 2, 0, 0, time.UTC)),
	)
	dated(t, dir, "2024-03-02",
		entryJSON(LogError, time.Date(2024, 3, 2, 11, 0, 0, 0, time.UTC)),
		entryJSON(LogError, time.Date(2024, 3, 2, 11, 5, 0, 0, time.UTC)),
	)
	dated(t, dir, "2024-03-03",
		entryJSON(LogQuery, time.Date(2024, 3, 3, 8, 0, 0, 0, time.UTC)),
		entryJSON(LogError, time.Date(2024, 3, 3, 8, 1, 0, 0, time.UTC)),
		entryJSON(LogConnect, time.Date(2024, 3, 3, 8, 2, 0, 0, time.UTC)),
		entryJSON(LogSchema, time.Date(2024, 3, 3, 8, 3, 0, 0, time.UTC)),
	)

	r := NewReader(dir)
	sessions, err := r.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}

	byName := map[string]SessionInfo{}
	for _, si := range sessions {
		byName[si.Filename] = si
	}

	for _, tc := range []struct {
		filename               string
		entries, queries, errs int
	}{
		{"2024-03-01.jsonl", 3, 3, 0},
		{"2024-03-02.jsonl", 2, 0, 2},
		// Four entries, one query, one error: the connect and the schema are
		// counted in Entries and in neither of the other two.
		{"2024-03-03.jsonl", 4, 1, 1},
	} {
		si, ok := byName[tc.filename]
		if !ok {
			t.Errorf("%s is missing from the list", tc.filename)
			continue
		}
		if si.Entries != tc.entries || si.Queries != tc.queries || si.Errors != tc.errs {
			t.Errorf("%s: entries=%d queries=%d errors=%d, want %d/%d/%d",
				tc.filename, si.Entries, si.Queries, si.Errors, tc.entries, tc.queries, tc.errs)
		}
	}
}

// Scenario: El inicio y el fin vienen del PRIMERO y del ÚLTIMO, en el orden del archivo.
//
// The window is read positionally: first entry's timestamp to last entry's
// timestamp. It is not a min and a max, so a file whose entries are out of order —
// which happens if the clock moved, or if two processes wrote the same day — reports
// a window that ends before it starts.
//
// That is worth pinning rather than fixing here: a min/max would be a behaviour
// change, and the positional reading is defensible because entries are appended in
// write order and that IS the session. What matters is that the decision is on
// record, and that the last entry really is indexed at len-1 and not at len-2.
func TestListSessions_TheWindowIsTheFirstAndLastEntryInFileOrder(t *testing.T) {
	dir := t.TempDir()

	// Deliberately NOT in chronological order, and the LAST line is the MIDDLE
	// one. So the answer is neither the minimum nor the maximum: indexing
	// len-2 instead of len-1 would give the 11:00 entry, which is the one thing
	// the answer is most likely to be confused with.
	first := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	middle := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	latest := time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC)
	dated(t, dir, "2024-03-01",
		entryJSON(LogQuery, first),
		entryJSON(LogQuery, latest),
		entryJSON(LogQuery, middle),
	)

	r := NewReader(dir)
	sessions, err := r.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	si := sessions[0]

	if !si.StartTime.Equal(first) {
		t.Errorf("StartTime is %v, want the first entry's %v", si.StartTime, first)
	}
	// The last LINE, which is the 10:00 entry — not the latest timestamp (11:00)
	// and not the second entry (also 11:00).
	if !si.EndTime.Equal(middle) {
		t.Errorf("EndTime is %v, want the last LINE's %v: not the latest timestamp (%v) and not the second entry", si.EndTime, middle, latest)
	}
	if si.EndTime.Equal(latest) {
		t.Error("EndTime is the latest timestamp, so it is a max over the file rather than the last entry")
	}
	// The window runs backwards, and that is the recorded behaviour.
	if si.EndTime.Before(si.StartTime) {
		t.Logf("the window runs backwards (start %v, end %v), which is the recorded consequence of reading positionally", si.StartTime, si.EndTime)
	}
}

// Scenario: Un log vacío no tiene ventana, y no revienta.
//
// An empty log is a real state: the Logger opens today's file when the app starts and
// nothing may be written before the first query. The window is then the zero time,
// and indexing the first entry of an empty slice would panic — which is what a
// missing length check does.
func TestListSessions_AnEmptyLogHasNoWindowAndDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	for _, content := range []string{"", "\n", "\n\n\n"} {
		name := dated(t, dir, "2024-03-01")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		r := NewReader(dir)
		sessions, err := r.ListSessions()
		if err != nil {
			t.Fatalf("content %q: ListSessions = %v, want the empty log listed", content, err)
		}
		if len(sessions) != 1 {
			t.Fatalf("content %q: got %d sessions, want the empty log listed", content, len(sessions))
		}
		si := sessions[0]
		if si.Entries != 0 || si.Queries != 0 || si.Errors != 0 {
			t.Errorf("content %q: entries=%d queries=%d errors=%d, want all zero", content, si.Entries, si.Queries, si.Errors)
		}
		if !si.StartTime.IsZero() || !si.EndTime.IsZero() {
			t.Errorf("content %q: the window is %v..%v, want the zero time", content, si.StartTime, si.EndTime)
		}
		// The date still comes from the name, so the row is renderable.
		if si.Date.IsZero() {
			t.Errorf("content %q: the date is zero, so the row would render as the year 1", content)
		}
	}
}

// Scenario: Un log que no se puede leer se lista igual, con los contadores a cero.
//
// A session whose file cannot be read still happened and still occupies a day, so
// dropping it would make a day with a permissions problem look like a day with no
// work. Listing it with zero counts says "something is here, we could not read it",
// which is the truth a user can act on.
//
// The trigger is a symlink to a path that does not exist: the name is present, it is
// not a directory, its stem parses as a date, and following it fails. Unlike a
// mode-bit trick this behaves the same for root, so the case is real in CI.
func TestListSessions_AnUnreadableLogIsListedWithZeroCounts(t *testing.T) {
	dir := t.TempDir()
	dated(t, dir, "2024-03-01", entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)))
	brokenLink(t, dir, "2024-03-02.jsonl")

	r := NewReader(dir)
	sessions, err := r.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions = %v, want the unreadable session listed rather than an error", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want both days listed", len(sessions))
	}

	var broken SessionInfo
	for _, si := range sessions {
		if si.Filename == "2024-03-02.jsonl" {
			broken = si
		}
	}
	if broken.Filename == "" {
		t.Fatal("the unreadable session is not in the list")
	}
	if broken.Entries != 0 || broken.Queries != 0 || broken.Errors != 0 {
		t.Errorf("the unreadable session reports %d/%d/%d, want all zero", broken.Entries, broken.Queries, broken.Errors)
	}
	if !broken.Date.Equal(time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the unreadable session's date is %v, want the one from its filename", broken.Date)
	}
	// And the readable one is unaffected.
	for _, si := range sessions {
		if si.Filename == "2024-03-01.jsonl" && si.Entries != 1 {
			t.Errorf("the readable session reports %d entries, want 1: one unreadable file changed its neighbour", si.Entries)
		}
	}
}

// Scenario: Un directorio de sesiones que no existe se reporta.
//
// Same contract as Cleanup: a missing directory is a configuration problem, and
// returning an empty list with no error would make a typo look like "no sessions
// yet", which is a very different thing to tell a user.
func TestListSessions_ReportsAMissingDirectory(t *testing.T) {
	r := NewReader(filepath.Join(t.TempDir(), "never-created"))
	sessions, err := r.ListSessions()
	if err == nil {
		t.Fatalf("ListSessions on a missing directory returned %v and no error", sessions)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ListSessions = %v, want it to wrap os.ErrNotExist", err)
	}
}

// --- Read, ReadFile, ReadAll ------------------------------------------------

// Scenario: Leer por fecha y leer por nombre llegan al mismo archivo.
//
// Read exists so a caller holding a time.Time does not have to know the naming
// scheme, and it derives the name the same way the Logger writes it. If the two ever
// disagreed, a session would be listed and then fail to open — which is exactly what
// the broken-symlink test above is about, so this is the positive side of it.
func TestRead_ByDateAndByFilenameAgree(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2024, 3, 1, 9, 30, 0, 0, time.UTC)
	dated(t, dir, "2024-03-01", entryJSON(LogQuery, ts), entryJSON(LogError, ts.Add(time.Minute)))

	r := NewReader(dir)

	byDate, err := r.Read(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	byName, err := r.ReadFile("2024-03-01.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	if len(byDate) != 2 {
		t.Fatalf("Read returned %d entries, want 2", len(byDate))
	}
	if len(byDate) != len(byName) {
		t.Fatalf("Read returned %d entries and ReadFile %d, want the same file read two ways", len(byDate), len(byName))
	}
	// LogEntry holds a map, so it is not comparable with ==. Compare the fields
	// that identify an entry, and the metadata on its own.
	for i := range byDate {
		a, b := byDate[i], byName[i]
		if !a.Timestamp.Equal(b.Timestamp) || a.Level != b.Level || a.Action != b.Action ||
			a.SQL != b.SQL || a.Duration != b.Duration || a.Rows != b.Rows || a.Error != b.Error {
			t.Errorf("entry %d differs between Read and ReadFile:\n%+v\n%+v", i, a, b)
		}
		if fmt.Sprint(a.Metadata) != fmt.Sprint(b.Metadata) {
			t.Errorf("entry %d's metadata differs: %v then %v", i, a.Metadata, b.Metadata)
		}
	}
	if byDate[0].Level != LogQuery || byDate[1].Level != LogError {
		t.Errorf("the entries came back as %v/%v, want query/error in file order", byDate[0].Level, byDate[1].Level)
	}
	if !byDate[0].Timestamp.Equal(ts) {
		t.Errorf("the timestamp survived as %v, want %v", byDate[0].Timestamp, ts)
	}
}

// Scenario: Leer un archivo o una fecha que no existe se reporta.
//
// Both take a name from the caller and both must fail loudly rather than returning
// nothing, because every caller of these is about to replay what it read.
func TestRead_MissingIsAnError(t *testing.T) {
	dir := t.TempDir()
	r := NewReader(dir)

	if _, err := r.ReadFile("1999-01-01.jsonl"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile of a missing file = %v, want it to wrap os.ErrNotExist", err)
	}
	if _, err := r.Read(time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read of a missing date = %v, want it to wrap os.ErrNotExist", err)
	}
}

// Scenario: ReadAll junta todos los logs, y en orden de fecha.
//
// This is the "what did I do all week" read, so the order is part of the contract: the
// days come out oldest first, and within a day oldest first. That falls out of the
// directory being read in name order and the names being dates, which is a happy
// accident of the naming scheme rather than a sort — worth stating, because it is the
// property a reader depends on.
func TestReadAll_ConcatenatesEveryLogInDateOrder(t *testing.T) {
	dir := t.TempDir()
	// Written out of order on purpose: the order must come from the names.
	dated(t, dir, "2024-03-03", entryJSON(LogQuery, time.Date(2024, 3, 3, 12, 0, 0, 0, time.UTC)))
	dated(t, dir, "2024-03-01",
		entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)),
		entryJSON(LogQuery, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)),
	)
	dated(t, dir, "2024-03-02", entryJSON(LogError, time.Date(2024, 3, 2, 11, 0, 0, 0, time.UTC)))

	// Neither of these belongs in a replay.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "2024-03-04.jsonl"), 0755); err != nil {
		t.Fatal(err)
	}
	// A dated name that cannot be read: skipped, and the rest still come through.
	brokenLink(t, dir, "2024-03-05.jsonl")

	r := NewReader(dir)
	all, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 4 {
		t.Fatalf("ReadAll returned %d entries, want the 4 readable ones", len(all))
	}
	wantOrder := []time.Time{
		time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
		time.Date(2024, 3, 2, 11, 0, 0, 0, time.UTC),
		time.Date(2024, 3, 3, 12, 0, 0, 0, time.UTC),
	}
	for i, want := range wantOrder {
		if !all[i].Timestamp.Equal(want) {
			t.Errorf("entry %d is at %v, want %v: the days are not in date order", i, all[i].Timestamp, want)
		}
	}
}

// Scenario: Un directorio de sesiones vacío da cero entradas, no un error.
//
// ReadAll on a directory with no logs is the "first run" case and must be empty, not
// a failure. A fresh install has a session dir the Logger just created and nothing in
// it.
func TestReadAll_AnEmptyDirectoryIsEmptyNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	all, err := NewReader(dir).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll on a dir with no logs = %v, want nil", err)
	}
	if len(all) != 0 {
		t.Errorf("ReadAll returned %d entries from a dir with no logs", len(all))
	}

	if _, err := NewReader(filepath.Join(dir, "missing")).ReadAll(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadAll on a missing dir = %v, want it to wrap os.ErrNotExist", err)
	}
}

// --- parseEntries -----------------------------------------------------------

// Scenario: Una línea malformada se salta, y las demás se leen.
//
// The log is append-only and nothing ever rewrites it, so a partial line can only be
// the LAST one — a crash mid-write. Skipping it and keeping the rest is what makes a
// truncated log still replayable, and it is why the decoder cannot simply return the
// error: one bad line would make the whole session unreadable.
//
// Pinned on the malformed line in the middle as well, because that is the stronger
// version of the same contract: even a line that is not last does not stop the read.
func TestParseEntries_SkipsMalformedLinesAndKeepsTheRest(t *testing.T) {
	good1 := entryJSON(LogQuery, time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC))
	good2 := entryJSON(LogError, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC))

	for _, tc := range []struct {
		name  string
		input string
	}{
		{"a truncated object on its own line", good1 + `{"level":"query",` + "\n" + good2},
		{"a truncated object at the end", good1 + good2 + `{"level":"que`},
		{"a line that is not JSON at all", good1 + "not json\n" + good2},
		{"an empty line", good1 + "\n" + good2},
		{"a bare scalar", good1 + "42\n" + good2},
		{"a JSON array", good1 + "[1,2,3]\n" + good2},
		{"a JSON string", good1 + `"hello"` + "\n" + good2},
		// NOT skipped, and that is a Go quirk rather than a decision: Unmarshal
		// accepts the JSON literal null into a struct and leaves it zero, with no
		// error. It becomes an entry with a zero timestamp. Pinned by
		// TestParseEntries_NullBecomesAZeroEntry.
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := parseEntries([]byte(tc.input))
			if err != nil {
				t.Fatalf("parseEntries = %v, want nil: one bad line must not make the log unreadable", err)
			}
			if len(entries) != 2 {
				t.Fatalf("parseEntries returned %d entries, want the 2 well-formed ones", len(entries))
			}
			if entries[0].Level != LogQuery || entries[1].Level != LogError {
				t.Errorf("the surviving entries are %v/%v, want query/error", entries[0].Level, entries[1].Level)
			}
		})
	}
}

// Scenario: parseEntries nunca devuelve un error, ni siquiera con basura.
//
// Its signature promises an error and it cannot produce one: every decode failure is
// swallowed by the skip. That is a deliberate trade — a log is not something to fail
// a replay over — and it means every caller has an error check that can never fire.
//
// KNOWN BEHAVIOUR, pinned because the dead error return is a trap for the next
// reader: the `if err == nil` in ListSessions looks load-bearing and is not
// reachable from a malformed file. It is reachable from an unreadable one, which is
// what its own test uses.
func TestParseEntries_NeverReturnsAnError(t *testing.T) {
	for _, input := range []string{
		"", "\n", "garbage", "{", "{{{", `{"level":`, "[]", "\x00\x01\x02",
		`{"timestamp":"not-a-time","level":"query"}`,
		`{"rows":"not-a-number","level":"query"}`,
	} {
		entries, err := parseEntries([]byte(input))
		if err != nil {
			t.Errorf("parseEntries(%q) = %v, want nil: the function has no way to return an error", input, err)
		}
		if len(entries) > 0 {
			t.Errorf("parseEntries(%q) returned %d entries, want none", input, len(entries))
		}
	}
}

// Scenario: Una línea "null" se cuenta como una entrada, vacía.
//
// KNOWN BEHAVIOUR, pinned because it is the one input class that is neither skipped
// nor decoded into anything real, and because it is a Go quirk rather than a choice
// anyone made here.
//
// json.Unmarshal accepts the JSON literal null into a struct: it is a valid JSON value
// for any type, it leaves the target untouched, and it reports no error. So a line
// containing null becomes a LogEntry with a zero timestamp, a zero level and no SQL,
// and it is counted in the session's Entries — which is the one number the count is
// read for.
//
// The streaming decoder this replaced behaved the same way, so nothing regressed. The
// alternative is a special case for a literal that this code never writes, and a
// guard whose only purpose is to reject something a hand-edit or a third-party writer
// could produce is not obviously worth a branch. Recorded so that a session count
// that is one too high has a known cause.
func TestParseEntries_NullBecomesAZeroEntry(t *testing.T) {
	entries, err := parseEntries([]byte("null\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("parseEntries(%q) returned %d entries, want 1: null decodes without error", "null", len(entries))
	}
	e := entries[0]
	if !e.Timestamp.IsZero() || e.Level != "" || e.SQL != "" {
		t.Errorf("the entry is %+v, want the zero value", e)
	}
}

// --- the hang that used to be here -------------------------------------------

// Scenario: Ninguna línea malformada puede dejar la lectura colgada.
//
// The regression test for a real hang, and it is a watchdog rather than a plain call
// because a plain call cannot fail: the bug WAS a non-terminating loop, so the test
// that covers it has to be able to say "it did not come back" instead of hanging the
// suite until the go test timeout kills it and takes the whole run with it.
//
// What went wrong, for the record: the reader used to stream a json.Decoder over the
// whole buffer and `continue` on a decode error. On a SYNTAX error Decode returns
// without advancing the reader, so More() stayed true and the loop spun forever.
// "garbage", a truncated "{", a binary file named YYYY-MM-DD.jsonl — all of them
// hung. A log written by a process killed mid-write is precisely what this format is
// meant to survive, so the input was not exotic.
//
// The budget is 300ms and the check is real time, not a step count, because the
// failure mode is "does not terminate" and only a clock can see that. The goroutine
// is leaked on failure, which is the lesser evil: the alternative is the suite
// hanging, and a leaked spinning goroutine is what the old code did to a process.
func TestParseEntries_NoMalformedInputCanHangTheReader(t *testing.T) {
	for _, input := range []string{
		"garbage",
		"{",
		"{{{",
		`{"level":`,
		`{"level":"query",`,
		"[1,2,3]",
		`"hello"`,
		"\x00\x01\x02",
		"\xff\xfe\xfd",
		strings.Repeat("{", 1000),
		`{"timestamp":"nope","level":"query"}` + "\n" + "{",
		"{\n\n\n{",
		"[" + strings.Repeat(`{"level":"query"},`, 100) + "[",
	} {
		t.Run(fmt.Sprintf("%q", truncateForName(input)), func(t *testing.T) {
			done := make(chan int, 1)
			go func() {
				entries, err := parseEntries([]byte(input))
				if err != nil {
					done <- -1
					return
				}
				done <- len(entries)
			}()

			select {
			case n := <-done:
				t.Logf("returned %d entries", n)
			case <-time.After(300 * time.Millisecond):
				t.Errorf("parseEntries(%q) had not returned after 300ms; the reader does not terminate on this input", truncateForName(input))
			}
		})
	}
}

// truncateForName keeps a subtest name short enough to read.
func truncateForName(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
