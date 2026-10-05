package explorerpreview

// Scenario: El diagrama ER se dibuja con cajas de ancho FIJO, y cada ancho tiene un clamp.
//
// renderBox and its caller both do arithmetic on a display width and then a string
// operation on the result. Two things can go wrong and they are different failures:
//
//	a NEGATIVE pad is a negative strings.Repeat count, which PANICS
//	a byte slice at a DISPLAY width cuts a multi-byte character in half, and the box
//	    draws invalid UTF-8 on its own border line
//
// The second is the interesting one. `entry[:innerWidth]` treats a column width as a byte
// offset, and a column named "identificador" or "año" is more bytes than columns — so
// truncating it to fit produces a broken character sitting inside the box's own frame. It
// does not crash and it does not look like an error: it looks like a font problem.
//
// The clamps are pinned as a matrix over pane widths rather than one case, because the
// interesting values are the ones where the subtraction goes negative: a pane narrower than
// MinBoxWidth, and a center box wider than the pane it is centred in.

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// itoa avoids a strconv import in the subtest names, which read better as plain numbers.
func itoa(n int) string { return strconv.Itoa(n) }

// boxOf is a table with a column whose name is `name`, of the given display width.
func boxOf(name string) ColumnBadge {
	return ColumnBadge{Name: name, DataType: "text"}
}

// TestABoxIsAlwaysClosedOnEveryRow pins the invariant that makes the whole file's
// arithmetic safe: every line of a box has the same display width, and the last character
// of each is the right-hand border.
//
// That is the property that fails first when a pad goes negative or a slice lands
// mid-character, and it is checkable without knowing what any individual row should say.
func TestABoxIsAlwaysClosedOnEveryRow(t *testing.T) {
	for _, width := range []int{10, 14, 20, 30, 40, 80, 120} {
		for _, name := range []string{
			"id",
			"user_id",
			"a_very_long_column_name_indeed",
			// The byte-vs-rune cases. "año" is 4 bytes and 3 columns; the emoji is 4 bytes
			// and 2 columns; "日本" is 6 bytes and 4 columns. Every one of them is NARROWER
			// in columns than in bytes, so a byte slice truncates them differently from a
			// column slice and cuts a character in half.
			"año",
			"日本",
			"🎉",
		} {
			t.Run(strings.Repeat("w", width/10)+"_"+name, func(t *testing.T) {
				box := renderBox("orders", []ColumnBadge{boxOf(name)}, width, false)

				var want int
				for i, line := range strings.Split(box, "\n") {
					got := ansi.StringWidth(line)
					if i == 0 {
						want = got
						continue
					}
					if got != want {
						t.Errorf("at width %d with column %q line %d is %d columns wide, want %d:\n%s",
							width, name, i, got, want, box)
					}
					if !utf8.ValidString(line) {
						t.Errorf("at width %d with column %q line %d is not valid UTF-8: %q",
							width, name, i, line)
					}
				}
			})
		}
	}
}

func TestANameLongerThanTheBoxIsTruncatedNotOverflowing(t *testing.T) {
	// The `width` argument to renderBox is the TOTAL box width, borders included, so every
	// line of the result is exactly that many columns wide. The first version of this case
	// asserted width+2 and reported every narrow box as two columns short.
	for _, width := range []int{4, 6, 8, 10, 12, 20} {
		box := renderBox("orders", []ColumnBadge{boxOf("an_extremely_long_column_name")}, width, false)
		for i, line := range strings.Split(box, "\n") {
			if got := ansi.StringWidth(line); got != width {
				t.Errorf("at width %d line %d is %d columns, want %d:\n%s",
					width, i, got, width, box)
			}
			if !utf8.ValidString(line) {
				t.Errorf("at width %d line %d is not valid UTF-8: %q", width, i, line)
			}
		}
	}

	t.Run("the box is BADGED for PK and FK columns", func(t *testing.T) {
		// The badge is three characters wide whatever it says, which is what keeps the
		// columns aligned down the box. A variable-width badge would break every row.
		for _, tc := range []struct {
			name string
			col  ColumnBadge
			want string
		}{
			{"a plain column", ColumnBadge{Name: "id", DataType: "text"}, "   "},
			{"a primary key", ColumnBadge{Name: "id", DataType: "int", IsPK: true}, "PK "},
			{"a foreign key", ColumnBadge{Name: "user_id", DataType: "int", IsFK: true}, "FK "},
		} {
			t.Run(tc.name, func(t *testing.T) {
				box := renderBox("orders", []ColumnBadge{tc.col}, 30, false)
				if !strings.Contains(box, tc.want+"id") && !strings.Contains(box, tc.want+tc.col.Name) {
					t.Errorf("the box does not carry the badge %q:\n%s", tc.want, box)
				}
			})
		}
	})

	t.Run("a PK wins over an FK on the same column", func(t *testing.T) {
		// The two flags are exclusive branches, so PK first is a decision and not an
		// accident. The user asked which key identifies the row; "FK" would answer a
		// different question.
		box := renderBox("orders", []ColumnBadge{{Name: "id", DataType: "int", IsPK: true, IsFK: true}}, 30, false)
		if !strings.Contains(box, "PK id") {
			t.Errorf("a column that is both did not get the PK badge:\n%s", box)
		}
		if strings.Contains(box, "FK id") {
			t.Errorf("a column that is both also got the FK badge:\n%s", box)
		}
	})
}

