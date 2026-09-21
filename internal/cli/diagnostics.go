package cli

import (
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/environment"
)

// Render at the engine's reporting point so warnings precede startup and
// foreground output, even when the operation later fails or is cancelled.
func diagnosticRenderer(stderr io.Writer) func(app.Diagnostic) {
	if stderr == nil {
		return nil
	}
	return func(diagnostic app.Diagnostic) {
		fmt.Fprintf(stderr, "Warning: %s\n", diagnostic.Message)
		for _, inputChange := range diagnostic.PendingInputChanges {
			fmt.Fprintf(stderr, "  - %s\n", inputChange)
		}
		if diagnostic.Code == "creation_drift" {
			fmt.Fprintln(stderr, "\nUsing the existing container without applying these creation changes.")
			if diagnostic.Change == environment.RebuildAndRecreate {
				fmt.Fprintln(stderr, "Rebuild image and recreate:")
			} else {
				fmt.Fprintln(stderr, "Recreate to apply changes:")
			}
		}
		fmt.Fprintf(stderr, "  %s\n", strings.Join(diagnostic.Command, " "))
	}
}
