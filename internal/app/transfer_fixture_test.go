package app

import (
	"context"
	"testing"
	"time"

	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func transferJournal(t *testing.T, source store.Record, destination environment.Binding, mode string) store.Transfer {
	t.Helper()
	op, err := fsutil.ID()
	if err != nil {
		t.Fatal(err)
	}
	id := source.ID
	tree := "data/" + op
	if mode == "clone" {
		id, err = fsutil.SessionID()
		if err != nil {
			t.Fatal(err)
		}
		tree = store.InitialStateTree
	}
	j := store.Transfer{Version: 5, ID: op, Mode: mode, Phase: "prepare", Source: source.Settings.Binding, Destination: destination,
		SourceID: source.ID, DestinationID: id, SourceTree: source.StateTree, DestinationTree: tree,
		SourceContainerID: source.Applied.ContainerID, SourceImageID: source.Applied.ImageID, SourceImageTag: source.Applied.ImageTag,
		ContainerName: environment.ResourceName(id, op), Started: time.Now().UTC()}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
	return j
}

func saveTransferJournal(t *testing.T, e *Engine, j store.Transfer) {
	t.Helper()
	locks, source, destination, err := e.lockTransfer(context.Background(), j.SourceID, j.DestinationID)
	if err != nil {
		t.Fatal(err)
	}
	defer store.CloseAll(locks)
	if _, err := destination.Dir("."); err != nil {
		t.Fatal(err)
	}
	if err := source.SaveTransfer(destination, j); err != nil {
		t.Fatal(err)
	}
}
