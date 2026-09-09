package cli

import (
	"bytes"
	"fmt"
	"io"
	"text/tabwriter"

	"devbox/internal/app"
	"devbox/internal/environment"
)

func statusChange(view app.View) string {
	if view.Error != "" || view.ConfigError != "" || view.Pending != nil {
		return "Cannot check"
	}
	switch view.Desired {
	case environment.NoChange:
		return "No changes"
	case environment.RuntimeSync:
		return "Runtime changes"
	case environment.Recreate:
		return "Recreate needed"
	case environment.RebuildAndRecreate:
		return "Rebuild + recreate needed"
	default:
		return "Cannot check"
	}
}

func printStatusList(out io.Writer, views []app.View) error {
	var table bytes.Buffer
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tCHANGE")
	for _, view := range views {
		fmt.Fprintf(w, "%s\t%s\t%s\n", displayCell(view.Name), containerState(view), statusChange(view))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := printListRows(out, views, table.String()); err != nil {
		return err
	}
	for _, view := range views {
		if len(view.PendingInputChanges) > 0 {
			if _, err := fmt.Fprintf(out, "%s:\n", displayCell(view.Name)); err != nil {
				return err
			}
			for _, inputChange := range view.PendingInputChanges {
				if _, err := fmt.Fprintf(out, "  - [%s] %s\n", inputChange.Scope, inputChange); err != nil {
					return err
				}
			}
		}
		if view.ConfigError != "" {
			if _, err := fmt.Fprintf(out, "! %s: desired configuration: %s\n", displayCell(view.Name), displayCell(view.ConfigError)); err != nil {
				return err
			}
		}
		if view.Error == "" && view.ConfigError == "" && view.Pending == nil && (view.Desired == environment.Recreate || view.Desired == environment.RebuildAndRecreate) {
			if _, err := fmt.Fprintf(out, "  devbox-neo recreate %s\n", displayCell(view.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}
