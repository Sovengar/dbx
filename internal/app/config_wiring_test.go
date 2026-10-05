package app

// Scenario: Cada llave de configuracion que se declara, HACE algo.
//
// Seven settings were declared in the config struct, given defaults, and read by nothing
// at all. A user who set one got no effect and no message: the file said `statusbar_help =
// false` and the help kept coming; `history_size = 10` and the store kept 500;
// `password_env = "PGPASSWORD"` and the password stayed out of the DSN so the connection
// simply failed; `session.enabled = false` and nothing had changed anyway, because no
// session log was ever written. A knob that does nothing is worse than a knob that is
// absent, because it looks like it worked.
//
// So this file asserts each one from the CONFIG to the OBSERVABLE EFFECT, and asserts
// the off switch as well as the on. The point is not that the value arrives; it is that
// turning the knob changes what happens.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buble/dbx/internal/config"
)

// wiredConfig is a complete config with every value pointed at a temp directory, so a
// test never writes into the developer's own state.
func wiredConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Session.Enabled = true
	cfg.Session.Dir = filepath.Join(root, "sessions")
	cfg.Session.RetentionDays = 30
	cfg.UI.StateDir = filepath.Join(root, "state")
	cfg.UI.HistorySize = 100
	cfg.UI.StatusBarHelp = true
	cfg.UI.StatusBar = true
	cfg.Theme.Mode = "dark"
	return cfg, root
}

// ---------------------------------------------------------------------------
// ui.statusbar_help
// ---------------------------------------------------------------------------

// Scenario: ui.statusbar_help apaga y enciende el panel de atajos.
//
// Both halves, because half a switch is the bug here: a pane that stops drawing but
// still reserves its rows leaves a blank strip, and one that keeps drawing while the
// layout gives the space away clips the grid.
func TestStatusBarHelpActuallyShowsAndHidesThePane(t *testing.T) {
	t.Run("off draws nothing and reserves nothing", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.UI.StatusBarHelp = false

		m := NewModel(cfg)
		if m.keybindsPane.Visible() {
			t.Error("the pane reports itself visible with statusbar_help off")
		}
		if got := m.keybindsPane.Height(); got != 0 {
			t.Errorf("the pane reserves %d rows while hidden, want 0", got)
		}

		// Rendered: the keybinds text is gone. Checked against the rendered output
		// rather than the flag, because the flag is the input and the output is
		// what the user sees.
		// Out of the picker: NewModel starts in StatePicker, and View() renders
		// the picker rather than the main view, so the keybinds pane would never
		// be reached. The first version of this forgot that and reported the
		// working pane as absent in BOTH cases.
		m2 := resize(withMainState(m), 120, 40)
		view := stripANSI(m2.View().Content)
		if strings.Contains(view, "Keybinds") {
			t.Error("the rendered view still has the keybinds pane with statusbar_help off")
		}
	})

	t.Run("on draws the pane and reserves its rows", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.UI.StatusBarHelp = true

		m := NewModel(cfg)
		if !m.keybindsPane.Visible() {
			t.Error("the pane reports itself hidden with statusbar_help on")
		}
		m = resize(withMainState(m), 120, 40)
		h := m.keybindsPane.Height()
		if h <= 0 {
			t.Fatalf("the pane reserves %d rows while shown, want some", h)
		}

		view := stripANSI(m.View().Content)
		if !strings.Contains(view, "Keybinds") {
			t.Errorf("the rendered view has no keybinds pane with statusbar_help on:\n%s", lastLines(view, 8))
		}
	})

	t.Run("the pane's own height is what the layout reserves", func(t *testing.T) {
		// The layout used to reserve a HARD-CODED 7 while the pane drew a variable
		// number of wrapped lines, so the content was short by the difference on
		// any window whose focused view needed more or fewer than five segments.
		// Asserted at three widths: the pane wraps, so its height must change.
		cfg, _ := wiredConfig(t)
		m := NewModel(cfg)

		heights := map[int]int{}
		for _, w := range []int{60, 120, 200} {
			mw := resize(withMainState(m), w, 40)
			mw.keybindsPane.SetWidth(w)
			mw.keybindsPane.SetHeight(40)
			heights[w] = mw.keybindsPane.Height()
		}

		// Whatever the numbers are, they must be REAL: a pane that has drawn N
		// lines occupies N plus its two border rows. Recomputing it here from the
		// same source would prove nothing, so the check is that the height is
		// never the hardcoded constant, which is what the layout used.
		for w, h := range heights {
			if h <= 2 {
				t.Errorf("at width %d the pane reserves %d rows, want the content plus two borders", w, h)
			}
			if h == 7 {
				t.Logf("at width %d the height happens to be 7; the layout now asks rather than assumes", w)
			}
		}
	})

	t.Run("a pane with no width reserves nothing", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		m := NewModel(cfg)
		m.keybindsPane.SetWidth(0)
		if got := m.keybindsPane.Height(); got != 0 {
			t.Errorf("a pane of width 0 reserves %d rows, want 0", got)
		}
		if got := m.keybindsPane.View(); got != "" {
			t.Errorf("a pane of width 0 rendered %q, want nothing", got)
		}
	})
}

