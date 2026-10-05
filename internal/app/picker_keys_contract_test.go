package app

// Scenario: El picker es lo primero que el usuario ve, y sus teclas estan cableadas a mano.
//
// The picker's key handling is not in appActions — it is a block of literals inside Update,
// before the state machine reaches anything that dispatches through the registry. Which
// makes it the one place where a key is bound without going through the single source of
// truth, and the place where a rebind silently does not apply.
//
// The three states each answer a different question, and answering the wrong one is the
// failure that matters:
//
//	the picker: q quits, everything else is the list's
//	the error:   r retries the connection, esc goes back to the picker, q quits
//	the loading: esc CANCELS the connection in flight and goes back to the picker
//
// The loading state's esc is the interesting one. It sets connectCancelled, which is what
// makes the in-flight connection's reply be dropped instead of landing on top of the
// picker. A block that set the state without the flag would show the picker with a stale
// connection arriving a second later.

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
)

// pickerModel is a model sitting in the picker, which is the state the app starts in.
func pickerModel(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.state = StatePicker
	return m
}

func TestThePickerQuitsOnQAndCtrlC(t *testing.T) {
	// ctrl+c carries NO Text. A modified key with Text set stringifies to the bare
	// character, so `{Code:'c', Text:"c", Mod:ModCtrl}` arrives as "c" — which is a
	// DIFFERENT key in this switch and quits nothing. The first version of this test sent
	// it that way and reported that ctrl+c did not quit, having sent "c".
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		t.Run(key.String(), func(t *testing.T) {
			t.Logf("the key stringifies as %q", key)
			m := pickerModel(t)
			out, cmd := fold(m, key)

			if cmd == nil {
				t.Fatalf("%s in the picker produced no command", key)
			}
			if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
				t.Errorf("%s in the picker produced %T, want tea.Quit", key, cmd())
			}
			if out.state != StatePicker {
				t.Errorf("%s changed the state to %v", key, out.state)
			}
		})
	}

	t.Run("any OTHER key does not quit", func(t *testing.T) {
		// The picker is a list; j, k, enter and the arrows all belong to it. Quitting on
		// one of those would lose the session.
		for _, key := range []tea.KeyPressMsg{
			{Code: 'j', Text: "j"},
			{Code: 'k', Text: "k"},
			{Code: tea.KeyDown},
			{Code: tea.KeyUp},
			{Code: tea.KeyEnter},
			{Code: 'x', Text: "x"},
		} {
			m := pickerModel(t)
			_, cmd := fold(m, key)
			if cmd != nil {
				if _, isQuit := cmd().(tea.QuitMsg); isQuit {
					t.Errorf("%s in the picker quit the app", key)
				}
			}
		}
	})
}

func TestTheErrorStateOffersRetryAndEscape(t *testing.T) {
	withProject := func() Model {
		m := pickerModel(t)
		m.state = StateError
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
		return m
	}

	t.Run("r retries the connection to the SAME project", func(t *testing.T) {
		m := withProject()
		out, cmd := fold(m, tea.KeyPressMsg{Code: 'r', Text: "r"})

		if cmd == nil {
			t.Fatal("r produced no command")
		}
		if out.state != StateLoading {
			t.Errorf("the state is %v, want the loading state", out.state)
		}
		// The flag has to be CLEARED on the way in. It is set when a connection is
		// abandoned, and a retry that did not clear it would be cancelled by the same flag
		// that the user cancelled a previous attempt with — so the retry would appear to
		// do nothing.
		if out.connectCancelled {
			t.Error("the retry left connectCancelled set, so the attempt would be cancelled as soon as it starts")
		}
	})

	t.Run("enter retries too", func(t *testing.T) {
		// Two keys for one action, and both are here rather than in the registry.
		m := withProject()
		out, cmd := fold(m, tea.KeyPressMsg{Code: tea.KeyEnter})

		if cmd == nil {
			t.Fatal("enter produced no command")
		}
		if out.state != StateLoading {
			t.Errorf("the state is %v, want the loading state", out.state)
		}
	})

	t.Run("r with NO project does nothing at all", func(t *testing.T) {
		// The error can be a schema failure, which arrives with no project to retry. Without
		// the guard this dereferences nil — and the crash happens on the retry press, long
		// after the error the user is looking at.
		m := pickerModel(t)
		m.state = StateError
		m.project = nil

		out, cmd := fold(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
		if cmd != nil {
			t.Error("r with no project issued a command")
		}
		if out.state != StateError {
			t.Errorf("the state became %v; the user should still be able to read the error", out.state)
		}
	})

	t.Run("esc goes back to the picker and CANCELS the connection", func(t *testing.T) {
		m := withProject()
		out, cmd := fold(m, tea.KeyPressMsg{Code: tea.KeyEscape})

		if out.state != StatePicker {
			t.Errorf("the state is %v, want the picker", out.state)
		}
		// Both flags, and they are the same decision seen from two ends: forcePicker stops
		// the next scan from reopening the connection that is still in flight, and
		// connectCancelled makes that connection's reply get dropped when it arrives.
		if !out.connectCancelled {
			t.Error("esc did not set connectCancelled; the in-flight reply would land on the picker")
		}
		if !out.forcePicker {
			t.Error("esc did not set forcePicker; the next scan would reconnect on its own")
		}
		if cmd == nil {
			t.Error("esc issued no rescan, so the picker would show the projects it had before the failure")
		}
	})

	t.Run("q and ctrl+c quit from the error screen too", func(t *testing.T) {
		for _, key := range []tea.KeyPressMsg{
			{Code: 'q', Text: "q"},
			{Code: 'c', Mod: tea.ModCtrl},
		} {
			m := withProject()
			_, cmd := fold(m, key)
			if cmd == nil {
				t.Errorf("%s produced no command", key)
				continue
			}
			if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
				t.Errorf("%s produced %T, want tea.Quit", key, cmd())
			}
		}
	})

	t.Run("any OTHER key leaves the error alone", func(t *testing.T) {
		// The error message is the only thing on screen; a key that changed the state would
		// take it away before the user had read it.
		for _, key := range []tea.KeyPressMsg{
			{Code: 'x', Text: "x"},
			{Code: tea.KeyDown},
			{Code: ' ', Text: " "},
		} {
			m := withProject()
			out, cmd := fold(m, key)
			if out.state != StateError {
				t.Errorf("%s changed the state to %v", key, out.state)
			}
			if cmd != nil {
				if _, isQuit := cmd().(tea.QuitMsg); isQuit {
					t.Errorf("%s quit from the error screen", key)
				}
			}
		}
	})
}

