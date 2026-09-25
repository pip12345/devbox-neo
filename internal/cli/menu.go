package cli

import (
	"fmt"
	"io"

	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"github.com/spf13/cobra"
)

// menu adds command-scoped guidance to the domain-independent UI runtime.
// Child workflows receive the same runner and therefore the same input buffer.
type menu struct {
	*cliui.Runner
	cmd *cobra.Command
}

func newMenu(cmd *cobra.Command) menu {
	return menu{Runner: cliui.New(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout()), cmd: cmd}
}

func (m menu) commandHint(home, reason string, args ...string) error {
	steps := []commanderror.Step{commanderror.Next(reason, args...)}
	if m.cmd != nil {
		steps = scopedSteps(m.cmd, steps, home)
	}
	_, err := fmt.Fprint(m.Out, stepsText(steps))
	return err
}

func writeMenuTitle(out io.Writer, title string) error { return cliui.Title(out, title) }
func writeMenuHint(out io.Writer, text string) error   { return cliui.Hint(out, text) }
func menuPrefix(n int) string                          { return cliui.Prefix(n) }

const menuChoicePrompt = cliui.ChoicePrompt

// Domain failures can be retried only while the command itself is alive.
func (m menu) report(err error) error {
	if err == nil {
		return nil
	}
	if m.Context.Err() != nil {
		return m.Context.Err()
	}
	m.Notice("Error: " + displayCell(err.Error()))
	return nil
}
