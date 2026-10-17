package querybrowser

// Naming a query is a mode inside the modal: `n` opens it on the query under the
// cursor, the frame grows a name line and its own footer while it is open, and
// Enter saves while Esc and every other key stay inside the mode until it ends.
// None of that was exercised, so the mode's keys had no contract.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestQueryBrowser_NamingEditsAndShowsTheNameLine(t *testing.T) {
	b := newQB(t, "SELECT 1", "SELECT 2")

	if _, ok := press(t, b, "n"); !ok {
		t.Fatal("n was not handled")
	}
	if !b.naming {
		t.Fatal("n did not open the naming mode")
	}

	// While naming, the frame carries the input line.
	view := ansi.Strip(b.View())
	if !strings.Contains(view, "Name: ") {
		t.Errorf("the naming frame has no name line:\n%s", view)
	}

	// A printable key appends.
	typeText(t, b, "mine")
	if b.nameInput != "mine" {
		t.Errorf("the input holds %q after typing, want %q", b.nameInput, "mine")
	}

	// Backspace removes one character.
	press(t, b, "backspace")
	if b.nameInput != "min" {
		t.Errorf("the input holds %q after one backspace, want %q", b.nameInput, "min")
	}

	// A key with no text is still consumed and types nothing.
	press(t, b, "up")
	if b.nameInput != "min" {
		t.Errorf("an arrow key changed the input to %q", b.nameInput)
	}

	// Esc cancels: the mode ends, the buffer clears and the store is untouched.
	press(t, b, "esc")
	if b.naming {
		t.Error("esc left the naming mode on")
	}
	if b.nameInput != "" {
		t.Errorf("esc left %q in the input", b.nameInput)
	}
	if got := b.store.All()[0].Name; got != "" {
		t.Errorf("esc saved the name %q", got)
	}
}

func TestQueryBrowser_EnterSavesTheNameAndRefreshesTheList(t *testing.T) {
	b := newQB(t, "SELECT 1")
	press(t, b, "n")
	typeText(t, b, "favourite query")

	if _, ok := press(t, b, "enter"); !ok {
		t.Fatal("enter was not handled")
	}
	if b.naming {
		t.Error("enter left the naming mode on")
	}
	if got := b.store.All()[0].Name; got != "favourite query" {
		t.Errorf("the saved name is %q, want %q", got, "favourite query")
	}

	// Saving refreshes the list, so the named entry is what the frame shows.
	if !strings.Contains(ansi.Strip(b.View()), "favourite query") {
		t.Errorf("the saved name is not in the list:\n%s", ansi.Strip(b.View()))
	}
}

func TestQueryBrowser_EnterWithNoEntryUnderTheCursorStillEndsNaming(t *testing.T) {
	// The naming mode is entered from a row, but the list can move under it.
	// Pressing enter then must end the mode without touching the store rather
	// than indexing past the entries.
	b := newQB(t, "SELECT 1")
	press(t, b, "n")
	b.cursor = 99

	if _, ok := press(t, b, "enter"); !ok {
		t.Fatal("enter was not handled")
	}
	if b.naming {
		t.Error("enter with no entry under the cursor left naming on")
	}
	if got := b.store.All()[0].Name; got != "" {
		t.Errorf("enter with no cursor named the entry %q", got)
	}
}

func TestQueryBrowser_BackspaceAtTheStartOfAnEmptyNameIsANoOp(t *testing.T) {
	b := newQB(t, "SELECT 1")
	press(t, b, "n")
	b.nameInput = ""

	press(t, b, "backspace")
	if b.nameInput != "" {
		t.Errorf("backspace on an empty name produced %q", b.nameInput)
	}
}

func TestQueryBrowser_ARenderedFavoriteCarriesItsStar(t *testing.T) {
	// The star is drawn only for a favorite, so the plain row and the
	// favorited row have to differ by exactly that mark.
	b := newQB(t, "SELECT 1")
	b.store.ToggleFavorite(0)
	b.refreshEntries()

	line := ansi.Strip(b.renderEntry(0, b.entries[0], 60))
	if !strings.Contains(line, "★ ") {
		t.Errorf("the rendered favorite has no star: %q", line)
	}

	plain := ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "SELECT 1"}, 60))
	if strings.Contains(plain, "★ ") {
		t.Errorf("a non-favorite rendered a star: %q", plain)
	}
}
