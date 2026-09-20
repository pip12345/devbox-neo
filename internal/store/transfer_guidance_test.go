package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/environment"
)

func TestTransferRetryCommandsKeepExactEndpoints(t *testing.T) {
	workspace := t.TempDir()
	identity := func(folder, profile string) environment.Identity {
		t.Helper()
		id, err := environment.Identify(folder, profile, profile == "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	source := identity(workspace, "basic")
	destination := identity(t.TempDir(), "basic")
	for _, tt := range []struct {
		mode     string
		from, to environment.Identity
		want     []string
	}{
		{"relocate", source, destination, []string{"devbox-neo", "copy", "--move", source.Name, destination.Workspace}},
		{"clone", source, destination, []string{"devbox-neo", "copy", source.Name, destination.Workspace}},
		{"clone", source, identity(destination.Workspace, "other"), []string{"devbox-neo", "copy", source.Name, destination.Workspace, "--to", ".profile-other"}},
		{"relocate", source, identity(workspace, ""), []string{"devbox-neo", "copy", "--move", workspace, "--from", ".profile-basic", "--to", ".project"}},
		{"clone", identity(workspace, ""), source, []string{"devbox-neo", "copy", workspace, "--from", ".project", "--to", ".profile-basic"}},
	} {
		j := Transfer{Mode: tt.mode, Source: tt.from, Destination: tt.to}
		got := j.RetryStep()
		if !reflect.DeepEqual(got.Command, tt.want) {
			t.Fatal(got.Command, tt.want)
		}
		if filepath.Base(got.Command[0]) != "devbox-neo" {
			t.Fatal(got)
		}
	}
}
