package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func incompleteCLIState(t *testing.T, e *app.Engine) (string, string) {
	t.Helper()
	name := "dbx-42643868833d.main"
	root := filepath.Join(e.Store.Home, "sessions", name)
	if err := os.MkdirAll(filepath.Join(root, "harnesses/pi/stores/home/extensions-pip"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "harnesses/pi/stores/home/extensions-pip/copied-file"), []byte("partial copy"), 0600); err != nil {
		t.Fatal(err)
	}
	return name, root
}

func TestDeleteIncompleteCreationCLIJSON(t *testing.T) {
	for _, scope := range []string{"--container", "--session"} {
		for _, preview := range []bool{false, true} {
			t.Run(scope+map[bool]string{false: " execute", true: " preview"}[preview], func(t *testing.T) {
				e, _, _ := namedCLIFixture(t)
				name, root := incompleteCLIState(t, e)
				local := ""
				cmd := deleteCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
				cmd.SetContext(context.Background())
				out := new(bytes.Buffer)
				cmd.SetOut(out)
				args := []string{name, scope, "--json"}
				if preview {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
				if err := cmd.Execute(); err != nil {
					t.Fatal(err, out.String())
				}
				var result app.DeleteResult
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err, out.String())
				}
				if scope == "--session" {
					if len(result.IncompleteDirectories) != 1 || len(result.RetainedIncompleteDirectories) != 0 {
						t.Fatal(result)
					}
				} else if len(result.IncompleteDirectories) != 0 || len(result.RetainedIncompleteDirectories) != 1 {
					t.Fatal(result)
				}
				_, err := os.Stat(root)
				if os.IsNotExist(err) != (scope == "--session" && !preview) {
					t.Fatal(result, err)
				}
			})
		}
	}
}

func TestIncompleteSessionMenuDeletesAndReturnsToBrowser(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader(""))
	name, root := incompleteCLIState(t, f.e)
	input := &choiceScript{t: t, out: out, steps: []string{"@Delete", "@Preview deletion", "@Back", "@Delete…", "y"}}
	f.cmd.SetIn(input)
	f.m = testMenuCommand(f.cmd.Context(), input, out, f.cmd)
	if err := f.session(app.View{Target: name}); err != nil {
		t.Fatal(err, out.String())
	}
	text := out.String()
	for _, want := range []string{
		"Delete: Container and saved data/history",
		"Would delete incomplete creation directory " + name,
		"Incomplete creation directory: " + name,
		"images retained",
		"Deleted incomplete creation directory " + name,
	} {
		if !strings.Contains(text, want) {
			t.Fatal("missing cleanup behavior", want, text)
		}
	}
	if strings.Contains(text, "No resources deleted") || strings.Contains(text, "no longer exists; refresh") {
		t.Fatal("cleanup left the user on a broken session", text)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal(err, text)
	}
}

func TestDeleteIncompleteContainerScopeExplainsRetention(t *testing.T) {
	var out bytes.Buffer
	if err := printDeleteResult(&out, app.DeleteResult{RetainedIncompleteDirectories: []string{"dbx-incomplete.main"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Incomplete creation directory retained", "--session", "images will be retained"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("silent no-op", out.String())
		}
	}
}
