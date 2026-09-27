package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"github.com/spf13/cobra"
)

func TestFailedDeletionWithoutProgressHasNoPartialResult(t *testing.T) {
	for _, dry := range []bool{false, true} {
		e, _, name := namedCLIFixture(t)
		e.Docker.Runner.(*dockertest.Daemon).Fail = func([]string) error { return errors.New("inspection failed") }
		local := ""
		cmd := deleteCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(""))
		args := []string{name, "--container", "--json"}
		if dry {
			args = append(args, "--dry-run")
		}
		cmd.SetArgs(args)
		if code := Execute(context.Background(), cmd); code == 0 {
			t.Fatal("failure exited successfully")
		}
		var report errorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Partial != nil {
			t.Fatal("failed preflight claimed progress", out.String(), err)
		}
	}
}

func TestDeletionReportsRemovalWhenActivitySaveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary filesystem permission enforcement")
	}
	e, _, name := namedCLIFixture(t)
	record := sessionRecord(t, e, name)
	path := filepath.Join(e.Store.Home, "sessions", record.Directory)
	t.Cleanup(func() { _ = os.Chmod(path, 0700) })
	e.Docker.Runner.(*dockertest.Daemon).Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "rm" {
			return os.Chmod(path, 0500)
		}
		return nil
	}
	result, err := e.Delete(context.Background(), app.DeleteOptions{Selection: app.Selection{Targets: []string{name}}, Scope: app.DeleteContainer})
	if err == nil || len(result.Containers) != 1 || result.Containers[0] != record.Applied.Creation.Name || len(result.Retained) != 1 {
		t.Fatal("committed Docker removal was hidden by activity-save failure", result, err)
	}
	if _, err := e.Store.Find(context.Background(), name, nil); err != nil {
		t.Fatal("failed activity save removed saved state", err)
	}
}
