package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecreateRepairStepsUsePublicTarget(t *testing.T) {
	e, q, target := namedCLIFixture(t)
	r := sessionRecord(t, e, target)
	if err := os.RemoveAll(q.Sources[0].Path); err != nil {
		t.Fatal(err)
	}
	root := newRoot(e.Docker)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--home", e.Store.Home, "recreate", target})
	if code := Execute(context.Background(), root); code != 1 {
		t.Fatal(code, out.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "edit "+target) || strings.Contains(stderr.String(), r.ID) {
		t.Fatal("repair points at an internal selector", stderr.String())
	}
}

func TestStatusWarningsKeepStderrAndJSONContracts(t *testing.T) {
	e, q, target := namedCLIFixture(t)
	root := filepath.Join(q.Sources[0].Path, "pi")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "skipped")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status", target, "--json"}, {"status", "--json"}, {"copy", target, t.TempDir(), "--dry-run", "--json"}} {
		cmd := newRoot(e.Docker)
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append([]string{"--home", e.Store.Home}, args...))
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(args, err, out.String(), stderr.String())
		}
		if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "Warning:") || strings.Contains(out.String(), `"Warnings"`) || strings.Contains(out.String(), `"warnings"`) {
			t.Fatal("warnings changed command data", args, out.String())
		}
		if strings.Count(stderr.String(), "Warning: skipping non-regular config entry") != 1 {
			t.Fatal("warning lost or repeated", args, stderr.String())
		}
	}
}

func TestMenuStatusQueuesWarningsInsteadOfWritingUnderRenderer(t *testing.T) {
	f, out, q, target := frontendFixture(t, strings.NewReader("0\n"))
	root := filepath.Join(q.Sources[0].Path, "pi")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "skipped")); err != nil {
		t.Fatal(err)
	}
	stderr := new(bytes.Buffer)
	f.e.Streams.Err = stderr
	if err := f.status(target); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 || strings.Count(out.String(), "Warning: skipping non-regular config entry") != 1 {
		t.Fatal("menu warning bypassed its presentation owner", out.String(), stderr.String())
	}
}
