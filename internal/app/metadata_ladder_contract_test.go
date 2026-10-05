package app

// Scenario: Dos cargadores, la MISMA tabla, dos políticas de error opuestas.
//
// loadMetadata asks the database five things about a table — columns are not among them,
// it asks for constraints, foreign keys, indexes and an overview — and it STOPS at the
// first failure, returning metadataLoadedMsg{err} with nothing loaded. loadExplorerPreviewData
// asks for the same things plus the columns, and SWALLOWS every failure, substituting nil
// and carrying on.
//
// Those are opposite answers to the same question, in the same file, sixty lines apart. It
// is defensible — the grid needs to say "I could not load this table" and the preview is a
// side panel that is better half-drawn than empty — and it is exactly the shape that is
// impossible to change without noticing, so it is pinned here: which arms abort, which arms
// swallow, and what the user is shown in each case.
//
// The reason this file exists at all is that the arms were unreachable. The model held a
// concrete *pgx.Conn, so a test could only reach a metadata failure by breaking a running
// database, and the whole ladder sat uncovered while the file reported its happy path.
// Now that the model holds postgres.Conn, each arm is one fixture: answer the other four
// queries, fail this one.
//
// The matchers are chosen so no read's SQL contains another's marker. Two of them nearly
// collide, and both collisions are silent:
//
//	`information_schema.tables` is a SUBSTRING of `information_schema.table_constraints`,
//	  so the overview's marker carries its table alias and the foreign keys' does not
//	  depend on step order at all.
//
//	ListColumns and the overview's row count share `FROM information_schema.columns`,
//	  so the column marker includes the newline that ends the clause in ListColumns and
//	  not in the count subquery. The first version of this table matched the bare text
//	  and a "columns fails" fixture therefore broke the overview too — which the loader
//	  swallows, so the test passed and proved nothing about columns.

