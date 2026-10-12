package postgres

import (
	"context"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// Scenario: ListIndexes on a row whose index definition is the empty string —
// the exact boundary of `if len(def) > 0`. Relaxed to `len(def) >= 0` the arm
// always runs, but its body asks strings.Contains("", "UNIQUE"), which is
// false, so Unique stays false on both sides. The test pins that equality: an
// empty definition is not a unique index under either reading, and the real
// definition alongside it shows the arm is alive and not just a constant.
func TestPostgresBoundaryListIndexesEmptyDefinitionIsNotUnique(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{{
		Match: "",
		Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "c"}},
			Rows:    [][]any{{"", ""}},
		},
	}}}

	got, err := NewSchemaLoader(conn).ListIndexes(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("ListIndexes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListIndexes returned %d indexes, want 1", len(got))
	}
	if got[0].Unique {
		t.Error("an empty index definition was reported as UNIQUE, want false")
	}

	// The other side of the same arm: a real definition still counts as
	// unique, so the assertion above is not just pinning a constant.
	uniqueConn := &pgxfake.Conn{Steps: []pgxfake.Step{{
		Match: "",
		Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "c"}},
			Rows:    [][]any{{"idx", "CREATE UNIQUE INDEX idx ON t (id)"}},
		},
	}}}
	got, err = NewSchemaLoader(uniqueConn).ListIndexes(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("ListIndexes: %v", err)
	}
	if len(got) != 1 || !got[0].Unique {
		t.Errorf("ListIndexes = %+v, want one unique index", got)
	}
}
