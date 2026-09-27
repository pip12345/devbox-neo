package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func TestRenameCommandPreviewAndJSON(t *testing.T) {
	e, q, id := namedCLIFixture(t)
	ctx := context.Background()
	before := sessionRecord(t, e, id)
	run := func(args ...string) (string, error) {
		name := ""
		cmd := renameCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(ctx)
		return out.String(), err
	}
	out, err := run(q.Workspace, "--name", q.LocalName, "--to", "Second", "--dry-run")
	if err != nil || !strings.Contains(out, "Would rename:") {
		t.Fatal(out, err)
	}
	if after := sessionRecord(t, e, id); !reflect.DeepEqual(before, after) {
		t.Fatal("preview changed session")
	}
	out, err = run(id, "--to", "Second", "--json")
	var result app.RenameResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.SessionID != id || result.LocalName != "Second" {
		t.Fatal(out, err)
	}
	if after := sessionRecord(t, e, id); !reflect.DeepEqual(before.Applied, after.Applied) || before.Directory != after.Directory {
		t.Fatal("rename changed runtime/storage")
	}
}
func TestRenameCommandRequiresNewNameBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{{"."}, {".", "--to", ""}, {".", "--to", "bad/name"}} {
		name := ""
		cmd := renameCommand(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid input initialized engine")
			return nil, nil
		}, &name)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted invalid name", args)
		}
	}
	cmd, _, err := New().Find([]string{"rename"})
	if err != nil || cmd.Name() != "rename" || !strings.Contains(cmd.Long, renameEffects) {
		t.Fatal(cmd, err)
	}
}
func TestRenameMenuEditsLabelWithoutRebuilding(t *testing.T) {
	script := &choiceScript{t: t, steps: []string{"@Rename session", "@New session name:", "Second", "@Rename session"}}
	f, out, _, id := frontendFixture(t, script)
	script.out = out
	before := sessionRecord(t, f.e, id)
	if err := f.session(app.View{Target: id}); err != nil {
		t.Fatal(err, out.String())
	}
	after := sessionRecord(t, f.e, id)
	if after.Settings.LocalName != "Second" || !reflect.DeepEqual(before.Applied, after.Applied) || f.focusItem != id {
		t.Fatal(after, out.String())
	}
	if strings.Contains(out.String(), "Proceed?") {
		t.Fatal("metadata edit added a redundant confirmation", out.String())
	}
}
