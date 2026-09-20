package app

import (
	"context"
	"devbox/internal/commanderror"
	"devbox/internal/fsutil"
	"devbox/internal/store"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"devbox/internal/environment"
)

func TestTransferResumesJournalBeforeDestinationCreation(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := record(t, e, opened.Name)
	dest := t.TempDir()
	spec, err := e.Resolve(Request{Workspace: dest, Profile: q.Profile})
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := fsutil.ID()
	newID, _ := fsutil.ID()
	j := store.Transfer{Version: 1, ID: nonce, Mode: "clone", Phase: "prepare", Source: source.Identity, Destination: spec.Identity, SourceID: source.ID, DestinationID: newID, Started: time.Now().UTC(), Desired: spec.Fingerprints}
	names := []string{source.Identity.Name, spec.Identity.Name}
	sort.Strings(names)
	locks, err := e.Store.LockAll(ctx, names)
	if err != nil {
		t.Fatal(err)
	}
	a, b := locks[0], locks[1]
	if a.Name != source.Identity.Name {
		a, b = b, a
	}
	if err = a.SaveTransfer(b, j); err != nil {
		t.Fatal(err)
	}
	altered := j
	altered.DestinationID, _ = fsutil.ID()
	if err = a.SaveTransfer(b, altered); err == nil {
		t.Fatal("journal contract changed")
	}
	for _, lock := range locks {
		if err = lock.RequireAvailable(); err == nil {
			t.Fatal("journal did not reserve both endpoints")
		}
	}
	store.CloseAll(locks)
	if _, err = e.Transfer(ctx, TransferOptions{Mode: "clone", Source: opened.Name, Destination: dest}); err != nil {
		t.Fatal(err)
	}
	if record(t, e, spec.Identity.Name).ID != newID {
		t.Fatal("retry allocated another identity")
	}
}
func TestTransferIdentityStateAndRunningPolicy(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		for _, running := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "-stopped", true: "-running"}[running], func(t *testing.T) {
				e, d, q := fixture(t)
				ctx := context.Background()
				opened, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				original := record(t, e, opened.Name)
				rel := "harnesses/pi/stores/home/sessions/history.json"
				write(t, filepath.Join(e.Store.Home, "sessions", opened.Name, rel), "conversation")
				if running {
					if _, err = e.Start(ctx, opened.Name, ""); err != nil {
						t.Fatal(err)
					}
				}
				target := t.TempDir()
				opts := TransferOptions{Mode: mode, Source: opened.Name, Destination: target}
				result, err := e.Transfer(ctx, opts)
				if mode == "clone" && running {
					var runningError *commanderror.Error
					if !errors.As(err, &runningError) || runningError.Code != "container_running" {
						t.Fatal(err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				copied := record(t, e, result.Destination)
				if (copied.ID == original.ID) != (mode == "relocate") {
					t.Fatal("wrong identity policy")
				}
				if copied.Identity.Workspace != target || copied.Action != store.TransferCommand(mode) {
					t.Fatal("wrong destination contract", copied.Identity)
				}
				view, err := e.Status(ctx, result.Destination, "")
				if err != nil || view.Desired != environment.NoChange || len(view.PendingInputChanges) != 0 || copied.Inputs.Container.Identity != copied.Identity {
					t.Fatal("destination did not commit its own input baseline", view, err)
				}
				data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", result.Destination, rel))
				if err != nil || string(data) != "conversation" {
					t.Fatal("lost state", err)
				}
				c, exists := d.Snapshot(result.Destination)
				if !exists || c.State.Running != (mode == "relocate" && running) {
					t.Fatal("wrong intended state")
				}
				_, sourceExists := d.Snapshot(opened.Name)
				if sourceExists != (mode == "clone") {
					t.Fatal("wrong source teardown")
				}
				for _, name := range []string{opened.Name, result.Destination} {
					pending, err := e.Store.Pending(name)
					if err != nil || pending != nil {
						t.Fatal("journal remained", pending, err)
					}
				}
				if mode == "clone" {
					if record(t, e, opened.Name).ID != original.ID {
						t.Fatal("source changed")
					}
				} else if _, err = e.Store.Read(ctx, opened.Name); !os.IsNotExist(err) {
					t.Fatal("source record remained", err)
				}
			})
		}
	}
}
func TestTransferFromMissingWorkspaceAndContainer(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			opened, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			d.Forget(opened.Name)
			if err = os.Remove(q.Workspace); err != nil {
				t.Fatal(err)
			}
			if _, err = e.Transfer(ctx, TransferOptions{Mode: mode, Source: opened.Name, Destination: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTransferSlotsAndDryRun(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	opts := TransferOptions{Mode: "clone", Source: q.Workspace, From: ".profile-test", To: ".project", DryRun: true}
	before := count(d, "create")
	result, err := e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if count(d, "create") != before {
		t.Fatal("dry run created container")
	}
	if _, err = os.Stat(filepath.Join(e.Store.Home, "sessions", result.Destination)); !os.IsNotExist(err) {
		t.Fatal("dry run created state", err)
	}
	if j, err := e.Store.ReadTransfer(opened.Name); err != nil || j != nil {
		t.Fatal("dry run journaled", err)
	}
	opts.DryRun = false
	result, err = e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if record(t, e, result.Destination).Identity.Slot != "project" {
		t.Fatal("wrong destination slot")
	}
}
func TestTransferFailedPreparationRestoresSourceAndRetries(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, opened.Name, ""); err != nil {
		t.Fatal(err)
	}
	rel := "harnesses/pi/stores/home/sessions/history.json"
	sourcePath := filepath.Join(e.Store.Home, "sessions", opened.Name, rel)
	write(t, sourcePath, "before")
	opts := TransferOptions{Mode: "relocate", Source: opened.Name, Destination: t.TempDir()}
	source := record(t, e, opened.Name)
	d.Fail = func(args []string) error {
		if args[0] == "exec" && args[len(args)-1] == "pi" {
			return errors.New("prepare interrupted")
		}
		return nil
	}
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("ignored failure")
	}
	c, _ := d.Snapshot(opened.Name)
	if !c.State.Running {
		t.Fatal("source not restarted")
	}
	var pendingError *commanderror.Error
	if _, err = e.Start(ctx, opened.Name, ""); !errors.As(err, &pendingError) || pendingError.Code != "pending_transfer" || len(pendingError.Next) != 1 || strings.Join(pendingError.Next[0].Command, " ") != "devbox-neo copy --move "+opened.Name+" "+opts.Destination {
		t.Fatal("pending source not guarded", err)
	}
	j, err := e.Store.ReadTransfer(opened.Name)
	if err != nil || j == nil || j.Phase != "prepare" {
		t.Fatal(j, err)
	}
	if _, err = e.Start(ctx, j.Destination.Name, ""); err == nil {
		t.Fatal("pending destination not guarded")
	}
	if _, err = e.DeleteContainers(ctx, Selection{Targets: []string{opened.Name}}, true); err == nil {
		t.Fatal("forced deletion bypassed transfer guard")
	}
	if _, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{opened.Name}}, Force: true, Scope: DeleteSession}); err == nil {
		t.Fatal("combined deletion bypassed transfer guard")
	}
	image, err := e.Docker.InspectImage(ctx, source.ImageTag)
	if err != nil || image.ID != source.ImageID {
		t.Fatal("source image tag not restored", err)
	}
	configPath := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, configPath, `{"version":1,"harness":"pi","env":["TOKEN=not-for-journals"]}`)
	if _, err = e.Transfer(ctx, opts); err == nil || !strings.Contains(err.Error(), "inputs changed") || strings.Contains(err.Error(), "not-for-journals") {
		t.Fatal("changed destination was accepted or leaked", err)
	}
	write(t, configPath, `{"version":1,"harness":"pi"}`)
	write(t, sourcePath, "new source write after rollback")
	d.Fail = nil
	result, err := e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", result.Destination, rel))
	if err != nil || string(data) != "new source write after rollback" {
		t.Fatal("retry used stale snapshot", err)
	}
}
func TestTransferCommittedRetryOnlyCleansSource(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := record(t, e, opened.Name)
	opts := TransferOptions{Mode: "relocate", Source: opened.Name, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.SetupContainer {
			return errors.New("source removal interrupted")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored source removal error")
	}
	journal, err := e.Store.ReadTransfer(opened.Name)
	if err != nil || journal == nil || journal.Phase != "committed" {
		t.Fatal(journal, err)
	}
	created := count(d, "create")
	rel := "harnesses/pi/stores/home/sessions/after-commit.json"
	write(t, filepath.Join(e.Store.Home, "sessions", result.Destination, rel), "destination owns this")
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken desired config")
	d.Fail = nil
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if count(d, "create") != created {
		t.Fatal("committed retry recreated destination")
	}
	if _, err = os.Stat(filepath.Join(e.Store.Home, "sessions", result.Destination, rel)); err != nil {
		t.Fatal("retry replaced destination", err)
	}
}
func TestTransferRejectsOccupiedDestinationAndActiveSource(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	destOpen, err := e.Create(ctx, Request{Workspace: dest, Profile: q.Profile})
	if err != nil {
		t.Fatal(err)
	}
	before := count(d, "create")
	if _, err = e.Transfer(ctx, TransferOptions{Mode: "relocate", Source: opened.Name, Destination: dest}); err == nil {
		t.Fatal("adopted destination")
	}
	if count(d, "create") != before {
		t.Fatal("preflight mutated destination")
	}
	_ = record(t, e, destOpen.Name)
	lock, err := e.Store.Lock(ctx, opened.Name)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("exec")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	opts := TransferOptions{Mode: "relocate", Source: opened.Name, Destination: t.TempDir()}
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("transferred active source")
	}
	lock, _ = e.Store.Lock(ctx, opened.Name)
	if err = lock.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	id, _ := environment.Identify(opts.Destination, q.Profile, false)
	foreign, _ := d.Snapshot(opened.Name)
	foreign.Name = "/" + id.Name
	foreign.Config.Labels = map[string]string{}
	d.SetContainer(foreign)
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("adopted foreign destination")
	}
}
