package grid

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCopyCSVIsOfferedOnlyForAWholeRealTable(t *testing.T) {
	t.Run("bulk table export offers COPY", func(t *testing.T) {
		ep := newExportPickerForTest()
		ep.ShowBulkFileMode("public", "users", [][]interface{}{{1}}, []string{"id"})
		if got := len(ep.visibleOptions()); got != 5 {
			t.Fatalf("visible options = %d, want 5", got)
		}
		if !strings.Contains(ep.View(), "COPY CSV") {
			t.Fatal("bulk picker does not render COPY CSV")
		}
		ep.cursor = 4
		cmd, handled := ep.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !handled || cmd == nil {
			t.Fatal("COPY CSV selection was not handled")
		}
		msg := cmd().(ExportSelectedMsg)
		if msg.Format != ExportCopyCSV || msg.Schema != "public" || msg.Table != "users" {
			t.Errorf("message = %+v", msg)
		}
	})

	t.Run("row and loaded-window exports omit COPY", func(t *testing.T) {
		for _, show := range []func(*ExportPicker){
			func(ep *ExportPicker) { ep.Show("public", "users", []interface{}{1}, nil, []string{"id"}) },
			func(ep *ExportPicker) { ep.ShowFileMode("public", "users", nil, [][]interface{}{{1}}, []string{"id"}) },
			func(ep *ExportPicker) { ep.ShowYankMode("public", "users", nil, [][]interface{}{{1}}, []string{"id"}) },
		} {
			ep := newExportPickerForTest()
			show(ep)
			if got := len(ep.visibleOptions()); got != 4 {
				t.Errorf("visible options = %d, want 4", got)
			}
			if strings.Contains(ep.View(), "COPY CSV") {
				t.Error("non-bulk picker renders COPY CSV")
			}
		}
	})
}

func TestGridWithoutSelectionEnablesBulkCopyOnlyForARealTable(t *testing.T) {
	realTable := newGrid(t, 2, 1, 80, 20, 100)
	realTable.SetData(realTable.Data(), "public", "users")
	realTable.startExport()
	if !realTable.exportPicker.bulkMode {
		t.Error("whole-table export did not enable COPY mode")
	}

	query := newGrid(t, 2, 1, 80, 20, 100)
	query.SetData(query.Data(), "", "query")
	query.startExport()
	if query.exportPicker.bulkMode {
		t.Error("an ad-hoc query result offered table COPY")
	}
}