import (
	"errors"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// The five reads, in the order the two loaders make them, with a matcher each that no
// other read's SQL contains.
var metadataReads = []struct {
	name string
	what string
	// match is a substring unique to this read's SQL.
	match string
}{
	{"constraints", "ListConstraints", "FROM pg_constraint"},
	{"foreign keys", "ListForeignKeys", "FROM information_schema.table_constraints"},
	{"indexes", "ListIndexes", "FROM pg_indexes"},
	// The overview's FIRST scalar read, not its fourth. GetTableOverview runs four
	// QueryRows and returns on the first failure, so a fixture matching a later one would
	// never be reached and the arm it was written for would go uncovered while the test
	// reported it as covered.
	{"overview", "GetTableOverview", "FROM information_schema.tables t"},
	{"columns", "ListColumns", "FROM information_schema.columns\n"},
}

// metadataConn answers every metadata read with an empty result EXCEPT one, which fails.
//
// An empty result is the right default here: the loaders all handle zero rows, and the
// assertion is about whether an error reached the message, not about what the data was.
func metadataConn(failing string) *pgxfake.Conn {
	conn := pgxfake.New()
	for _, read := range metadataReads {
		err := error(nil)
		if read.name == failing {
			err = errors.New("permission denied for table " + read.name)
		}
		conn.Steps = append(conn.Steps, pgxfake.Step{Match: read.match, Err: err})
	}
	return conn
}

// loadMetadata's policy: the FIRST failure ends the load, and the message carries the error.
//
// Aborting is the right call for the grid — a table whose constraints are unreadable is a
// table the grid cannot show honestly, and a preview of it would be a lie about the
// schema. So the arms are: the three reads that abort must not run the reads after them.
func TestTheGridLoaderAbortsAtTheFirstFailedRead(t *testing.T) {
	// The reads that abort, in call order. The overview is last and its failure is
	// swallowed by the loader itself, which is why it is not in this list.
	aborting := []string{"constraints", "foreign keys", "indexes"}

	for _, read := range metadataReads {
		t.Run(read.name, func(t *testing.T) {
			conn := metadataConn(read.name)
			m := routerModelLoaded(t)
			m.conn = conn

			cmd := m.loadMetadata("public", "orders")
			if cmd == nil {
				t.Fatal("loadMetadata refused, so this proves nothing")
			}

			msg, ok := cmd().(metadataLoadedMsg)
			if !ok {
				t.Fatalf("loadMetadata produced %T, want metadataLoadedMsg", cmd())
			}

			abort := false
			for _, name := range aborting {
				abort = abort || name == read.name
			}

			if abort {
				if msg.err == nil {
					t.Errorf("the %s read failed and the load reported success, so the grid would show a table whose schema it never read", read.name)
				}
				if len(msg.constraints) != 0 || len(msg.foreignKeys) != 0 || len(msg.indexes) != 0 {
					t.Errorf("the %s read failed but the message carries %d constraints, %d foreign keys and %d indexes",
						read.name, len(msg.constraints), len(msg.foreignKeys), len(msg.indexes))
				}
				return
			}

			// The overview, and anything the loader treats as optional.
			if msg.err != nil {
				t.Errorf("the %s read failed and the load reported it; only the first three reads abort", read.name)
			}
		})
	}

	// The early exit is not just an error flag — the reads AFTER the failure never happen.
	// A loader that carried on and then reported the error would pass the assertions above
	// and still be wrong, because it would have queried a table the user has no access to
	// four more times.
	t.Run("and it stops asking", func(t *testing.T) {
		conn := metadataConn("constraints")
		m := routerModelLoaded(t)
		m.conn = conn

		cmd := m.loadMetadata("public", "orders")
		if cmd == nil {
			t.Fatal("loadMetadata refused, so this proves nothing")
		}
		if msg := cmd().(metadataLoadedMsg); msg.err == nil {
			t.Fatal("the constraints read did not fail; the fixture is wrong")
		}

		for _, read := range metadataReads {
			if read.name == "constraints" || read.name == "columns" {
				continue
			}
			if conn.Asked(read.match) {
				t.Errorf("the load asked for %s after the constraints read failed", read.name)
			}
		}
	})
}

// The other policy: every failure is swallowed and the preview is assembled from whatever
// survived. Four nils and no error is a legitimate answer, and the preview renders it.
func TestThePreviewLoaderKeepsWhatItCanReadAndSaysNothingAboutTheRest(t *testing.T) {
	for _, read := range metadataReads {
		t.Run(read.name+" fails", func(t *testing.T) {
			conn := metadataConn(read.name)
			m := routerModelLoaded(t)
			m.conn = conn

			cmd := m.loadExplorerPreviewData("public", "orders")
			if cmd == nil {
				t.Fatal("loadExplorerPreviewData refused, so this proves nothing")
			}

			// The message has no error field at all, which is the policy: the preview is
			// best-effort and says nothing about what it could not read.
			msg, ok := cmd().(explorerPreviewDataMsg)
			if !ok {
				t.Fatalf("loadExplorerPreviewData produced %T, want explorerPreviewDataMsg", cmd())
			}

			// Whatever else happened, the preview must still be about the table it was asked
			// for. A loader that returned a message for nothing would render an empty panel
			// with no indication of what it was supposed to contain.
			if msg.schema != "public" || msg.table != "orders" {
				t.Errorf("the message is about %s.%s, want public.orders", msg.schema, msg.table)
			}
			// And the load must not have aborted: every read was attempted.
			for _, other := range metadataReads {
				if !conn.Asked(other.match) {
					t.Errorf("the preview never asked for %s, so it aborted instead of carrying on", other.name)
				}
			}
		})
	}

	t.Run("a table that cannot be read at all still names itself", func(t *testing.T) {
		// The extreme: every read fails. The preview is empty and there is no error, which
		// means a user whose table was dropped mid-session sees an empty preview rather
		// than a message. That is the policy, and the cost of it is worth naming.
		conn := pgxfake.New()
		for _, read := range metadataReads {
			conn.Steps = append(conn.Steps, pgxfake.Step{
				Match: read.match,
				Err:   errors.New("relation does not exist"),
			})
		}
		m := routerModelLoaded(t)
		m.conn = conn

		cmd := m.loadExplorerPreviewData("public", "gone")
		if cmd == nil {
			t.Fatal("loadExplorerPreviewData refused, so this proves nothing")
		}
		msg := cmd().(explorerPreviewDataMsg)
		if msg.schema != "public" || msg.table != "gone" {
			t.Errorf("the message is about %s.%s, want public.gone", msg.schema, msg.table)
		}
		if len(msg.columns) != 0 {
			t.Errorf("the message carries %d columns from a table that does not exist", len(msg.columns))
		}
	})
}
