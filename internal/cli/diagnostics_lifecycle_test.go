package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

type diagnosticWriter func([]byte) (int, error)

func (w diagnosticWriter) Write(p []byte) (int, error) { return w(p) }

func diagnosticFixture(t *testing.T) (*app.Engine, *dockertest.Daemon, app.Request, string) {
	t.Helper()
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(state.Home, "configs/test")
	if err := os.MkdirAll(filepath.Join(profile, "pi"), 0700); err != nil {
		t.Fatal(err)
	}
	put := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(profile, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("config.json", `{"version":1,"harness":"pi"}`)
	put("before-open.sh", "echo hook-marker")
	d := &dockertest.Daemon{}
	e := &app.Engine{Store: state, Docker: docker.Runtime{Runner: d}, UID: os.Getuid(), GID: os.Getgid()}
	q := app.Request{Workspace: t.TempDir(), LocalName: "test", Sources: testConfigSources(state.Home, "test")}
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	put("config.json", `{"version":1,"harness":"pi","network":"host"}`)
	if err := os.Symlink("missing", filepath.Join(profile, "pi/skipped")); err != nil {
		t.Fatal(err)
	}
	spec, err := e.Resolve(q)
	if err != nil || len(spec.Warnings) != 1 {
		t.Fatal("expected one resolution warning", spec.Warnings, err)
	}
	want := "Warning: this container differs from current configuration:\n  - network: default -> host\n\nUsing the existing container without applying these creation changes.\nRecreate to apply changes:\n  devbox-neo recreate " + result.Name + "\n"
	want += fmt.Sprintf("Warning: %s\n", spec.Warnings[0])
	return e, d, q, want
}

func TestDiagnosticRenderingPrecedesResolutionAndChildOutput(t *testing.T) {
	e, d, q, want := diagnosticFixture(t)
	var combined, stdout, stderr bytes.Buffer
	e.Streams.Out = io.MultiWriter(&stdout, &combined)
	e.Streams.Err = io.MultiWriter(&stderr, &combined)
	e.OnDiagnostic = diagnosticRenderer(e.Streams.Err)
	d.Fail = func([]string) error {
		if combined.String() != want {
			t.Fatalf("Docker reached before complete warning output: %q", combined.String())
		}
		return nil
	}
	d.Attached = func(_ context.Context, c docker.Command) error {
		if c.Stdin == nil {
			return nil
		}
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "hook-marker") {
			d.Fail = nil
			if _, err := io.WriteString(c.Stdout, "child stdout\n"); err != nil {
				return err
			}
			_, err = io.WriteString(c.Stderr, "child stderr\n")
		}
		return err
	}
	result, err := e.Open(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || combined.String() != want+"child stdout\nchild stderr\n" || stdout.String() != "child stdout\n" || stderr.String() != want+"child stderr\n" {
		t.Fatalf("output lost, duplicated, redirected, or reordered: stdout=%q stderr=%q combined=%q", stdout.String(), stderr.String(), combined.String())
	}
}

func TestRootWiresImmediateDiagnosticRendering(t *testing.T) {
	e, _, q, want := diagnosticFixture(t)
	// Never use host Docker, even if a missing callback lets execution continue.
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	root := New()
	root.SetOut(&stdout)
	root.SetErr(diagnosticWriter(func(p []byte) (int, error) {
		if strings.Contains(string(p), "this container differs from current configuration") {
			cancel()
		}
		return stderr.Write(p)
	}))
	root.SetArgs([]string{"--home", e.Store.Home, "open", q.Workspace, "--name", q.LocalName})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("root did not deliver diagnostic before Docker access", err)
	}
	if stderr.String() != want || stdout.Len() != 0 {
		t.Fatalf("root output changed: stdout=%q stderr=%q want=%q", stdout.String(), stderr.String(), want)
	}
}
