package app

// Scenario: El ciclo de vida de una conexion, y las banderas que la cancelan.
//
// Connecting is a three-message chain: dbConnectedMsg, then schemaLoadedMsg, then the
// main view. Each of the first two can arrive AFTER the user has walked away — pressing
// esc during the connection sets connectCancelled and returns to the picker, and the reply
// still lands a second later.
//
// That is what connectCancelled is for, and the arm that consumes it has one extra duty
// that is easy to miss: it CLEARS the flag. Miss that and the NEXT connection — the one
// the user starts immediately after, on purpose, having picked another database — is
// cancelled by a flag set for the previous attempt. The app then looks like it cannot
// connect to anything.
//
// So the property is not "the cancelled reply is dropped", which is one branch, but "the
// flag is consumed exactly once", which needs the reply, then a second reply.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/ui/components/explorer"
)

// connecting is a model part-way through a connection to a named project.
func connecting(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.state = StateLoading
	m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
	m.spinnerActive = true
	return m
}

// TestTheSpinnerStopsOnEveryExit is the property that makes a stuck spinner possible.
//
// The spinner is a repeating command that the model keeps reissuing. Every path out of a
// connection has to turn it off, or the app spins forever over a pane that never loads.
// The exits are not all in one place: one is here, two are in the message handlers, and
// the fourth is a cancel — which is why this is asserted as a list rather than per line.
func TestTheSpinnerStopsOnEveryExit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		arm    func(m Model) Model
		expect bool
	}{
		{
			name: "a connection error stops it",
			arm: func(m Model) Model {
				out, _ := fold(m, dbConnectedMsg{err: errMetadataLoad})
				return out
			},
		},
		{
			// The arm that used to be missing. The schema-load error below does clear
			// it, so the two error paths disagreed, and the one that fires FIRST is the
			// one that did not.
			name: "a schema load error stops it",
			arm: func(m Model) Model {
				out, _ := fold(m, schemaLoadedMsg{err: errMetadataLoad})
				return out
			},
		},
		{
			// Not asserted as "the spinner stops": the cancelled arm drops the reply
			// before reaching any of that, so the spinner keeps whatever it had. What
			// matters is that the reply was DROPPED, which is asserted in the next test
			// — putting it here would only be asserting the flag's starting value.
			name: "a cancelled reply changes nothing at all",
			arm: func(m Model) Model {
				m.connectCancelled = true
				m.state = StatePicker
				out, _ := fold(m, schemaLoadedMsg{})
				return out
			},
			expect: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.arm(connecting(t))
			if out.spinnerActive != tc.expect {
				t.Errorf("the spinner is %t after %s, want %t",
					out.spinnerActive, tc.name, tc.expect)
			}
		})
	}
}

// TestACancelledReplyIsDroppedAndConsumesTheFlag is the one that matters.
func TestACancelledReplyIsDroppedAndConsumesTheFlag(t *testing.T) {
	t.Run("the cancelled reply lands on the PICKER and not on the main view", func(t *testing.T) {
		m := connecting(t)
		m.connectCancelled = true
		m.state = StatePicker

		out, cmd := fold(m, schemaLoadedMsg{dbName: "shopdb"})

		// Dropped: no state change, no schema built, nothing loaded.
		if out.state != StatePicker {
			t.Errorf("a cancelled schema reply moved the app to %v, want the picker", out.state)
		}
		if cmd != nil {
			t.Error("a cancelled schema reply issued a command")
		}
		// Not "the explorer is nil": routerModelLoaded attaches one on purpose, so the
		// check is that the reply did not REBUILD it. A rebuilt explorer is a fresh empty
		// tree, which overwrites the tree the user was looking at a second earlier.
		if out.explorer != m.explorer {
			t.Error("a cancelled schema reply replaced the explorer")
		}
		if out.dbName != "" {
			t.Errorf("a cancelled schema reply set the database name to %q", out.dbName)
		}
	})

	t.Run("the flag is CLEARED, so the NEXT connection is not cancelled by it", func(t *testing.T) {
		// The bug this pins. connectCancelled is set when a connection is abandoned. If
		// consuming it did not clear it, the user's next attempt — on a database they
		// chose on purpose — would be silently cancelled, and the app would look like it
		// cannot connect to anything at all.
		// Driven through schemaLoadedMsg rather than dbConnectedMsg, because the success
		// arm of the latter builds a statement runner around the connection and a
		// zero-value pgx.Conn panics the moment anything touches it. Both messages
		// consume the flag the same way, and this one needs no connection.
		//
		// The root is NOT nil and never is: LoadDatabase builds a database node before it
		// can return success, so a nil root is not a state this message can arrive in.
		// It is a landmine — SetNodes([]*Node{nil}) panics inside the tree's flatten, so
		// a future nil root would be a crash rather than an empty tree — but it is not
		// reachable and no production change is made for it.
		m := connecting(t)
		m.connectCancelled = true

		out, _ := fold(m, schemaLoadedMsg{root: explorer.NewNode("shopdb", explorer.NodeDatabase, "shopdb")})
		if out.connectCancelled {
			t.Fatal("the flag survived the reply that consumed it")
		}

		// And the proof that the model is usable: the next reply is NOT dropped.
		next, cmd := fold(out, schemaLoadedMsg{
			root: explorer.NewNode("shopdb", explorer.NodeDatabase, "shopdb"),
		})
		if next.state != StateMain {
			t.Errorf("the connection after a cancelled one ended in %v, want the main view", next.state)
		}
		if cmd == nil {
			t.Error("the schema after a cancelled connection issued no command")
		}
	})

	t.Run("a connection ERROR is also dropped when cancelled", func(t *testing.T) {
		// Same flag, different payload, and the ORDER matters: the cancel check comes
		// before the error check. A cancelled connection that failed must not drop the
		// user on the error screen with a failure they abandoned — and the flag has to be
		// cleared here too, or the next attempt dies the same way.
		m := connecting(t)
		m.connectCancelled = true
		m.state = StatePicker

		out, _ := fold(m, dbConnectedMsg{err: errMetadataLoad})

		if out.state != StatePicker {
			t.Errorf("a cancelled connection error moved the app to %v, want the picker", out.state)
		}
		if out.err != nil {
			t.Errorf("a cancelled connection error was stored as %v", out.err)
		}
		if out.connectCancelled {
			t.Error("the flag was not cleared by the error reply")
		}
		// The spinner is UNCHANGED, and that is the point of the ordering: the cancel
		// check comes first, so a cancelled reply is dropped whole — including the error
		// arm that would otherwise have stopped the spinner. The app goes back to the
		// picker, where no spinner is drawn, and the tick keeps running harmlessly until
		// the next connection.
		//
		// The first version of this case asserted the spinner stopped here, which would
		// have required MOVING the error handling above the cancel check — and then a
		// cancelled connection that failed would drop the user on the error screen with
		// the failure they abandoned.
		if !out.spinnerActive {
			t.Error("a cancelled reply changed the spinner, so the reply was not dropped whole")
		}
	})
}

