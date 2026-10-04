package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
	"fmt"
)

func sessionSnapshot(t *testing.T, e *Engine, id string) (docker.Container, bool) {
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
func pendingTransfer(e *Engine, id string) (*store.Transfer, error) {
	journals, err := e.Store.Transfers()
	if err != nil {
		return nil, err
	}
	var found *store.Transfer
	for i := range journals {
		if journals[i].SourceID == id {
			if found != nil {
				return nil, fmt.Errorf("ambiguous journal")
			}
			found = &journals[i]
		}
	}
	return found, nil
}

func forgetSession(t *testing.T, e *Engine, id string) {
	t.Helper()
	r := sessionRecord(t, e, id)
	e.Docker.Runner.(*dockertest.Daemon).Forget(r.Applied.Creation.Name)
}