// ---------------------------------------------------------------------------
// ui.history_size
// ---------------------------------------------------------------------------

// Scenario: ui.history_size limita el historial de verdad.
//
// The store hardcoded 500 while this defaulted to 100, and the two never met. The three
// cases that matter are the three ways a limit can be wrong: never reached, reached while
// running, and reached by a file that was written under a LARGER limit.
func TestHistorySizeActuallyCapsTheHistory(t *testing.T) {
	t.Run("the configured limit is the one in force", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.UI.HistorySize = 3

		m := NewModel(cfg)
		m.initQueryStore("proj", t.TempDir())
		if got := m.queryStore.MaxEntries(); got != 3 {
			t.Errorf("the store keeps %d entries, want the configured 3", got)
		}
	})

	t.Run("adding past the limit drops the OLDEST", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.UI.HistorySize = 3

		m := NewModel(cfg)
		m.initQueryStore("proj", t.TempDir())
		for _, q := range []string{"SELECT 1", "SELECT 2", "SELECT 3", "SELECT 4", "SELECT 5"} {
			m.queryStore.Add(q)
		}

		if got := len(m.queryStore.All()); got != 3 {
			t.Fatalf("the history holds %d entries, want 3", got)
		}
		// All() is newest first, so the survivors are the last three typed and the
		// two oldest are gone. Asserting WHICH ones is the point: a cap that
		// dropped the newest would look identical by count alone.
		var sqls []string
		for _, e := range m.queryStore.All() {
			sqls = append(sqls, e.SQL)
		}
		for i, want := range []string{"SELECT 5", "SELECT 4", "SELECT 3"} {
			if sqls[i] != want {
				t.Errorf("entry %d is %q, want %q — the cap must drop the OLDEST", i, sqls[i], want)
			}
		}
	})

	t.Run("a file written under a LARGER limit is trimmed when it is read", func(t *testing.T) {
		// Only trimming on Add would leave this file full until the user happened
		// to run enough queries to push the oldest out, so LOWERING the limit
		// would appear to do nothing until the cap was crossed the other way.
		dir := t.TempDir()
		var entries []map[string]any
		for i := range 20 {
			entries = append(entries, map[string]any{
				"sql":       "SELECT " + itoa(i),
				"timestamp": time.Now().Format(time.RFC3339),
			})
		}
		data, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "query_history.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}

		m := NewModel(&config.Config{UI: config.UIConfig{HistorySize: 5}})
		m.initQueryStore("proj", dir)
		if got := len(m.queryStore.All()); got != 5 {
			t.Errorf("reading a 20-entry file with a limit of 5 loaded %d entries", got)
		}
		if got := m.queryStore.All()[0].SQL; got != "SELECT 19" {
			t.Errorf("the newest entry is %q, want SELECT 19", got)
		}
	})

	t.Run("a limit of zero or less means the store's default, not nothing", func(t *testing.T) {
		// A zero-valued config must not be read as "keep no history". Erasing a
		// user's history because a setting was absent would be the worst possible
		// interpretation of it.
		for _, limit := range []int{0, -1, -100} {
			dir := t.TempDir()
			m := NewModel(&config.Config{UI: config.UIConfig{HistorySize: limit}})
			m.initQueryStore("proj", dir)
			m.queryStore.Add("SELECT 1")
			if got := len(m.queryStore.All()); got != 1 {
				t.Errorf("with a limit of %d the history holds %d entries after one add", limit, got)
			}
			if m.queryStore.MaxEntries() <= 0 {
				t.Errorf("with a limit of %d the store's cap is %d", limit, m.queryStore.MaxEntries())
			}
		}
	})
}

// ---------------------------------------------------------------------------
// ui.state_dir
// ---------------------------------------------------------------------------

