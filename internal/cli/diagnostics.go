package cli

import (
	"fmt"
	"io"

	"devbox/internal/app"
)

// Plans are delivered synchronously before runtime mutation. They are receipts
// for an explicitly requested action, not config warnings produced by access.
func diagnosticRenderer(stderr io.Writer) func(app.Diagnostic) {
	if stderr == nil {
		return nil
	}
	return func(diagnostic app.Diagnostic) {
		if diagnostic.Target != "" {
			fmt.Fprintf(stderr, "%s: ", displayCell(diagnostic.Target))
		}
		fmt.Fprintln(stderr, displayCell(diagnostic.Message))
	}
}
