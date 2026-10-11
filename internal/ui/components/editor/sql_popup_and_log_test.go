package editor

// Scenario: El popup de autocompletado forma parte del render del editor, y el arranque
//          del esquema queda registrado con el dato que recibió.
//
// Two survivors of the non-Boundary sweep, both CONDITIONALS_NEGATION in sql.go:
//
//   - sql.go:637 `if popup != ""` -> `!=`: Render only returns "" for a HIDDEN popup, and
//     the call sits behind `e.autocomplete.Visible()`, so the guard is always true and the
//     negation drops the popup from the view silently. The editor still renders, the text
//     is still there — the completion simply never appears.
//   - sql.go:74 `export != nil` -> `==` inside the SetSchema debug line: the only trace of
//     whether the editor was handed a real export or a reset-to-nil, and a negated
//     argument makes an investigation read the log backwards.

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/debuglog"
)

// Scenario: Con el popup abierto, el render del editor lo incluye.
func TestTheCompletionPopupIsPartOfTheView(t *testing.T) {
	e := newEditor(t, 80, 10)
	e.SetSchema(testExport())
	e.SetContent("SELECT * FROM u")
	e.SetCursorPos(0, len("SELECT * FROM u"))
	e.triggerAutocomplete()
	if !e.AutocompleteVisible() {
		t.Fatal("precondition: the popup should be open after SELECT * FROM u")
	}
	out := ansi.Strip(e.View())
	if !strings.Contains(out, "users") {
		t.Errorf("the open popup is missing from the render:\n%s", out)
	}
}

// Scenario: SetSchema deja en el log si recibió un export o un nil.
//
// One log per call, each in its own directory: asserting that BOTH booleans appear
// somewhere would pass under the negation too, with the two lines swapped. Each call may
// produce its own value and no other.
func TestSetSchemaLogsWhetherTheExportArrived(t *testing.T) {
	for _, tc := range []struct {
		name   string
		export func() *context.SchemaExport
		want   string
		other  string
	}{
		{"a nil export logs export=false", func() *context.SchemaExport { return nil }, "export=false", "export=true"},
		{"a real export logs export=true", func() *context.SchemaExport { return testExport() }, "export=true", "export=false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old := debuglog.DefaultDir
			debuglog.DefaultDir = dir
			t.Cleanup(func() { debuglog.DefaultDir = old })

			NewSQLEditor(testStyles()).SetSchema(tc.export())

			data, err := os.ReadFile(debuglog.Path("autocomplete"))
			if err != nil {
				t.Fatalf("SetSchema was not logged: %v", err)
			}
			log := string(data)
			if !strings.Contains(log, tc.want) {
				t.Errorf("the log does not contain %q:\n%s", tc.want, log)
			}
			if strings.Contains(log, tc.other) {
				t.Errorf("the log contains %q, which is the wrong answer:\n%s", tc.other, log)
			}
		})
	}
}
