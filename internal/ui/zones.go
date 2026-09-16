package ui

import (
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

var Zones = zone.New()

func Mark(id string, content string) string {
	return Zones.Mark(id, content)
}

func InBounds(id string, msg tea.MouseMsg) bool {
	zi := Zones.Get(id)
	if zi == nil {
		return false
	}
	return zi.InBounds(msg)
}

// Pos returns the mouse coordinates relative to the top-left cell of the zone
// (0,0). If the zone is unknown or the event is outside it, it returns (-1, -1).
func Pos(id string, msg tea.MouseMsg) (int, int) {
	zi := Zones.Get(id)
	if zi == nil {
		return -1, -1
	}
	return zi.Pos(msg)
}