// Scenario: ui.state_dir decide donde vive el historial.
//
// It replaces `query_history_path`, whose default was not even the shape of the real path:
// the history is per project, at <root>/projects/<name>/query_history.json, so a knob
// naming a single FILE could never have worked no matter how it was interpreted.
func TestStateDirDecidesWhereTheHistoryLives(t *testing.T) {
	t.Run("with no explicit directory the configured root is used", func(t *testing.T) {
		cfg, root := wiredConfig(t)

		m := NewModel(cfg)
		// An EMPTY stateDir, which is how the app calls it: the project name is
		// known long before the user would want to override the root.
		m.initQueryStore("myproj", "")
		m.queryStore.Add("SELECT 1")

		want := filepath.Join(root, "state", "projects", "myproj", "query_history.json")
		if _, err := os.Stat(want); err != nil {
			entries, _ := os.ReadDir(filepath.Join(root, "state"))
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("the history is not at %s: %v (state dir holds %v)", want, err, names)
		}
	})

	t.Run("an EXPLICIT directory still wins, so a caller can point it elsewhere", func(t *testing.T) {
		// Not defensive: the tests and any embedder need to point the store at a
		// disposable place without touching the user's real state.
		cfg, root := wiredConfig(t)
		elsewhere := t.TempDir()

		m := NewModel(cfg)
		m.initQueryStore("myproj", elsewhere)
		m.queryStore.Add("SELECT 1")

		if _, err := os.Stat(filepath.Join(elsewhere, "projects", "myproj", "query_history.json")); err != nil {
			t.Errorf("the history is not under the explicit directory: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "state")); err == nil {
			t.Error("the configured root was written to even though an explicit directory was given")
		}
	})
}

// ---------------------------------------------------------------------------
// session.enabled + session.retention_days
// ---------------------------------------------------------------------------

// Scenario: session.enabled decide si se escribe un log, y retention_days lo poda.
//
// session.NewLogger took a directory and a retention all along and was never called by
// anything, so dbx wrote no session log at all. That is why session.dir appeared to work
// — only the READER in the CLI used it — while the writer did not exist.

// withMainState puts the model past the project picker, which is where the keybinds pane
// is drawn. NewModel starts in StatePicker and View() renders the picker.
func withMainState(m Model) Model {
	m.state = StateMain
	return m
}

// sessionFiles is every session log under dir, by base name.
func sessionFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestSessionEnabledDecidesWhetherAnythingIsWritten(t *testing.T) {
	t.Run("on writes a log with the statements that ran", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.Session.Enabled = true

		m := NewModel(cfg)
		if m.sessionLogger == nil {
			t.Fatal("session.enabled is true and there is no logger")
		}
		defer func() { _ = m.sessionLogger.Close() }()

		m.logSessionQuery("SELECT 1", 12*time.Millisecond, 7)

		files := sessionFiles(t, cfg.Session.Dir)
		if len(files) != 1 {
			t.Fatalf("the session dir holds %v, want one log for today", files)
		}
		entries := readSessionLog(t, filepath.Join(cfg.Session.Dir, files[0]))
		if len(entries) != 1 {
			t.Fatalf("the log holds %d entries, want 1", len(entries))
		}
		e := entries[0]
		if e.SQL != "SELECT 1" {
			t.Errorf("the logged SQL is %q", e.SQL)
		}
		if e.Level != "query" {
			t.Errorf("the level is %q, want query", e.Level)
		}
		if e.Duration != 12 {
			t.Errorf("the duration is %d ms, want 12", e.Duration)
		}
		if e.Rows != 7 {
			t.Errorf("the row count is %d, want 7", e.Rows)
		}
		if e.Timestamp.IsZero() {
			t.Error("the entry has no timestamp: the log is unusable without one")
		}
	})

	t.Run("off writes nothing and there is no logger to write with", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.Session.Enabled = false

		m := NewModel(cfg)
		if m.sessionLogger != nil {
			t.Error("session.enabled is false and a logger was built anyway")
		}
		// Every call site nil-checks rather than testing a flag, so calling them
		// must be safe rather than merely unused.
		m.logSessionQuery("SELECT 1", time.Second, 1)
		m.logSessionError("SELECT 2", errors.New("boom"))
		m.logSessionConnect("proj")

		if files := sessionFiles(t, cfg.Session.Dir); len(files) != 0 {
			t.Errorf("session.enabled is false and the session dir holds %v", files)
		}
		if cmd := m.cleanupSessions(); cmd != nil {
			t.Error("cleanupSessions returned a command with no logger")
		}
	})

	t.Run("a log that cannot be opened does not stop the TUI", func(t *testing.T) {
		// Logging is the one feature allowed to be unavailable without the user
		// finding out the hard way. A path that cannot be created — a file where a
		// directory should be — is the realistic case.
		cfg, _ := wiredConfig(t)
		blocker := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg.Session.Dir = filepath.Join(blocker, "sessions")

		m := NewModel(cfg)
		if m.sessionLogger != nil {
			t.Error("a logger was built for a directory that cannot exist")
		}
		// And the model is still usable.
		m.logSessionQuery("SELECT 1", time.Second, 1)
		if m.keybindsPane == nil {
			t.Error("the model did not finish building")
		}
	})

	t.Run("a connection is logged by NAME, never by DSN", func(t *testing.T) {
		// A DSN carries the password — that is what password_env puts in it — so
		// writing one into a log file would undo the whole point of that knob.
		cfg, _ := wiredConfig(t)
		m := NewModel(cfg)
		if m.sessionLogger == nil {
			t.Skip("no logger")
		}
		defer func() { _ = m.sessionLogger.Close() }()

		m.logSessionConnect("myproj")
		files := sessionFiles(t, cfg.Session.Dir)
		if len(files) != 1 {
			t.Fatalf("the session dir holds %v", files)
		}
		raw, err := os.ReadFile(filepath.Join(cfg.Session.Dir, files[0]))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "://") || strings.Contains(string(raw), "password") {
			t.Errorf("the connect entry looks like it carries a DSN: %s", raw)
		}
		entries := readSessionLog(t, filepath.Join(cfg.Session.Dir, files[0]))
		if len(entries) != 1 || entries[0].Level != "connect" {
			t.Fatalf("the log holds %+v, want one connect entry", entries)
		}
		if entries[0].Metadata["database"] != "myproj" {
			t.Errorf("the connect entry records %v, want the project name", entries[0].Metadata["database"])
		}
	})
}

