package store

import (
	"reflect"
	"testing"

	"devbox/internal/environment"
)

func TestTransferRetryCommandsKeepExactEndpoints(t *testing.T) {
	workspace := t.TempDir()
	identity := func(folder, name string) environment.Identity {
		t.Helper()
		id, err := environment.Identify(folder, name)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	source := identity(workspace, "Main")
	destination := identity(t.TempDir(), "Main")
	other := identity(workspace, "Experiment")
	for _, test := range []struct {
		mode     string
		from, to environment.Identity
		want     []string
	}{
		{"relocate", source, destination, []string{"dbx", "copy", "--move", source.Name, destination.Workspace, "--as", "Main"}},
		{"clone", source, destination, []string{"dbx", "copy", source.Name, destination.Workspace, "--as", "Main"}},
		{"relocate", source, other, []string{"dbx", "copy", "--move", source.Name, workspace, "--as", "Experiment"}},
		{"clone", other, source, []string{"dbx", "copy", other.Name, workspace, "--as", "Main"}},
	} {
		journal := Transfer{Mode: test.mode, Source: test.from, Destination: test.to}
		if got := journal.RetryStep(); !reflect.DeepEqual(got.Command, test.want) {
			t.Fatal(got.Command, test.want)
		}
	}
}
