package cli

import (
	"errors"
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
	runner := cliui.New(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
	// UI cancellation must also reach command-owned preparation/operations.
	// Finish restores the terminal without cancelling this command context.
	cmd.SetContext(runner.Context)
	return menu{Runner: runner, cmd: cmd}
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

func (m menu) warnings(warnings []string) {
	for _, warning := range warnings {
		m.Notice("Warning: " + displayCell(warning))
	}
}

const menuChoicePrompt = cliui.ChoicePrompt

// Domain failures can be retried only while the command itself is alive.
func (m menu) report(err error) error {
	if err == nil {
		return nil
	}
	if cancelled := m.Context.Err(); cancelled != nil {
		if errors.Is(err, cancelled) {
			return err
		}
		return errors.Join(err, cancelled)
	}
	report := describeError(err)
	if m.cmd != nil {
		home, _ := m.cmd.Flags().GetString("home")
		var scope func(*errorReport)
		scope = func(r *errorReport) {
			r.Next = scopedSteps(m.cmd, r.Next, home)
			for i := range r.Related {
				scope(&r.Related[i])
			}
		}
		scope(&report)
	}
	m.Notice(humanErrorText(report))
	return nil
}