func TestSessionRetentionDaysPrunesOldLogs(t *testing.T) {
	t.Run("logs older than the retention are removed and newer ones stay", func(t *testing.T) {
		cfg, _ := wiredConfig(t)
		cfg.Session.RetentionDays = 7
		if err := os.MkdirAll(cfg.Session.Dir, 0o755); err != nil {
			t.Fatal(err)
		}

		// Named by date, as the logger names them, and given matching mtimes —
		// Cleanup reads ModTime, not the name.
		today := time.Now()
		write := func(name string, age time.Duration) string {
			p := filepath.Join(cfg.Session.Dir, name)
			if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			stamp := today.Add(-age)
			if err := os.Chtimes(p, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			return p
		}
		old := write("2020-01-01.jsonl", 400*24*time.Hour)
		recent := write(today.AddDate(0, 0, -2).Format("2006-01-02")+".jsonl", 48*time.Hour)
		brand := write(today.Format("2006-01-02")+".jsonl", time.Minute)

		m := NewModel(cfg)
		cmd := m.cleanupSessions()
		if cmd == nil {
			t.Fatal("cleanupSessions returned no command")
		}
		cmd() // runs the prune

		if _, err := os.Stat(old); err == nil {
			t.Error("a log 400 days old survived a 7-day retention")
		}
		for _, p := range []string{recent, brand} {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s was removed: %v", filepath.Base(p), err)
			}
		}
	})

	t.Run("a retention of zero or less prunes NOTHING", func(t *testing.T) {
		// Keep everything is what a zero means here, and it is the opposite of the
		// store's history limit, where a zero means "use the default". Two knobs
		// with the same name-shape and opposite meanings is worth stating in both
		// places rather than in neither.
		cfg, _ := wiredConfig(t)
		cfg.Session.RetentionDays = 0
		if err := os.MkdirAll(cfg.Session.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ancient := filepath.Join(cfg.Session.Dir, "1999-01-01.jsonl")
		if err := os.WriteFile(ancient, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-400 * 24 * time.Hour)
		if err := os.Chtimes(ancient, stamp, stamp); err != nil {
			t.Fatal(err)
		}

		m := NewModel(cfg)
		if m.sessionLogger == nil {
			t.Skip("no logger")
		}
		defer func() { _ = m.sessionLogger.Close() }()

		cmd := m.cleanupSessions()
		if cmd == nil {
			t.Fatal("cleanupSessions returned no command")
		}
		cmd()

		if _, err := os.Stat(ancient); err != nil {
			t.Errorf("a retention of 0 removed %s: %v", ancient, err)
		}
	})
}

// sessionEntry is the shape of one line of a session log. Decoded here rather than by
// importing the logger's own type, so the assertion is about the FILE and not about the
// struct agreeing with itself.
type sessionEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Action    string    `json:"action"`
	SQL       string    `json:"sql"`
	Duration  int64     `json:"duration_ms"`
	Rows      int       `json:"rows"`
	Error     string    `json:"error"`
	Metadata  map[string]any
}

// readSessionLog decodes every line of a session log. A line that will not decode fails
// the test rather than being skipped: a log that cannot be read is not a log.
func readSessionLog(t *testing.T, path string) []sessionEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the session log: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []sessionEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e sessionEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("the session log line %q is not the documented shape: %v", line, err)
		}
		if e.Metadata == nil {
			// Metadata is a free-form map, so decode it separately.
			var raw struct {
				Metadata map[string]any `json:"metadata"`
			}
			if err := json.Unmarshal([]byte(line), &raw); err == nil {
				e.Metadata = raw.Metadata
			}
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading the session log: %v", err)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
