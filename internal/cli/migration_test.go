package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func pendingMigrationHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "old", "session.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":6}`), 0600); err != nil {
		t.Fatal(err)
	}
	return home, path
}

func TestMigrationCommandIsRemoved(t *testing.T) {
	root := New()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "migrate-runtime" {
			t.Fatal("extra migration command remains")
		}
	}
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{"migrate-runtime"})
	if err := root.Execute(); err == nil {
		t.Fatal("removed command was accepted as a browser argument")
	}
}

func TestRequiredMigrationBlocksStateBackedCommandsWithoutPromptingScripts(t *testing.T) {
	for _, args := range [][]string{
		{"status"}, {"list"}, {"config", "list"},
		{"config", "create", "base", "--harness", "pi", "--json"},
		{"create", ".", "--name", "main", "--config", "base"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home, path := pendingMigrationHome(t)
			d := &dockertest.Daemon{}
			cmd := newRoot(docker.Runtime{Runner: d})
			var out, stderr bytes.Buffer
			cmd.SetIn(strings.NewReader("2\n"))
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"--home", home}, args...))
			var blocked *commanderror.Error
			if err := cmd.Execute(); !errors.As(err, &blocked) || blocked.Code != "migration_required" {
				t.Fatal("state-backed use was not blocked", err)
			}
			if out.Len() != 0 || stderr.Len() != 0 || len(d.History()) != 0 {
				t.Fatal("script entered UI/Docker", out.String(), stderr.String())
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != `{"version":6}` {
				t.Fatal("blocked use changed state", err)
			}
			for _, dir := range []string{"state", "configs", "auth", "cache"} {
				if _, err := os.Stat(filepath.Join(home, dir)); !os.IsNotExist(err) {
					t.Fatal("blocked use initialized home", dir, err)
				}
			}
		})
	}
}

func TestHelpAndVersionStayAvailableDuringMigrationBlock(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--help"}, {"status", "--help"}} {
		home, _ := pendingMigrationHome(t)
		cmd := newRoot(docker.Runtime{})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--home", home}, args...))
		if err := cmd.Execute(); err != nil || out.Len() == 0 {
			t.Fatal("help/version blocked", args, err)
		}
	}
}

func TestMigrationPopupIsOneBlockingChoice(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		fail        bool
		want        string
		calls       int
	}{
		{"exit", "1\n", false, "migration_required", 0},
		{"back", "0\n", false, "migration_required", 0},
		{"eof", "", false, "migration_required", 0},
		{"agree", "2\n", false, "", 1},
		{"failure", "2\n", true, "migration_failed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "dbx"}
			cmd.SetContext(context.Background())
			cmd.SetIn(strings.NewReader(tc.input))
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			calls := 0
			failure := errors.New("preflight failed")
			req := migration.Requirement{Name: "Test update", Description: "All sessions in this home; history and defaults stay.", Warning: "Container-local files are lost."}
			err := migrationPrompt(cmd, "/selected/home", req, func(context.Context) error {
				calls++
				if tc.fail {
					return failure
				}
				return nil
			})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var blocked *commanderror.Error
				if !errors.As(err, &blocked) || blocked.Code != tc.want {
					t.Fatal("wrong block", err)
				}
			}
			if tc.fail && !errors.Is(err, failure) {
				t.Fatal("migration failure cause lost")
			}
			if calls != tc.calls || out.Len() != 0 || !strings.Contains(stderr.String(), "Migration required") || !strings.Contains(stderr.String(), "Container-local") {
				t.Fatal("popup/migration/output contract", calls, out.String(), stderr.String())
			}
			if _, err := cmd.OutOrStdout().Write([]byte("original command output")); err != nil || out.String() != "original command output" {
				t.Fatal("original output was not restored", err)
			}
		})
	}
}

func TestMigrationPopupCancellationDoesNotRunUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{Use: "dbx"}
	cmd.SetContext(ctx)
	cmd.SetIn(strings.NewReader("2\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	ran := false
	err := migrationPrompt(cmd, "/home", migration.Requirement{Name: "Update"}, func(context.Context) error { ran = true; return nil })
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatal("cancelled gate ran migration", err)
	}
}

func TestMigrationPopupTerminalDefaultsToExit(t *testing.T) {
	p := newTerminalProbe(t)
	ran := false
	done := p.workflow(func(ctx context.Context, tty *os.File) error {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		return migrationPrompt(cmd, "/home", migration.Requirement{Name: "Update", Warning: "Container-local files are lost."}, func(context.Context) error { ran = true; return nil })
	})
	p.wait("Migration required")
	p.send("\r")
	var blocked *commanderror.Error
	if err := <-done; !errors.As(err, &blocked) || blocked.Code != "migration_required" || ran {
		t.Fatal("default Enter did not block/exit", err)
	}
}
