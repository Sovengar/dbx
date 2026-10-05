package app

// Scenario: Exportar decide entre portapapeles y fichero, y esa decision depende de
// CUANTAS filas hay.
//
// handleExport is the last thing that happens when the user picks a format in the export
// picker, and it branches on something the user cannot see: a single row always goes to
// the clipboard, several rows always go to a FILE unless they came from a yank. Both
// branches are reachable, the decision is written twice (once per row-count), and neither
// had a test.
//
// The failure modes are all user-visible and all silent. A file written to the wrong
// directory is found later by `ls`. A yank that wrote a file instead of copying puts the
// clipboard contents of the user's next copy somewhere on disk. And a format whose switch
// arm is missing produces an EMPTY file, which looks like the export worked.

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// clipboardStub puts a stub named `name` at the FRONT of the real PATH, writing its stdin
// to the file named by CLIP_OUT. Prepended rather than replacing, because the stub is a
// shell script that runs cat.
func clipboardStub(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat > \"$CLIP_OUT\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "")

	out := filepath.Join(dir, "clip")
	t.Setenv("CLIP_OUT", out)
	return out
}

func clipboardContents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing was copied: %v", err)
	}
	return string(data)
}

// exportModel is a model whose project directory is a temp dir, so a written file lands
// somewhere disposable.
func exportModel(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.project = foundProjectIn(t.TempDir())
	return m
}

func TestASingleRowExportAlwaysGoesToTheClipboard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format grid.ExportFormat
		check  func(*testing.T, string)
	}{
		{"SQL", grid.ExportSQL, func(t *testing.T, got string) {
			if !strings.Contains(got, "INSERT INTO") {
				t.Errorf("the clipboard does not hold an INSERT:\n%s", got)
			}
			assertEvenQuotes(t, got)
		}},
		{"JSON", grid.ExportJSON, func(t *testing.T, got string) {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(got), &obj); err != nil {
				t.Errorf("the clipboard does not hold a JSON object: %v\n%s", err, got)
			}
			if obj["name"] != "ada" {
				t.Errorf("the object is %v", obj)
			}
		}},
		{"CSV", grid.ExportCSV, func(t *testing.T, got string) {
			records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
			if err != nil {
				t.Errorf("the clipboard does not hold CSV: %v\n%q", err, got)
				return
			}
			if len(records) != 2 || records[0][0] != "id" {
				t.Errorf("the CSV is %q", records)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clip := clipboardStub(t, dir, "xclip")
			m := exportModel(t)

			done, ok := m.handleExport(grid.ExportSelectedMsg{
				Format: tc.format, Schema: "public", Table: "users",
				Columns: []string{"id", "name"},
				Row:     []interface{}{int64(1), "ada"},
			})().(exportDoneMsg)
			if !ok {
				t.Fatal("the command did not produce an exportDoneMsg")
			}
			if done.err != nil {
				t.Fatalf("the export failed: %v", done.err)
			}
			if !done.clipboard {
				t.Error("a single row export did not go to the clipboard")
			}
			if done.filename != "" {
				t.Errorf("a single row export also wrote the file %q", done.filename)
			}
			tc.check(t, clipboardContents(t, clip))

			// And nothing on disk.
			entries, err := os.ReadDir(m.project.Path)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a single row export wrote %v", entries)
			}
		})
	}
}

