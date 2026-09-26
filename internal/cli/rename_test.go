package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

func TestRenameCommandPreviewAndJSON(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	ctx := context.Background()
	before, err := e.Store.Read(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string, error) {
		localName := ""
		cmd := renameCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &localName)
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(ctx)
		return out.String(), stderr.String(), err
	}
	out, _, err := run(q.Workspace, "--name", q.LocalName, "--to", "Second", "--dry-run")
	if err != nil || !strings.Contains(out, "Would rename:") || !strings.Contains(out, renameEffects) {
		t.Fatal(out, err)
	}
	if after, err := e.Store.Read(ctx, name); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preview changed session", err)
	}
	out, stderr, err := run(name, "--to", "Second", "--json")
	var result app.TransferResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Mode != "relocate" || result.SessionID != before.ID || result.LocalName != "Second" {
		t.Fatal(out, err)
	}
	if !strings.Contains(stderr, renameEffects) {
		t.Fatal("rename did not disclose effects before transfer", stderr)
	}
}

func TestRenameCommandRequiresNewNameBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{{"."}, {".", "--to", ""}, {".", "--to", "bad/name"}} {
		name := ""
		cmd := renameCommand(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid input initialized the engine")
			return nil, nil
		}, &name)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("accepted missing or invalid name", args)
		}
	}
	cmd, _, err := New().Find([]string{"rename"})
	if err != nil || cmd.Name() != "rename" || !strings.Contains(cmd.Long, renameEffects) {
		t.Fatal("rename missing from CLI or help", cmd, err)
	}
}

func TestRenameMenuConfirmsAndReturnsToBrowser(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "accept"}[accept], func(t *testing.T) {
			script := &choiceScript{t: t, steps: []string{"@Rename session", "@New session name:", "Second", "@Rename session"}}
			if accept {
				script.steps = append(script.steps, "y")
			} else {
				script.steps = append(script.steps, "n", "0", "0")
			}
			f, out, q, name := frontendFixture(t, script)
			script.out = out
			before, err := f.e.Store.Read(context.Background(), name)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.session(app.View{Name: name}); err != nil {
				t.Fatal(err, out.String())
			}
			_, err = f.e.Store.Read(context.Background(), name)
			if os.IsNotExist(err) != accept {
				t.Fatal("wrong source retention", err)
			}
			next := environment.ContainerName(q.Workspace, "Second")
			after, err := f.e.Store.Read(context.Background(), next)
			if accept && (err != nil || after.ID != before.ID || f.focusItem != next) {
				t.Fatal(after, err, f.focusItem)
			}
			if !accept && !os.IsNotExist(err) {
				t.Fatal("declined rename created destination", err)
			}
			if !strings.Contains(out.String(), renameEffects) || !strings.Contains(out.String(), "Proceed? [y/N]") {
				t.Fatal("menu did not disclose consequences", out.String())
			}
		})
	}
}
