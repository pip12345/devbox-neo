package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"devbox/internal/app"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestEditWorkspaceSavesDesiredSettingsWithoutApplying(t *testing.T) {
	e, _, id := namedCLIFixture(t)
	before := sessionRecord(t, e, id)
	if err := e.SetDefault(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	next := t.TempDir()
	cmd.SetArgs([]string{id, "--workspace", next, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	var result store.Record
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	after := sessionRecord(t, e, id)
	if result.ID != before.ID || after.Settings.Workspace != next || after.Directory != before.Directory || !reflect.DeepEqual(after.Applied, before.Applied) {
		t.Fatal("workspace edit applied runtime changes", after)
	}
	if selected, err := e.Store.ReadDefault(context.Background(), before.Settings.Workspace); err != nil || selected != nil {
		t.Fatal(selected, err)
	}
}
