package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/commanderror"
)

func TestLifecycleGuidanceSeparatesActionsFromErrors(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error, code string, command ...string) {
		t.Helper()
		var actionable *commanderror.Error
		if !errors.As(err, &actionable) || actionable.Code != code || actionable.Target != result.Name || len(actionable.Next) != 1 {
			t.Fatalf("unexpected guidance: %v", err)
		}
		if !reflect.DeepEqual(actionable.Next[0].Command, append([]string{"devbox-neo"}, command...)) || strings.Contains(err.Error(), "\n") {
			t.Fatal("actions must be structured, not embedded in the message", actionable)
		}
	}
	_, err = e.DeleteSessions(ctx, []string{result.Name}, "", false)
	check(err, "container_present", "delete", result.Name)
	err = e.ChangeNetwork(ctx, result.Name, "", "bridge", false)
	check(err, "primary_network_protected", "recreate", result.Name)
	d.Forget(result.Name)
	err = e.Stop(ctx, result.Name, "", false)
	check(err, "container_missing", "session", "show", result.Name)
	err = e.Exec(ctx, result.Name, "", []string{"true"}, false)
	check(err, "container_missing", "start", result.Name)
	d.Fail = func(args []string) error {
		if args[0] == "image" && args[1] == "inspect" {
			return errors.New("image unavailable")
		}
		return nil
	}
	_, err = e.Start(ctx, result.Name, "")
	check(err, "recovery_unavailable", "recreate", result.Name)
}