func TestSeveralRowsGoToAFileUnlessItIsAYank(t *testing.T) {
	for _, tc := range []struct {
		name    string
		format  grid.ExportFormat
		wantExt string
		check   func(*testing.T, string)
	}{
		{"SQL", grid.ExportSQL, ".sql", func(t *testing.T, got string) {
			if !strings.Contains(got, "INSERT INTO") {
				t.Errorf("the file does not hold an INSERT:\n%s", got)
			}
			// One INSERT with two value tuples, not two INSERTs. The count is of TUPLES,
			// not statements: this export hoists the column list out once, so a count of
			// ");" is always zero and asserting on it would have passed whatever came out.
			if n := strings.Count(got, "\n  ("); n != 2 {
				t.Errorf("the file holds %d value tuples, want 2:\n%s", n, got)
			}
			if strings.Count(got, "INSERT INTO") != 1 {
				t.Errorf("the file has %d INSERT headers, want the column list hoisted once:\n%s", strings.Count(got, "INSERT INTO"), got)
			}
			assertEveryTupleMatchesColumnCount(t, got, 2)
			assertEvenQuotes(t, got)
		}},
		{"JSON", grid.ExportJSON, ".json", func(t *testing.T, got string) {
			var rows []map[string]interface{}
			if err := json.Unmarshal([]byte(got), &rows); err != nil {
				t.Errorf("the file does not hold a JSON list: %v", err)
				return
			}
			if len(rows) != 2 {
				t.Errorf("the file holds %d rows, want 2", len(rows))
			}
		}},
		{"CSV", grid.ExportCSV, ".csv", func(t *testing.T, got string) {
			records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
			if err != nil {
				t.Errorf("the file does not hold CSV: %v", err)
				return
			}
			if len(records) != 3 {
				t.Errorf("the file holds %d records, want a header and two rows", len(records))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No clipboard stub: if the export tried the clipboard the command would fail
			// and the test would notice, so PATH being empty of clipboard tools is itself
			// the assertion that this branch writes a file.
			t.Setenv("PATH", t.TempDir())
			t.Setenv("WAYLAND_DISPLAY", "")

			m := exportModel(t)
			done, ok := m.handleExport(grid.ExportSelectedMsg{
				Format: tc.format, Schema: "public", Table: "users",
				Columns: []string{"id", "name"},
				Rows: [][]interface{}{
					{int64(1), "ada"},
					{int64(2), "grace"},
				},
			})().(exportDoneMsg)
			if !ok {
				t.Fatal("the command did not produce an exportDoneMsg")
			}
			if done.err != nil {
				t.Fatalf("the export failed: %v", done.err)
			}
			if done.clipboard {
				t.Error("a multi-row export went to the clipboard instead of a file")
			}
			if filepath.Ext(done.filename) != tc.wantExt {
				t.Errorf("the file is %q, want the extension %s", done.filename, tc.wantExt)
			}
			if got := filepath.Base(done.filename); got != "public_users"+tc.wantExt {
				t.Errorf("the file is named %q, want the schema and table", got)
			}
			// It landed in the project directory, which is what makes it findable.
			if filepath.Dir(done.filename) != m.project.Path {
				t.Errorf("the file is in %s, want the project directory %s", filepath.Dir(done.filename), m.project.Path)
			}
			tc.check(t, clipboardContents(t, done.filename))
		})
	}

	t.Run("yank mode sends several rows to the clipboard and says how many", func(t *testing.T) {
		dir := t.TempDir()
		clip := clipboardStub(t, dir, "xclip")
		m := exportModel(t)

		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users", YankMode: true,
			Columns: []string{"id", "name"},
			Rows:    [][]interface{}{{int64(1), "ada"}, {int64(2), "grace"}, {int64(3), "hopper"}},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if done.err != nil {
			t.Fatalf("the yank failed: %v", done.err)
		}
		if !done.clipboard {
			t.Error("a yank of several rows went to a file")
		}
		// The count is what the toast says, and "Yanked 1 row" when three were yanked is
		// the kind of small wrongness that makes a user distrust the rest of the app.
		if done.yankCount != 3 {
			t.Errorf("the yank reported %d rows, want 3", done.yankCount)
		}
		if entries, err := os.ReadDir(m.project.Path); err == nil && len(entries) != 0 {
			t.Errorf("a yank also wrote %v", entries)
		}
		got := clipboardContents(t, clip)
		records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
		if err != nil {
			t.Fatalf("the clipboard does not hold CSV: %v", err)
		}
		if len(records) != 4 {
			t.Errorf("the clipboard holds %d records, want a header and three rows", len(records))
		}
	})

	t.Run("a SINGLE row yank reports no count, because the count is for a batch", func(t *testing.T) {
		// The single-row arm returns before the yank count is computed, so a one-row yank
		// reports zero. The toast presumably says something else for that case; asserted
		// here so the asymmetry is on the record rather than inferred.
		dir := t.TempDir()
		clipboardStub(t, dir, "xclip")
		m := exportModel(t)

		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users", YankMode: true,
			Columns: []string{"id"}, Row: []interface{}{int64(1)},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if !done.clipboard {
			t.Error("a single-row yank did not go to the clipboard")
		}
		if done.yankCount != 0 {
			t.Errorf("a single-row yank reported a count of %d", done.yankCount)
		}
	})
}

func TestAnExportWithNothingToExportSaysSo(t *testing.T) {
	// Both Row and Rows nil: the picker's confirm reached the handler with no selection.
	// The message names the problem rather than writing an empty file, which would look
	// like a successful export of an empty table.
	m := exportModel(t)
	done, ok := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCSV, Schema: "public", Table: "users",
		Columns: []string{"id"},
	})().(exportDoneMsg)
	if !ok {
		t.Fatal("the command did not produce an exportDoneMsg")
	}
	if done.err == nil {
		t.Fatal("exporting nothing returned no error")
	}
	if !strings.Contains(done.err.Error(), "no data") {
		t.Errorf("the error is %q; it should say there is nothing to export", done.err)
	}
	if entries, err := os.ReadDir(m.project.Path); err == nil && len(entries) != 0 {
		t.Errorf("exporting nothing wrote %v", entries)
	}
}

func TestAFailedExportIsReportedRatherThanSwallowed(t *testing.T) {
	t.Run("a directory that cannot be written to", func(t *testing.T) {
		// The project path pointed at a FILE, so writing the export inside it fails.
		// A silent failure here means the user pressed export, saw nothing happen, and
		// has no idea whether the data was copied.
		blocked := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		m := routerModelLoaded(t)
		m.project = foundProjectIn(blocked)

		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users",
			Columns: []string{"id"},
			Rows:    [][]interface{}{{int64(1)}},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if done.err == nil {
			t.Fatal("writing into a file returned no error")
		}
		if done.filename != "" {
			t.Errorf("a failed write reported the filename %q", done.filename)
		}
	})

	t.Run("no clipboard tool", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv("WAYLAND_DISPLAY", "")

		m := exportModel(t)
		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users",
			Columns: []string{"id"}, Row: []interface{}{int64(1)},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if done.err == nil {
			t.Fatal("copying with no clipboard tool returned no error")
		}
		if done.clipboard {
			t.Error("a failed copy reported success")
		}
	})
}

// foundProjectIn is a project pointing at an existing directory.
func foundProjectIn(dir string) *config.FoundProject {
	return &config.FoundProject{Name: "p", Path: dir, Active: true}
}
