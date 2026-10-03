package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"devbox/internal/commanderror"
)

func TestCreationDefaultIsOptInAndDeletionNeverSelectsSurvivor(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("first session implicitly became default", selected, err)
	}
	q.LocalName, q.MakeDefault = "chosen", true
	chosen, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Locate(ctx, q.Workspace, ""); err != nil || selected.ID != chosen.SessionID {
		t.Fatal(selected, err)
	}
	q.LocalName, q.MakeDefault = "unselected", false
	unselected, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.ID != chosen.SessionID {
		t.Fatal("unchecked creation changed the default", selected, err)
	}
	q.LocalName, q.MakeDefault = "replacement", true
	replacement, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.ID != replacement.SessionID {
		t.Fatal("explicit creation did not replace default", selected, err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{chosen.SessionID, unselected.SessionID, replacement.SessionID}}, Scope: DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("deletion selected a survivor", selected, err)
	}
	if _, err := e.Locate(ctx, first.SessionID, ""); err != nil {
		t.Fatal("surviving session lost", err)
	}
}

func TestCreationFailureDoesNotReplaceDefault(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	q.MakeDefault = true
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.LocalName = "failed"
	failure := errors.New("build failed")
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			return failure
		}
		return nil
	}
	if _, err := e.Create(ctx, q); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.ID != first.SessionID {
		t.Fatal("failed creation changed the default", selected, err)
	}
}

func TestCreationDefaultFailureKeepsSessionAndGivesExactRepair(t *testing.T) {
	e, d, q := fixture(t)
	q.MakeDefault = true
	first, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	q.LocalName = "created"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Fail = func(args []string) error {
		// Cancel after the record has committed, before default selection.
		if args[0] == "stop" {
			cancel()
		}
		return nil
	}
	result, err := e.Create(ctx, q)
	var failure *commanderror.Error
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.Code != "create_default_failed" {
		t.Fatal(err)
	}
	if len(failure.Next) != 1 || !reflect.DeepEqual(failure.Next[0].Command, []string{"dbx", "edit", result.SessionID, "--default"}) {
		t.Fatal("incorrect retry", failure.Next)
	}
	if r := sessionRecord(t, e, result.SessionID); r.Settings.LocalName != q.LocalName {
		t.Fatal("committed session lost")
	}
	if selected, err := e.Store.ReadDefault(context.Background(), q.Workspace); err != nil || selected == nil || selected.ID != first.SessionID {
		t.Fatal("failed selection replaced previous default", selected, err)
	}
}

func TestCreationStopFailureStillAppliesRequestedDefault(t *testing.T) {
	e, d, q := fixture(t)
	q.MakeDefault = true
	failure := errors.New("stop failed")
	d.Fail = func(args []string) error {
		if args[0] == "stop" {
			return failure
		}
		return nil
	}
	result, err := e.Create(context.Background(), q)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(context.Background(), q.Workspace); err != nil || selected == nil || selected.ID != result.SessionID {
		t.Fatal("committed creation lost requested default", selected, err)
	}
}
