package cli

// Helpers for the seams contract test. They exist as a separate file so the test above reads
// as the contract and this reads as the plumbing.
//
// The production closures are reproduced here rather than read back out of root.go, because
// the whole point is to run THEIR bodies — and a helper that called `buildModel` would be
// calling whatever the current value is, which is the thing under test.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/spf13/cobra"
)

// The PRODUCTION values of the two seams, captured at package initialisation — which happens
// before any test function runs, and therefore before any test has replaced them.
//
// This is the only way to run a default closure. Calling `buildModel` from a test runs
// whatever it currently holds, which after the first seam-swapping test is a stub; assigning
// a copy of the default and calling that proves nothing about the default. So the default is
// captured here, once, and the test calls the capture.
//
// runProgram is no longer captured here, and that is the outcome rather than an omission:
// with programOpts named as a variable, a test can run the PRODUCTION closure directly
// (TestTheDefaultProgramRunnerRunsWithoutATerminal) instead of needing a copy taken before
// another test swapped it. The capture existed only because the default could not be run at
// all — a bubbletea program opens /dev/tty — so a copy was the next best thing, and a copy
// proves nothing. The seam removed the need for it.
var productionBuildModel = buildModel

// quittingModel quits on its first update, so a real program started against it ends at once.
type quittingModel struct{}

func (quittingModel) Init() tea.Cmd { return nil }

func (m quittingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, nil
	}
	return m, tea.Quit
}

func (m quittingModel) View() tea.View { return tea.NewView("") }

// cobraCommandForTest is a cobra command carrying the --connection flag getConnection reads.
type cobraCommandForTest struct {
	t *testing.T
}

func (c cobraCommandForTest) command() *cobra.Command {
	c.t.Helper()
	cmd := &cobra.Command{Use: "probe"}
	cmd.Flags().StringP("connection", "c", "", "Connection name from config")
	return cmd
}

// askCmdForTest is the ask command with the two flags runAsk reads.
func askCmdForTest(t *testing.T, sqlOnly bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "ask"}
	cmd.Flags().BoolP("json", "j", false, "Output as JSON")
	cmd.Flags().Bool("sql-only", sqlOnly, "Print SQL without running it")
	cmd.Flags().StringP("connection", "c", "", "Connection name from config")
	return cmd
}

// pipeCmdForTest is the pipe command, whose flags runPipe reads.
func pipeCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "pipe"}
	cmd.Flags().BoolP("json", "j", false, "Output as JSON")
	cmd.Flags().StringP("connection", "c", "", "Connection name from config")
	return cmd
}
