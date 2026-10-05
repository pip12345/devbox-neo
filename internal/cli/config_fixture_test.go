package cli

import (
	"context"
	"path/filepath"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"devbox/internal/store"
)

func harnessSetting(name string) *string { return &name }

func namedCLIFixture(t *testing.T) (*app.Engine, app.CreateRequest, string) {
	t.Helper()
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := resource.Service{Home: state.Home}
	owner := testConfigOwner(t, state.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{Harness: harnessSetting("pi")}); err != nil {
		t.Fatal(err)
	}
	e := &app.Engine{Store: state, Docker: docker.Runtime{Runner: &dockertest.Daemon{}}, UID: 1000, GID: 1000}
	q := app.CreateRequest{Workspace: t.TempDir(), LocalName: "Main", Sources: testConfigSources(state.Home, "base")}
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return e, q, sessionRecord(t, e, made.SessionID).Directory
}

func testConfigOwner(t *testing.T, home, reference string) resource.Owner {
	t.Helper()
	owner, err := (resource.Service{Home: home}).ConfigDirectory(reference, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

// Baseline lifecycle fixtures deliberately pair a session name with one named
// config. Workflow tests pass their own references to exercise independent names.
func testConfigSources(home, name string) []config.Reference {
	return []config.Reference{{Label: name, Kind: config.ReferenceFixed, Path: filepath.Join(home, "configs", name)}}
}