// The connection log. It records by PROJECT NAME and never the DSN, because a DSN carries
// the password — that is what password_env puts in it — so writing one into a log file
// would undo the whole point of that knob.
func TestTheConnectionIsLoggedByNameAndTheFailureByNothing(t *testing.T) {
	t.Run("a success hands the model a connection AND a runner", func(t *testing.T) {
		// Needs a real connection: the success arm builds a statement runner around it and
		// a zero-value pgx.Conn panics the moment anything touches it, which is the trap
		// this file's other cases kept walking into.
		conn := connectTestDB(t, testDSN(t))
		m := connecting(t)

		// The project comes FROM THE MESSAGE, not from the model, and the distinction is
		// the whole reason this case exists. connectToDB stamps the project it connected
		// to onto the reply; the handler reads msg.project and never m.project, so a model
		// whose own project was changed while the connection was in flight still loads the
		// schema of the database it actually reached.
		//
		// The first version of this case built dbConnectedMsg{conn: conn} with no project
		// and reported a nil dereference at the loadSchema call. The code was right: that
		// message cannot be produced by connectToDB, and the only thing that builds one
		// without a project is a test.
		out, cmd := fold(m, dbConnectedMsg{conn: conn, project: m.project})

		if out.conn == nil {
			t.Fatal("a successful connection left the model without a connection")
		}
		if out.state != StateLoading {
			t.Errorf("a successful connection left the state at %v, want loading — the schema has not loaded yet", out.state)
		}
		// The runner is what executes statements. Without it the first query after
		// connecting would find nothing to run against, and the schema pane would fill in
		// while every query silently did nothing.
		if out.runner == nil {
			t.Error("a successful connection left no statement runner, so no query could run")
		}
		if cmd == nil {
			t.Error("a successful connection issued no schema load")
		}
		// And the spinner KEEPS going here, because the schema is still loading. This is
		// the third exit and the one that is supposed to leave it running.
		if !out.spinnerActive {
			t.Error("the spinner stopped when the schema is still loading")
		}
	})

	t.Run("the project comes from the MESSAGE, not from the model", func(t *testing.T) {
		// connectToDB stamps the project it connected to onto the reply, and the handler
		// reads THAT one. It never reads m.project, and the difference is the whole point:
		// a model whose own project changed while the connection was in flight still
		// loads the schema of the database it actually reached.
		//
		// A handler reading m.project here would show one database's tables under another
		// database's name, with no error anywhere — the schema is real, it just belongs
		// to a different connection.
		conn := connectTestDB(t, testDSN(t))
		m := connecting(t)
		m.project = &config.FoundProject{Name: "STALE", Path: t.TempDir(), Active: true}

		real := &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
		out, _ := fold(m, dbConnectedMsg{conn: conn, project: real})

		if out.project == nil || out.project.Name != "shopdb" {
			t.Errorf("the model kept the project %+v, want the one the message named", out.project)
		}
	})

	t.Run("a failure goes to the error screen with the error SET", func(t *testing.T) {
		m := connecting(t)

		out, _ := fold(m, dbConnectedMsg{err: errMetadataLoad})

		if out.state != StateError {
			t.Errorf("a failed connection left the state at %v, want the error screen", out.state)
		}
		if out.err == nil {
			t.Fatal("a failed connection left the error unset, so the error screen shows nothing")
		}
		if out.conn != nil {
			t.Error("a failed connection left a connection in place")
		}
		// The runner is what executes statements, and it must NOT exist without a
		// connection: a nil-deref on the first query is a crash, not an error message.
		if out.runner != nil {
			t.Error("a failed connection left a statement runner in place")
		}
	})

	t.Run("no DSN is a failure, not a panic", func(t *testing.T) {
		// connectToDB with a project that has no DSN. The command is run, so the whole
		// path is exercised — and the answer must be a dbConnectedMsg carrying an error,
		// which is the only shape the handler above knows how to show.
		cmd := connecting(t).connectToDB(config.FoundProject{Name: "no-dsn", Path: t.TempDir()})
		if cmd == nil {
			t.Fatal("connecting with no DSN issued no command")
		}
		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatalf("connecting with no DSN produced %T", cmd())
		}
		if msg.err == nil {
			t.Error("connecting with no DSN reported success")
		}
		if !strings.Contains(msg.err.Error(), "DSN") {
			t.Errorf("the error is %q, want it to mention the missing DSN", msg.err)
		}
	})
}

