package cli

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"devbox/internal/app"
)

func TestApplicationDiagnosticRendering(t *testing.T) {
	var out bytes.Buffer
	diagnosticRenderer(&out)(app.Diagnostic{Code: "apply_plan", Target: "session", Message: "Apply runtime configuration."})
	if out.String() != "session: Apply runtime configuration.\n" {
		t.Fatal(out.String())
	}
}

func TestDiagnosticRendererSilentAndFailedWriters(t *testing.T) {
	if diagnosticRenderer(nil) != nil {
		t.Fatal("nil output must disable rendering")
	}
	diagnosticRenderer(io.Discard)(app.Diagnostic{Message: "discarded"})
	writes := 0
	render := diagnosticRenderer(diagnosticWriter(func([]byte) (int, error) { writes++; return 0, errors.New("output unavailable") }))
	render(app.Diagnostic{Target: "session", Message: "message"})
	if writes != 2 {
		t.Fatal("writer failure changed best-effort output", writes)
	}
}
