package cli

import (
	"context"
	"errors"
	"os"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func sessionRecord(t *testing.T, e *app.Engine, id string) store.Record {
	t.Helper()
	r, err := e.Locate(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func sessionSnapshot(t *testing.T, e *app.Engine, id string) (docker.Container, bool) {
	t.Helper()
	r, err := e.Locate(context.Background(), id, "")
	if errors.Is(err, os.ErrNotExist) {
		return docker.Container{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return e.Docker.Runner.(*dockertest.Daemon).Snapshot(r.Applied.Creation.Name)
}
func forgetSession(t *testing.T, e *app.Engine, id string) {
	t.Helper()
	r := sessionRecord(t, e, id)
	e.Docker.Runner.(*dockertest.Daemon).Forget(r.Applied.Creation.Name)
}
