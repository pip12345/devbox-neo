package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLifecycleGuidanceSeparatesActionsFromErrors(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), ";") {
			t.Fatalf("unexpected guidance: %v", err)
		}
	}
	_, err = e.DeleteSessions(ctx, []string{result.Name}, "", false)
	check(err, "\n\nDelete the container first:\n  devbox-neo delete "+result.Name+"\nThen retry session deletion.")
	err = e.ChangeNetwork(ctx, result.Name, "", "bridge", false)
	check(err, "\nChange the network configuration, then recreate:\n  devbox-neo recreate "+result.Name)
	d.Forget(result.Name)
	err = e.Stop(ctx, result.Name, "", false)
	check(err, "\n\nInspect its session state:\n  devbox-neo session show "+result.Name)
	err = e.Exec(ctx, result.Name, "", []string{"true"}, false)
	check(err, "\n\nNext:\n  devbox-neo start "+result.Name+"\nThen retry your command.")
	d.Fail = func(args []string) error {
		if args[0] == "image" && args[1] == "inspect" {
			return errors.New("image unavailable")
		}
		return nil
	}
	_, err = e.Start(ctx, result.Name, "")
	check(err, "\n\nNext:\n  devbox-neo recreate "+result.Name)
}
