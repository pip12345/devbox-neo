package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

type diagnosticWriter func([]byte) (int, error)

func (w diagnosticWriter) Write(p []byte) (int, error) { return w(p) }

func TestApplicationDiagnosticPrecedesMutationAndChildOutput(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	d := e.Docker.Runner.(*dockertest.Daemon)
	if err := os.WriteFile(filepath.Join(q.Sources[0].Path, "setup.sh"), []byte("echo child"), 0600); err != nil {
		t.Fatal(err)
	}
	var combined, stdout, stderr bytes.Buffer
	e.Streams.Out = io.MultiWriter(&stdout, &combined)
	e.Streams.Err = io.MultiWriter(&stderr, &combined)
	e.OnDiagnostic = diagnosticRenderer(e.Streams.Err)
	d.Fail = func(args []string) error {
		switch args[0] {
		case "stop", "create", "start", "rm":
			if !strings.Contains(combined.String(), "Container-local changes will be lost") {
				t.Fatal("mutation preceded explanation", combined.String())
			}
		}
		return nil
	}
	d.Attached = func(_ context.Context, c docker.Command) error {
		if c.Stdin != nil {
			if _, err := io.WriteString(c.Stdout, "child stdout\n"); err != nil {
				return err
			}
			if _, err := io.WriteString(c.Stderr, "child stderr\n"); err != nil {
				return err
			}
		}
		return nil
	}
	result, err := e.Recreate(context.Background(), q, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || stdout.String() != "child stdout\n" || !strings.HasSuffix(combined.String(), "child stdout\nchild stderr\n") {
		t.Fatal("lost or reordered output", combined.String())
	}
}

func TestRootWiresImmediateApplicationDiagnostic(t *testing.T) {
	e, _, id := namedCLIFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	root := newRoot(e.Docker)
	root.SetOut(&stdout)
	root.SetErr(diagnosticWriter(func(p []byte) (int, error) {
		if strings.Contains(string(p), "Apply runtime configuration") {
			cancel()
		}
		return stderr.Write(p)
	}))
	root.SetArgs([]string{"--home", e.Store.Home, "recreate", id})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("root did not deliver plan synchronously", err)
	}
	if !strings.Contains(stderr.String(), id+": Apply runtime configuration") || stdout.Len() != 0 {
		t.Fatal(stdout.String(), stderr.String())
	}
}
