package cli

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"devbox/internal/app"
	"devbox/internal/environment"
)

func TestDiagnosticRenderingParity(t *testing.T) {
	before, after := "default", "host"
	changes := []environment.InputChange{
		{Scope: environment.ContainerScope, Code: "changed", Field: "network", Before: &before, After: &after},
		{Scope: environment.ContainerScope, Code: "entry_added", Field: "env", Key: "TOKEN"},
	}
	for _, tt := range []struct {
		name       string
		diagnostic app.Diagnostic
		want       string
	}{
		{"recreate", app.Diagnostic{Code: "creation_drift", Message: "this container differs from current configuration:", Command: []string{"devbox-neo", "recreate", "session"}, Change: environment.Recreate, PendingInputChanges: changes}, "Warning: this container differs from current configuration:\n  - network: default -> host\n  - environment variable TOKEN added\n\nUsing the existing container without applying these creation changes.\nRecreate to apply changes:\n  devbox-neo recreate session\n"},
		{"rebuild", app.Diagnostic{Code: "creation_drift", Message: "this container differs from current configuration:", Command: []string{"devbox-neo", "recreate", "session"}, Change: environment.RebuildAndRecreate}, "Warning: this container differs from current configuration:\n\nUsing the existing container without applying these creation changes.\nRebuild image and recreate:\n  devbox-neo recreate session\n"},
		{"deferred", app.Diagnostic{Code: "runtime_deferred", Message: "managed configuration is deferred while running; it will apply at the next startup", Command: []string{"devbox-neo", "stop", "session"}}, "Warning: managed configuration is deferred while running; it will apply at the next startup\n  devbox-neo stop session\n"},
		{"unquoted-command", app.Diagnostic{Message: "message", Command: []string{"devbox-neo", "a b", "c"}}, "Warning: message\n  devbox-neo a b c\n"},
		{"empty-command", app.Diagnostic{Message: "message"}, "Warning: message\n  \n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			diagnosticRenderer(&stderr)(tt.diagnostic)
			if stderr.String() != tt.want {
				t.Fatalf("got stderr=%q; want stderr=%q", stderr.String(), tt.want)
			}
		})
	}
}

func TestDiagnosticRendererSilentAndFailedWriters(t *testing.T) {
	if diagnosticRenderer(nil) != nil {
		t.Fatal("nil output must disable rendering")
	}
	diagnosticRenderer(io.Discard)(app.Diagnostic{Message: "discarded"})
	writes := 0
	render := diagnosticRenderer(diagnosticWriter(func([]byte) (int, error) {
		writes++
		return 0, errors.New("output unavailable")
	}))
	render(app.Diagnostic{Message: "message", Command: []string{"devbox-neo", "stop", "session"}})
	if writes != 2 {
		t.Fatal("writer failure changed best-effort output sequencing", writes)
	}
}