func TestTheLoadingStateCancelsOnEsc(t *testing.T) {
	t.Run("esc abandons the connection and rescans", func(t *testing.T) {
		m := pickerModel(t)
		m.state = StateLoading
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}

		out, cmd := fold(m, tea.KeyPressMsg{Code: tea.KeyEscape})

		if out.state != StatePicker {
			t.Errorf("the state is %v, want the picker", out.state)
		}
		if !out.connectCancelled {
			t.Error("esc did not set connectCancelled; the connection would finish and land on the picker")
		}
		if !out.forcePicker {
			t.Error("esc did not set forcePicker")
		}
		if cmd == nil {
			t.Error("esc issued no rescan")
		}
	})

	t.Run("q abandons it too, rather than quitting outright", func(t *testing.T) {
		// Deliberately NOT a quit. The user pressed q while the app was connecting and the
		// answer is "give up on this database", not "close the app" — the picker is still
		// there and there are other databases. Pinning it because it is the surprising
		// choice and the next person to read this block will wonder.
		for _, key := range []tea.KeyPressMsg{
			{Code: 'q', Text: "q"},
			{Code: 'c', Mod: tea.ModCtrl},
		} {
			m := pickerModel(t)
			m.state = StateLoading
			m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}

			out, cmd := fold(m, key)
			if out.state != StatePicker {
				t.Errorf("%s left the state at %v, want the picker — q while connecting means give up on THIS database", key, out.state)
			}
			if cmd != nil {
				if _, isQuit := cmd().(tea.QuitMsg); isQuit {
					t.Errorf("%s quit the app while connecting", key)
				}
			}
		}
	})

	t.Run("any OTHER key leaves it connecting", func(t *testing.T) {
		m := pickerModel(t)
		m.state = StateLoading
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}

		for _, key := range []tea.KeyPressMsg{
			{Code: 'r', Text: "r"},
			{Code: 'x', Text: "x"},
			{Code: tea.KeyEnter},
		} {
			out, _ := fold(m, key)
			if out.state != StateLoading {
				t.Errorf("%s changed the state to %v mid-connection", key, out.state)
			}
			if out.connectCancelled {
				t.Errorf("%s cancelled the connection", key)
			}
		}
	})
}

// TestTheScreensThatBypassTheRegistryAreNamed exists because of what it says rather than
// what it asserts.
//
// The three blocks above bind q, ctrl+c, r, enter and esc to literal key strings, outside
// appActions and outside the registry. That is deliberate — they run before there is a
// focused pane to dispatch to — and it has a consequence a rebind cannot reach: changing
// q in the registry does not change it here. Anyone who rebinds quit and expects it to work
// everywhere will find it does not, in exactly the three screens where there is nothing else
// to press.
//
// So the claim is asserted as a list rather than as behaviour: these keys are hardcoded, and
// this is where. If a block is moved into the registry the assertion stops matching and
// someone has to decide whether that was on purpose.
func TestTheScreensThatBypassTheRegistryAreNamed(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("reading app.go: %v", err)
	}
	text := string(src)

	for _, key := range []string{`key == "q"`, `key == "ctrl+c"`, `key == "r"`, `key == "esc"`} {
		if !strings.Contains(text, key) {
			t.Errorf("%s is no longer a literal in app.go; if it moved into the keybind registry, this file's premise changed", key)
		}
	}

	t.Run("the enter alias for retry is here too", func(t *testing.T) {
		if !strings.Contains(text, `key == "enter"`) {
			t.Error(`key == "enter" is no longer a literal in app.go`)
		}
	})
}
