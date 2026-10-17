package app

// The external editor is the one app action whose result is produced by a
// callback the tea runtime owns, so its two outcomes had never been exercised:
// the callback only runs inside a Program. The readback is a method now, which
// makes both outcomes — the file that came back and the file that did not —
// reachable from a test, and the action's own paths (no temp dir, no EDITOR)
// are driven through the real dispatch table.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheExternalEditorReadbackReturnsTheEditedSQLOrTheOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "query.sql")
	if err := os.WriteFile(path, []byte("SELECT edited"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The editor changed the file: its content is what the app now holds.
	got, ok := editorReadback{tmpPath: path, original: "SELECT original"}.result(nil).(editorContentMsg)
	if !ok {
		t.Fatalf("the readback produced %T, want editorContentMsg", editorReadback{tmpPath: path}.result(nil))
	}
	if got.content != "SELECT edited" {
		t.Errorf("the readback returned %q, want the edited file's content", got.content)
	}

	// The editor removed the file. Falling back to the original is what keeps
	// the query from vanishing, which is the failure the user would notice.
	missing := filepath.Join(dir, "gone.sql")
	got, ok = editorReadback{tmpPath: missing, original: "SELECT original"}.result(nil).(editorContentMsg)
	if !ok {
		t.Fatalf("the missing-file readback produced %T, want editorContentMsg", got)
	}
	if got.content != "SELECT original" {
		t.Errorf("the readback returned %q, want the original query", got.content)
	}
}

func TestEditorContentMsgOpensTheEditorWithTheReturnedText(t *testing.T) {
	m := routerModelLoaded(t)

	out, cmd := m.Update(editorContentMsg{content: "SELECT 9"})
	mm, ok := out.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", out)
	}
	if !mm.editorOpen {
		t.Error("editorContentMsg left the editor closed")
	}
	if got := mm.editor.Content(); got != "SELECT 9" {
		t.Errorf("the editor holds %q, want the message's content", got)
	}
	if cmd != nil {
		t.Error("editorContentMsg issued a command")
	}
}

func TestExternalEditorReportsAFailedTempFile(t *testing.T) {
	m := routerModelLoaded(t)
	m.editorOpen = true
	m.editor.SetContent("SELECT 1")
	// A temp dir that is not there makes CreateTemp fail, which is the branch
	// that instead of an editor tells the user the file could not be made.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))

	out, cmd := dispatch(t, m, "external_editor")
	if cmd != nil {
		t.Error("a failed temp file still produced a command")
	}
	if !out.editorOpen {
		t.Error("a failed temp file closed the editor, losing what the user was writing")
	}
}

func TestExternalEditorFallsBackToVimWhenEditorIsUnset(t *testing.T) {
	m := routerModelLoaded(t)
	m.editor.SetContent("SELECT 1")
	t.Setenv("EDITOR", "")

	out, cmd := dispatch(t, m, "external_editor")
	if cmd == nil {
		t.Fatal("external_editor produced no command with a non-empty editor")
	}
	// The editor closes while the external one owns the terminal; it is the
	// readback message that reopens it with the result.
	if out.editorOpen {
		t.Error("external_editor left the editor open under the external one")
	}
}

// The empty editor is the refusal: there is nothing to hand to $EDITOR, and
// opening one on an empty file would put the app in a modal for no reason.
func TestExternalEditorRefusesAnEmptyEditor(t *testing.T) {
	m := routerModelLoaded(t)
	m.editor.SetContent("")

	_, cmd := dispatch(t, m, "external_editor")
	if cmd != nil {
		t.Error("external_editor ran on an empty editor")
	}
}