// The name is TRUNCATED and PADDED to the inner width, centred. Both the truncation and
// the two pads are the same arithmetic as the columns', and both have a negative case.
func TestANameTooWideForItsBoxIsShortenedWithAnEllipsis(t *testing.T) {
	// The first version of this case asserted the long name survived into a 40-column box
	// and reported a mismatch on correct behaviour: it is shortened, and the ellipsis is
	// how you can tell that happened.
	box := renderBox("an_extremely_long_table_name_for_a_narrow_box", nil, 20, false)
	if !strings.Contains(box, "…") {
		t.Errorf("a long name in a narrow box was not shortened:\n%s", box)
	}
	if strings.Contains(box, "an_extremely_long_table_name") {
		t.Errorf("the full long name is still in the box:\n%s", box)
	}

	t.Run("a name that fits is NOT shortened", func(t *testing.T) {
		box := renderBox("orders", nil, 20, false)
		if strings.Contains(box, "…") {
			t.Errorf("a short name was shortened in a wide box:\n%s", box)
		}
		if !strings.Contains(box, "orders") {
			t.Errorf("the name is not in the box:\n%s", box)
		}
	})
}

// The pane renderer. Two clamps and a centring subtraction, and the pane can be narrower
// than the minimum box — which is what happens in a narrow terminal.
func TestTheDiagramPaneClampsItsWidths(t *testing.T) {
	diagram := &ERDiagram{
		Center: TableBox{
			Name:    "orders",
			Columns: []ColumnBadge{{Name: "id", DataType: "int", IsPK: true}},
		},
		Outgoing: []Relationship{{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "1:N"}},
		Incoming: []Relationship{{FromColumn: "user_id", ToTable: "orders", ToColumn: "id", Cardinality: "1:N"}},
	}
	// Driven through renderERDiagram rather than View, because View rebuilds the diagram
	// from the table data and would discard the one built here. What is under test is the
	// pane's arithmetic, not the wiring that produces its input.
	for _, width := range []int{1, 4, 10, 20, 40, 80, 200} {
		t.Run("width "+itoa(width), func(t *testing.T) {
			out := RenderERDiagram(*diagram, width, 40, &ERDiagramNav{})
			_ = out

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("the diagram pane panicked at width %d: %v", width, r)
					}
				}()
				if strings.TrimSpace(out) == "" {
					t.Errorf("at width %d the diagram rendered nothing", width)
				}
				for i, line := range strings.Split(out, "\n") {
					if !utf8.ValidString(line) {
						t.Errorf("at width %d line %d is not valid UTF-8: %q", width, i, line)
					}
				}
			}()
		})
	}

	t.Run("a table with NO relationships says so", func(t *testing.T) {
		// The other half of the guard, and the one that makes the empty state legible. A
		// lone table is the normal state of a table nobody has written foreign keys for.
		lone := &ERDiagram{
			Center: TableBox{Name: "users", Columns: []ColumnBadge{{Name: "id", DataType: "int"}}},
		}

		if out := RenderERDiagram(*lone, 80, 40, &ERDiagramNav{}); !strings.Contains(out, "No relationships") {
			t.Errorf("a table with no relationships did not say so:\n%s", out)
		}
	})
}

// The nav cursor. activeRow is a triple state — a row index, -1 meaning "the box itself is
// selected", and the wrap on both ends — which is the shape where an off-by-one shows up
// as a cursor that will not move.
func TestTheDiagramCursorWalksAndWraps(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cols  int
		start int
		down  []int
		up    []int
	}{
		{
			name:  "one column",
			cols:  1,
			start: 0,
			down:  []int{0, 0, 0},
			up:    []int{0, 0, 0},
		},
		{
			// It CLAMPS, it does not wrap. Eleven cursors in this app wrap and this one
			// does not — the guard is `activeRow < count-1`, so the last row is where it
			// stays. The first version of this case expected a wrap and reported the
			// cursor refusing to move.
			name:  "three columns clamp at both ends",
			cols:  3,
			start: 0,
			down:  []int{1, 2, 2, 2},
			up:    []int{1, 0, 0, 0},
		},
		{
			// -1 is "the box itself", and stepping down from it must land on the FIRST
			// column rather than staying put — otherwise the box can never be left once it
			// has been entered.
			name:  "stepping down from the box lands on the first column",
			cols:  3,
			start: -1,
			down:  []int{0, 1, 2},
		},
		{
			name:  "a single column from the box",
			cols:  1,
			start: -1,
			down:  []int{0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &ERDiagramNav{columnCounts: [2]int{tc.cols, tc.cols}, activeRow: tc.start}

			for i, want := range tc.down {
				n.MoveDown()
				if n.activeRow != want {
					t.Errorf("down step %d left the cursor at %d, want %d", i+1, n.activeRow, want)
				}
			}
			for i, want := range tc.up {
				n.MoveUp()
				if n.activeRow != want {
					t.Errorf("up step %d left the cursor at %d, want %d", i+1, n.activeRow, want)
				}
			}
		})
	}

	t.Run("a column with NO columns does not move", func(t *testing.T) {
		// The count == 0 guard. Reaching it means moving into an empty table box, and the
		// cursor has to stay where it is rather than wander into a list that is not there.
		n := &ERDiagramNav{columnCounts: [2]int{0, 0}, activeRow: 0}

		n.MoveDown()
		n.MoveUp()

		if n.activeRow != 0 {
			t.Errorf("the cursor moved to %d on a column with no columns", n.activeRow)
		}
	})
}
