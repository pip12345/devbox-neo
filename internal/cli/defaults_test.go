package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func TestFolderEditUsesMainBrowser(t *testing.T) {
	f, browser, q, _ := frontendFixture(t, strings.NewReader("0\n"))
	if err := f.browse(false); err != nil {
		t.Fatal(err)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return f.e, nil }, &name)
	var direct bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&direct)
	cmd.SetErr(&direct)
	cmd.SetArgs([]string{q.Workspace})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if direct.String() != browser.String() {
		t.Fatalf("edit opened a different browser:\n%s\nmain browser:\n%s", direct.String(), browser.String())
	}
}
