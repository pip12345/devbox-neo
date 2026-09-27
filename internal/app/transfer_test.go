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

	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
)

func TestTransferResumesJournalBeforeDestinationCreation(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, opened.SessionID)
	dest := t.TempDir()
	spec, err := e.Resolve(Request{Workspace: dest, LocalName: q.LocalName, Sources: q.Sources})
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := fsutil.ID()
	newID, _ := fsutil.ID()
	spec.Identity.Name = environment.ResourceName(dest, q.LocalName, "directory")
	j := store.Transfer{Version: 3, ContainerName: environment.ResourceName(dest, q.LocalName, nonce), SourceContainerID: source.Applied.SetupContainer, ID: nonce, Mode: "clone", Phase: "prepare", Source: environment.Identity{Binding: source.Settings.Binding, Name: source.Directory}, Destination: spec.Identity, SourceID: source.ID, DestinationID: newID, Started: time.Now().UTC(), Desired: spec.Fingerprints}
	names := []string{source.Directory, spec.Identity.Name}
	sort.Strings(names)
	locks, err := e.Store.LockAll(ctx, names, map[string]string{source.Directory: source.ID, spec.Identity.Name: newID})
	if err != nil {
		t.Fatal(err)
	}
	a, b := locks[0], locks[1]
	if a.Name != source.Directory {
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
	if _, err = e.Transfer(ctx, TransferOptions{Mode: "clone", Source: opened.SessionID, Destination: dest}); err != nil {
		t.Fatal(err)
	}
	if sessionRecord(t, e, spec.Identity.Name).ID != newID {
		t.Fatal("retry allocated another identity")
	}
}
func TestTransferIdentityStateAndRunningPolicy(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		for _, running := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "-stopped", true: "-running"}[running], func(t *testing.T) {
				e, _, q := fixture(t)
				ctx := context.Background()
				opened, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				original := sessionRecord(t, e, opened.SessionID)
				rel := "harnesses/pi/stores/home/sessions/history.json"
				write(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, opened.SessionID).Directory, rel), "conversation")
				if running {
					if _, err = e.Start(ctx, opened.SessionID, ""); err != nil {
						t.Fatal(err)
					}
				}
				target := t.TempDir()
				opts := TransferOptions{Mode: mode, Source: opened.SessionID, Destination: target}
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
				copied := sessionRecord(t, e, result.Destination)
				if (copied.ID == original.ID) != (mode == "relocate") {
					t.Fatal("wrong identity policy")
				}
				if copied.Settings.Workspace != target || copied.Action != store.TransferCommand(mode) {
					t.Fatal("wrong destination contract", copied.Settings.Binding)
				}
				view, err := e.Status(ctx, result.Destination, "")
				if err != nil || view.Desired != environment.NoChange || len(view.PendingInputChanges) != 0 || copied.Applied.Inputs.Container.Workspace != copied.Settings.Workspace {
					t.Fatal("destination did not commit its own input baseline", view, err)
				}
				data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, rel))
				if err != nil || string(data) != "conversation" {
					t.Fatal("lost state", err)
				}
				c, exists := sessionSnapshot(t, e, result.Destination)
				if !exists || c.State.Running != (mode == "relocate" && running) {
					t.Fatal("wrong intended state")
				}
				_, sourceExists := e.Docker.Runner.(*dockertest.Daemon).Snapshot(original.Applied.Creation.Name)
				if sourceExists != (mode == "clone") {
					t.Fatal("wrong source teardown")
				}
				for _, name := range []string{opened.SessionID, result.Destination} {
					pending, err := e.Store.Pending(name)
					if err != nil || pending != nil {
						t.Fatal("journal remained", pending, err)
					}
				}
				if mode == "clone" {
					if sessionRecord(t, e, opened.SessionID).ID != original.ID {
						t.Fatal("source changed")
					}
				} else if _, err = e.Store.Read(ctx, original.Directory); !os.IsNotExist(err) {
					t.Fatal("source record remained", err)
				}
			})
		}
	}
}
func TestTransferFromMissingWorkspaceAndContainer(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, _, q := fixture(t)
			q.Sources = q.Sources[:1]
			if err := os.RemoveAll(filepath.Join(q.Workspace, ".devbox")); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			opened, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			forgetSession(t, e, opened.SessionID)
			if err = os.Remove(q.Workspace); err != nil {
				t.Fatal(err)
			}
			if _, err = e.Transfer(ctx, TransferOptions{Mode: mode, Source: opened.SessionID, Destination: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTransferNamesAndDryRun(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	opts := TransferOptions{Mode: "clone", Source: q.Workspace, LocalName: q.LocalName, As: "project", DryRun: true}
	before := count(d, "create")
	result, err := e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if count(d, "create") != before {
		t.Fatal("dry run created container")
	}
	if _, err = e.Store.Find(ctx, result.Destination, nil); !os.IsNotExist(err) {
		t.Fatal("dry run created state", err)
	}
	if j, err := pendingTransfer(e, opened.SessionID); err != nil || j != nil {
		t.Fatal("dry run journaled", err)
	}
	opts.DryRun = false
	result, err = e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if sessionRecord(t, e, result.Destination).Settings.LocalName != "project" {
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
	if _, err = e.Start(ctx, opened.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	rel := "harnesses/pi/stores/home/sessions/history.json"
	sourcePath := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, opened.SessionID).Directory, rel)
	write(t, sourcePath, "before")
	opts := TransferOptions{Mode: "relocate", Source: opened.SessionID, Destination: t.TempDir()}
	source := sessionRecord(t, e, opened.SessionID)
	d.Fail = func(args []string) error {
		if args[0] == "exec" && args[len(args)-1] == "pi" {
			return errors.New("prepare interrupted")
		}
		return nil
	}
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("ignored failure")
	}
	c, _ := sessionSnapshot(t, e, opened.SessionID)
	if !c.State.Running {
		t.Fatal("source not restarted")
	}
	var pendingError *commanderror.Error
	if _, err = e.Start(ctx, opened.SessionID, ""); !errors.As(err, &pendingError) || pendingError.Code != "pending_transfer" || len(pendingError.Next) != 1 || strings.Join(pendingError.Next[0].Command, " ") != "devbox-neo copy --move "+opened.SessionID+" "+opts.Destination+" --as "+q.LocalName {
		t.Fatal("pending source not guarded", err)
	}
	j, err := pendingTransfer(e, opened.SessionID)
	if err != nil || j == nil || j.Phase != "prepare" {
		t.Fatal(j, err)
	}
	if _, err = e.Start(ctx, j.Destination.Name, ""); err == nil {
		t.Fatal("pending destination not guarded")
	}
	if _, err = e.DeleteContainers(ctx, Selection{Targets: []string{opened.SessionID}}, true); err == nil {
		t.Fatal("forced deletion bypassed transfer guard")
	}
	if _, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{opened.SessionID}}, Force: true, Scope: DeleteSession}); err == nil {
		t.Fatal("combined deletion bypassed transfer guard")
	}
	image, err := e.Docker.InspectImage(ctx, source.Applied.ImageTag)
	if err != nil || image.ID != source.Applied.ImageID {
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
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, rel))
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
	source := sessionRecord(t, e, opened.SessionID)
	opts := TransferOptions{Mode: "relocate", Source: opened.SessionID, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.Applied.SetupContainer {
			return errors.New("source removal interrupted")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored source removal error")
	}
	journal, err := pendingTransfer(e, opened.SessionID)
	if err != nil || journal == nil || journal.Phase != "committed" {
		t.Fatal(journal, err)
	}
	created := count(d, "create")
	rel := "harnesses/pi/stores/home/sessions/after-commit.json"
	write(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, rel), "destination owns this")
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken desired config")
	d.Fail = nil
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if count(d, "create") != created {
		t.Fatal("committed retry recreated destination")
	}
	if _, err = os.Stat(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, rel)); err != nil {
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
	destOpen, err := e.Create(ctx, Request{Workspace: dest, LocalName: q.LocalName, Sources: q.Sources})
	if err != nil {
		t.Fatal(err)
	}
	before := count(d, "create")
	if _, err = e.Transfer(ctx, TransferOptions{Mode: "relocate", Source: opened.SessionID, Destination: dest}); err == nil {
		t.Fatal("adopted destination")
	}
	if count(d, "create") != before {
		t.Fatal("preflight mutated destination")
	}
	_ = sessionRecord(t, e, destOpen.SessionID)
	lock, err := e.Store.Lock(ctx, sessionRecord(t, e, opened.SessionID).Directory, sessionRecord(t, e, opened.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("exec")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	opts := TransferOptions{Mode: "relocate", Source: opened.SessionID, Destination: t.TempDir()}
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("transferred active source")
	}
	lock, _ = e.Store.Lock(ctx, sessionRecord(t, e, opened.SessionID).Directory, sessionRecord(t, e, opened.SessionID).ID)
	if err = lock.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	id, _ := environment.Identify(opts.Destination, q.LocalName)
	foreign, _ := sessionSnapshot(t, e, opened.SessionID)
	foreign.Name = "/" + id.Name
	foreign.Config.Labels = map[string]string{}
	d.SetContainer(foreign)
	if _, err = e.Transfer(ctx, opts); err == nil {
		t.Fatal("adopted foreign destination")
	}
}
