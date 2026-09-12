package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
)

func TestTypedSessionAbsenceDoesNotCreateOnStart(t *testing.T) {
	e, d, q := fixture(t)
	_, err := e.Locate(context.Background(), q.Workspace, q.Profile)
	var missing *commanderror.Error
	if !errors.Is(err, os.ErrNotExist) || !errors.As(err, &missing) || missing.Code != "session_missing" {
		t.Fatal(err)
	}
	_, err = e.Start(context.Background(), q.Workspace, q.Profile)
	if !errors.Is(err, os.ErrNotExist) || !errors.As(err, &missing) || missing.Code != "session_missing" || len(missing.Next) != 1 {
		t.Fatal(err)
	}
	if len(d.History()) != 0 {
		t.Fatal("missing session reached Docker")
	}
}

func TestActionableLeaseConflictAndCorruptStateStayFailClosed(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, opened.Name)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("test", "running")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	var busy *commanderror.Error
	if err := e.Stop(ctx, opened.Name, "", false); !errors.As(err, &busy) || busy.Code != "session_busy" || len(busy.Next) != 1 || strings.Contains(strings.Join(busy.Next[0].Command, " "), "--force") {
		t.Fatal(err)
	}
	lock, _ = e.Store.Lock(ctx, opened.Name)
	lock.Release(lease.ID)
	lock.Close()
	path, _ := e.Store.RecordPath(opened.Name)
	write(t, path, "broken")
	before := len(d.History())
	var invalid *commanderror.Error
	if _, err := e.Start(ctx, opened.Name, ""); !errors.As(err, &invalid) || invalid.Code != "invalid_session_record" || errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(d.History()) != before {
		t.Fatal("corrupt record was treated as fresh state")
	}
}

func TestRecoveryErrorRetainsDockerFailureIdentity(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Forget(opened.Name)
	failure := &docker.ExitError{Code: 19, Operation: "image"}
	d.Fail = func(args []string) error {
		if args[0] == "image" && args[1] == "inspect" {
			return failure
		}
		return nil
	}
	_, err = e.Start(ctx, opened.Name, "")
	var recovery *commanderror.Error
	if !errors.As(err, &recovery) || recovery.Code != "recovery_unavailable" || !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestPiDefaultsToFullscreenAndAllowsLaterOverrides(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, opened.Name)
	if !reflect.DeepEqual(r.Launch.Args, []string{"--tui-mode", "fullscreen"}) || !reflect.DeepEqual(r.Launch.Continue, []string{"-c"}) {
		t.Fatal(r.Launch)
	}
	q.Continue = true
	q.Args = []string{"--tui-mode", "regular"}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "--tui-mode", "fullscreen", "-c", "--tui-mode", "regular"}
	found := false
	for _, args := range d.History() {
		if args[0] == "exec" && argvSuffix(args, want) {
			found = true
		}
	}
	if !found {
		t.Fatal("one-off override was not last in launch argv")
	}
	if !reflect.DeepEqual(record(t, e, opened.Name).Launch.Args, r.Launch.Args) {
		t.Fatal("one-off TUI override was persisted")
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","harness_args":["--tui-mode","regular"]}`)
	q.Args = nil
	q.Continue = false
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal(err)
	}
	if got := record(t, e, opened.Name).Launch.Args; !reflect.DeepEqual(got, []string{"--tui-mode", "fullscreen", "--tui-mode", "regular"}) {
		t.Fatal("profile override lost", got)
	}
}

func TestMissingHarnessGuidanceUsesResolvedOwner(t *testing.T) {
	for _, project := range []bool{false, true} {
		e, _, q := fixture(t)
		want := []string{"devbox-neo", "profile", "init", "test", "--harness", "<name>"}
		write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1}`)
		if project {
			q.Profile = ""
			write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1}`)
			want = []string{"devbox-neo", "project", "init", q.Workspace, "--harness", "<name>"}
		}
		_, err := e.Open(context.Background(), q)
		var missing *commanderror.Error
		if !errors.As(err, &missing) || missing.Code != "harness_required" || len(missing.Next) != 1 || !reflect.DeepEqual(missing.Next[0].Command, want) {
			t.Fatal(err, missing)
		}
	}
}