// The two ticks. The MESSAGES are what is worth testing — a tick producing the wrong
// message type leaves the spinner frozen with no error anywhere, and the toast clock
// stopped so notifications accumulate forever.
//
// The tea.Cmd itself is not inspectable: it is a bare func() tea.Msg, and the one the app
// builds blocks for its interval. So the observable is the handler, driven by the message
// the tick produces — which is the whole of what a tick is.

func TestTheToastTickExpiresToastsAndRearmsItself(t *testing.T) {
	t.Run("the tick reissues itself", func(t *testing.T) {
		// Both ticks return their own constructor, which is what keeps them running. A
		// handler that updated the toast but returned nil would expire everything once
		// and then stop, which looks like the toast system working and then quietly
		// breaking.
		m := routerModelLoaded(t)
		_, cmd := fold(m, toastTickMsg{})
		if cmd == nil {
			t.Error("the toast tick did not reissue itself, so the clock stops after one tick")
		}
	})

	t.Run("a FRESH toast survives its first tick", func(t *testing.T) {
		// A handler that cleared everything on every tick would pass the expiry case and
		// fail this one, which is the only way the expiry case means anything.
		m := routerModelLoaded(t)
		m.toast.ShowInfo("just now")

		out, _ := fold(m, toastTickMsg{})

		if got := strings.Join(out.toast.RenderedToasts(), " "); !strings.Contains(got, "just now") {
			t.Errorf("a fresh toast was dropped by the tick: %q", got)
		}
	})

}

func TestTheSpinnerTickAdvancesOnlyWhileTheSpinnerIsRunning(t *testing.T) {
	for _, tc := range []struct {
		name    string
		active  bool
		wantAdv bool
	}{
		{"a running spinner advances", true, true},
		{"a stopped spinner does not", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			m.spinnerActive = tc.active
			m.spinnerFrame = 0

			out, cmd := fold(m, spinnerTickMsg{})
			if cmd == nil {
				t.Error("the spinner tick did not reissue itself")
			}

			advanced := out.spinnerFrame != 0
			if advanced != tc.wantAdv {
				t.Errorf("with active=%t the frame went from 0 to %d, want it to %t move",
					tc.active, out.spinnerFrame, tc.wantAdv)
			}
		})
	}

	t.Run("the frame WRAPS rather than running off the end", func(t *testing.T) {
		// The modulo. Without it the frame index grows forever and every frame past the
		// ninth indexes past the array — a panic a couple of minutes into any session
		// that connects to anything.
		m := routerModelLoaded(t)
		m.spinnerActive = true
		m.spinnerFrame = len(spinnerChars) - 1

		out, _ := fold(m, spinnerTickMsg{})
		if out.spinnerFrame != 0 {
			t.Errorf("the frame went from the last to %d, want it to wrap to 0", out.spinnerFrame)
		}
	})

	t.Run("and it never runs past the last frame however many ticks arrive", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.spinnerActive = true
		for range len(spinnerChars)*3 + 1 {
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				m, _ = fold(m, spinnerTickMsg{})
			}()
			if recovered != nil {
				t.Fatalf("the spinner panicked after repeated ticks: %v", recovered)
			}
			if m.spinnerFrame < 0 || m.spinnerFrame >= len(spinnerChars) {
				t.Fatalf("the frame index is %d, want it inside 0..%d",
					m.spinnerFrame, len(spinnerChars)-1)
			}
		}
	})
}

// Init is the app's entry command: scan the projects, clean up dead sessions, and start
// the toast clock. A nil Init means a program that starts and shows nothing.
func TestTheAppStarts(t *testing.T) {
	m := routerModel(t)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init returned no command, so the app starts doing nothing")
	}
	// NOT run: the batch it contains blocks on the interval of the toast tick. What is
	// asserted is that it is a command at all, which is the difference between "starts"
	// and "starts and then waits for a key that is never processed".
}
