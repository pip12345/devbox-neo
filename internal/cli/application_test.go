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

func TestRecreateContainerFlagForcesReplacement(t *testing.T) {
	for _, force := range []bool{false, true} {
		e, _, id := namedCLIFixture(t)
		old := sessionRecord(t, e, id)
		name := ""
		cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		args := []string{id}
		if force {
			args = append(args, "--container")
		}
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		current := sessionRecord(t, e, id)
		if (current.Applied.SetupContainer != old.Applied.SetupContainer) != force || current.Applied.ImageID != old.Applied.ImageID {
			t.Fatal("force flag did not preserve minimal/default behavior")
		}
	}
}

func pendingCLICopy(t *testing.T, e *app.Engine, id string) {
	t.Helper()
	d := e.Docker.Runner.(*dockertest.Daemon)
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			return errors.New("failed preparation")
		}
		return nil
	}
	if _, err := e.Transfer(context.Background(), app.TransferOptions{Mode: "clone", Source: id, Destination: t.TempDir()}); err == nil {
		t.Fatal("copy unexpectedly succeeded")
	}
	d.Fail = nil
}

func TestCopyAbortCLIAndMenuDoNotNeedValidConfig(t *testing.T) {
	for _, menu := range []bool{false, true} {
		t.Run(map[bool]string{false: "CLI", true: "menu"}[menu], func(t *testing.T) {
			f, out, q, id := frontendFixture(t, strings.NewReader("6\ny\n"))
			pendingCLICopy(t, f.e, id)
			if err := os.WriteFile(filepath.Join(q.Sources[0].Path, "config.json"), []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			if menu {
				if _, err := f.transfer(id); err != nil {
					t.Fatal(err, out.String())
				}
			} else {
				name := ""
				cmd := transferCommand(func(*cobra.Command) (*app.Engine, error) { return f.e, nil }, &name)
				var output bytes.Buffer
				cmd.SetOut(&output)
				cmd.SetErr(&output)
				cmd.SetArgs([]string{id, "--abort", "--json"})
				if err := cmd.ExecuteContext(context.Background()); err != nil {
					t.Fatal(err, output.String())
				}
				var result app.TransferResult
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Aborted || result.Source != id {
					t.Fatal(output.String(), err)
				}
			}
			if pending, err := f.e.Store.PendingID(id); err != nil || pending != nil {
				t.Fatal("abort retained reservation", pending, err)
			}
		})
	}
}
