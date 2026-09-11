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
