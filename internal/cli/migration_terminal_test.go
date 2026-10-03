package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func TestMigrationPopupAgreementContinuesOriginalAction(t *testing.T) {
	p := newTerminalProbe(t)
	var out bytes.Buffer
	applied := false
	done := p.workflow(func(ctx context.Context, tty *os.File) error {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(&out)
		cmd.SetErr(tty)
		err := migrationPrompt(cmd, "/home", migration.Requirement{Name: "Update", Warning: "Container-local files are lost."}, func(context.Context) error { applied = true; return nil })
		if err != nil {
			return err
		}
		cmd.Println("original action continued")
		return nil
	})
	p.wait("Migration required")
	p.send("\x1b[B\r")
	if err := <-done; err != nil || !applied || out.String() != "original action continued\n" {
		t.Fatal("agreement did not release original action/output", out.String(), err)
	}
}

func TestJSONWithTerminalInputCannotAgreeImplicitly(t *testing.T) {
	home, _ := pendingMigrationHome(t)
	_, tty := testTerminal(t)
	cmd := &cobra.Command{Use: "status"}
	cmd.SetContext(context.Background())
	cmd.SetIn(tty)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.Flags().Bool("json", true, "")
	var blocked *commanderror.Error
	if err := requireMigration(cmd, home, docker.Runtime{}); !errors.As(err, &blocked) || blocked.Code != "migration_required" || out.Len() != 0 {
		t.Fatal("JSON entered interactive gate", out.String(), err)
	}
}
