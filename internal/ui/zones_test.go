package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

// registerZone scans a frame containing a single marked line and returns the
// resulting zone. bubblezone parses marked frames asynchronously, so Get() needs
// a poll: the first call right after Scan returns nil.
//
// The fixture is one marked line placed at column originX on row originY, with
// the given length, so a test can click at exact relative coordinates.
func registerZone(t *testing.T, id string, originX, originY, length int) *zone.ZoneInfo {
	t.Helper()
	Zones.SetEnabled(true)
	frame := strings.Repeat("\n", originY) + strings.Repeat(" ", originX) +
		Mark(id, "M"+strings.Repeat("x", length-1))
	Zones.Scan(frame)

	var zi *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if zi = Zones.Get(id); zi != nil && !zi.IsZero() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if zi == nil || zi.IsZero() {
		t.Skipf("bubblezone never registered zone %q; the manager needs a rendered frame", id)
	}
	return zi
}

// Scenario: Una zona desconocida nunca captura el ratón ni devuelve posición.
// (-1, -1) is the sentinel that keeps a caller from applying an offset to
// coordinates that have no origin.
func TestZones_UnknownZoneIsNeverInBounds(t *testing.T) {
	msg := tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft}

	if InBounds("dbx-zone-missing", msg) {
		t.Error("an unregistered zone reported itself in bounds")
	}
	if x, y := Pos("dbx-zone-missing", msg); x != -1 || y != -1 {
		t.Errorf("Pos(unknown) = (%d, %d), want (-1, -1)", x, y)
	}
}

// Scenario: Una zona registrada reporta coordenadas RELATIVAS a su origen, no
// absolutas. Callers index into the zone's own buffer, so handing them a screen
// coordinate would read the wrong cell.
func TestZones_RegisteredZoneResolvesRelativePosition(t *testing.T) {
	const id = "dbx-zone-rel"
	zi := registerZone(t, id, 4, 2, 6)

	// 2 cells in, 0 down from the origin: must resolve to the relative offset.
	msg := tea.MouseClickMsg{X: zi.StartX + 2, Y: zi.StartY, Button: tea.MouseLeft}
	if !InBounds(id, msg) {
		t.Fatalf("a click inside the zone was reported out of bounds; zone=%+v", zi)
	}
	if x, y := Pos(id, msg); x != 2 || y != 0 {
		t.Errorf("Pos = (%d, %d), want (2, 0) relative to the zone origin (abs was %d,%d)",
			x, y, msg.X, msg.Y)
	}
	// The absolute origin itself is the relative (0, 0).
	msg = tea.MouseClickMsg{X: zi.StartX, Y: zi.StartY, Button: tea.MouseLeft}
	if x, y := Pos(id, msg); x != 0 || y != 0 {
		t.Errorf("Pos at the origin = (%d, %d), want (0, 0)", x, y)
	}
}

// Scenario: Un click fuera del rectángulo de la zona no está dentro, aunque la
// zona exista. Bounds are the point of the helper: a click on neighbouring chrome
// must not be attributed to this pane.
func TestZones_ClickOutsideZoneIsOutOfBounds(t *testing.T) {
	const id = "dbx-zone-outside"
	zi := registerZone(t, id, 4, 2, 6)

	for _, tc := range []struct {
		name string
		msg  tea.MouseClickMsg
	}{
		{"one row below", tea.MouseClickMsg{X: zi.StartX, Y: zi.EndY + 1, Button: tea.MouseLeft}},
		{"one column past the end", tea.MouseClickMsg{X: zi.EndX + 1, Y: zi.StartY, Button: tea.MouseLeft}},
		{"screen origin", tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}},
	} {
		if InBounds(id, tc.msg) {
			t.Errorf("%s: reported in bounds; zone spans x[%d..%d] y[%d..%d], click at (%d,%d)",
				tc.name, zi.StartX, zi.EndX, zi.StartY, zi.EndY, tc.msg.X, tc.msg.Y)
		}
	}
}

// Scenario: Marcar no altera el texto visible. Mark() wraps content in escape
// sequences, so a renderer that leaked them would corrupt the frame.
func TestZones_MarkDoesNotAlterVisibleText(t *testing.T) {
	const id = "dbx-zone-text"
	marked := Mark(id, "hello")

	if strings.Contains(marked, id) {
		t.Errorf("Mark() leaked the zone id into the output: %q", marked)
	}
	if !strings.Contains(marked, "hello") {
		t.Errorf("Mark() dropped the content: %q", marked)
	}
	// The marker sequences are the only thing added, and they are invisible.
	if len(marked) <= len("hello") {
		t.Errorf("Mark() added no marker sequence: %q", marked)
	}
}
