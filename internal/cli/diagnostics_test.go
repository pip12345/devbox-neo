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
	message := "Replacing the container. Files and tools stored only inside the old container will be lost."
	diagnosticRenderer(&out)(app.Diagnostic{Code: "apply_plan", Target: "session", Message: message})
	if out.String() != "session: "+message+"\n" {
		t.Fatal(out.String())
	}
}

func TestApplicationDiagnosticEscapesControls(t *testing.T) {
	var out bytes.Buffer
	diagnosticRenderer(&out)(app.Diagnostic{Target: "session\nname", Message: "Updating\x1b[2J\nWarning:\rtext"})
	want := "\"session\\nname\": \"Updating\\x1b[2J\\nWarning:\\rtext\"\n"
	if got := out.String(); got != want {
		t.Fatalf("diagnostic controls: got %q, want %q", got, want)
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
