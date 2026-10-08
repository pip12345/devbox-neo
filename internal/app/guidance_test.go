package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/store"
)

func TestLifecycleGuidanceSeparatesActionsFromErrors(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error, code string, command ...string) {
		t.Helper()
		var actionable *commanderror.Error
		if !errors.As(err, &actionable) || actionable.Code != code || actionable.Target != result.Session || len(actionable.Next) != 1 {
			t.Fatalf("unexpected guidance: %v", err)
		}
		if !reflect.DeepEqual(actionable.Next[0].Command, append([]string{"dbx"}, command...)) || strings.Contains(err.Error(), "\n") {
			t.Fatal("actions must be structured, not embedded in the message", actionable)
		}
	}
	lock, err := e.Store.Lock(ctx, sessionRecord(t, e, result.SessionID).Directory, sessionRecord(t, e, result.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.planSessionDeletion(ctx, []*store.Locked{lock}, false)
	lock.Close()
	check(err, "container_present", "delete", result.Session, "--container")
	err = e.ChangeNetwork(ctx, result.SessionID, "", "bridge", false)
	check(err, "primary_network_protected", "recreate", result.Session)
	forgetSession(t, e, result.SessionID)
	before := runtimeMutations(d)
	err = e.Stop(ctx, result.SessionID, "", false)
	check(err, "container_missing", "status", result.Session)
	err = e.Exec(ctx, result.SessionID, "", []string{"true"}, false)
	check(err, "container_missing", "recreate", result.Session)
	_, err = e.Start(ctx, result.SessionID, "")
	check(err, "container_missing", "recreate", result.Session)
	_, err = e.NetworkFacts(ctx, result.Session, "")
	check(err, "container_missing", "recreate", result.Session)
	for _, connect := range []bool{false, true} {
		err = e.ChangeNetwork(ctx, result.Session, "", "secondary", connect)
		check(err, "container_missing", "recreate", result.Session)
	}
	if runtimeMutations(d) != before {
		t.Fatal("missing-container guidance mutated runtime")
	}
}
