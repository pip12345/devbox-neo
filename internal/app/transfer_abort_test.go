package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func interruptedTransfer(t *testing.T, mode string, prepared bool) (*Engine, *dockertest.Daemon, CreateRequest, store.Record, TransferOptions, store.Transfer) {
	t.Helper()
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "relocate" {
		if _, err := e.Start(ctx, made.SessionID, ""); err != nil {
			t.Fatal(err)
		}
	}
	r := sessionRecord(t, e, made.SessionID)
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "sessions", r.Directory, "harnesses/pi/stores/home/history"), "source history")
	opts := TransferOptions{Mode: mode, Source: r.ID, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if (!prepared && args[0] == "build") || (prepared && args[0] == "stop" && args[len(args)-1] != r.Applied.SetupContainer) {
			return errors.New("interrupted preparation")
		}
		return nil
	}
	if _, err := e.Transfer(ctx, opts); err == nil {
		t.Fatal("preparation unexpectedly succeeded")
	}
	d.Fail = nil
	j, err := e.Store.ReadTransfer(r.Directory)
	if err != nil || j == nil || j.Phase != "prepare" {
		t.Fatal(j, err)
	}
	return e, d, q, r, opts, *j
}

func TestPreparationRetryUsesCorrectedCurrentConfig(t *testing.T) {
	e, _, q, source, opts, journal := interruptedTransfer(t, "clone", false)
	ctx := context.Background()
	write(t, filepath.Join(q.Sources[0].Path, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nRUN true\n")
	write(t, filepath.Join(q.Sources[0].Path, "setup.sh"), "echo corrected")
	write(t, filepath.Join(e.Store.Home, "sessions", source.Directory, "harnesses/pi/stores/home/history"), "latest source history")
	result, err := e.Transfer(ctx, opts)
	if err != nil || result.Destination != journal.Destination.Name {
		t.Fatal("config repair could not retry exact transfer", result, err)
	}
	dest := sessionRecord(t, e, result.Destination)
	if string(getFile(t, filepath.Join(e.Store.Home, "sessions", dest.Directory, "harnesses/pi/stores/home/history"))) != "latest source history" {
		t.Fatal("retry reused stale copy")
	}
	if len(dest.Applied.Inputs.Container.Setup) != 1 {
		t.Fatal("corrected setup was not applied")
	}
}

func TestAbortWithDestinationSortingBeforeSource(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			root := t.TempDir()
			q.Workspace = filepath.Join(root, "z-source")
			destination := filepath.Join(root, "a-destination")
			for _, path := range []string{q.Workspace, destination} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			source := sessionRecord(t, e, made.SessionID)
			history := filepath.Join(e.Store.Home, "sessions", source.Directory, "harnesses/pi/stores/home/history")
			write(t, history, "source history")
			if mode == "relocate" {
				if _, err := e.Start(ctx, source.Directory, ""); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("interrupted preparation")
			d.Fail = func(args []string) error {
				if args[0] == "build" {
					return failure
				}
				return nil
			}
			if _, err := e.Transfer(ctx, TransferOptions{Mode: mode, Source: source.Directory, Destination: destination}); !errors.Is(err, failure) {
				t.Fatal("did not interrupt transfer preparation", err)
			}
			d.Fail = nil
			journal, err := e.Store.ReadTransfer(source.Directory)
			if err != nil || journal == nil || journal.Phase != "prepare" {
				t.Fatal(journal, err)
			}
			if journal.Destination.Name >= journal.Source.Name {
				t.Fatal("fixture must put destination before source", journal)
			}
			result, err := e.AbortTransfer(ctx, source.Directory)
			if err != nil || !result.Aborted || result.SessionID != source.ID || result.Destination != journal.Destination.Name {
				t.Fatal(result, err)
			}
			if pending, err := e.Store.PendingID(source.ID); err != nil || pending != nil {
				t.Fatal("abort retained reservation", pending, err)
			}
			if string(getFile(t, history)) != "source history" {
				t.Fatal("abort changed source history")
			}
			container, exists := sessionSnapshot(t, e, source.ID)
			if !exists || container.State.Running != (mode == "relocate") {
				t.Fatal("abort changed source runtime lifetime", container.State)
			}
		})
	}
}

func TestAbortRetainsSourceWithoutResolvingBrokenConfig(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, _, q, source, _, journal := interruptedTransfer(t, mode, false)
			write(t, filepath.Join(q.Sources[0].Path, "config.json"), "broken config")
			ctx := context.Background()
			result, err := e.AbortTransfer(ctx, source.Directory)
			if err != nil || !result.Aborted || result.SessionID != source.ID {
				t.Fatal(result, err)
			}
			if pending, err := e.Store.PendingID(source.ID); err != nil || pending != nil {
				t.Fatal("reservation not released", pending, err)
			}
			if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", journal.Destination.Name)); !os.IsNotExist(err) {
				t.Fatal("destination attempt retained", err)
			}
			if string(getFile(t, filepath.Join(e.Store.Home, "sessions", source.Directory, "harnesses/pi/stores/home/history"))) != "source history" {
				t.Fatal("source history lost")
			}
			if selected, err := e.Store.ReadDefault(ctx, source.Settings.Workspace); err != nil || selected == nil || selected.ID != source.ID {
				t.Fatal("abort changed default", selected, err)
			}
			c, _ := sessionSnapshot(t, e, source.ID)
			if c.State.Running != (mode == "relocate") {
				t.Fatal("abort changed source lifetime")
			}
			if err := e.Exec(ctx, source.ID, "", []string{"true"}, false); err != nil {
				t.Fatal("source remained blocked", err)
			}
		})
	}
}

func TestAbortKeepsReservationUntilDestinationCleanupSucceeds(t *testing.T) {
	e, d, _, source, _, journal := interruptedTransfer(t, "clone", true)
	dest, err := e.Store.Read(context.Background(), journal.Destination.Name)
	if err != nil {
		t.Fatal(err)
	}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == dest.Applied.SetupContainer {
			return errors.New("remove denied")
		}
		return nil
	}
	if _, err := e.AbortTransfer(context.Background(), source.ID); err == nil {
		t.Fatal("cleanup error hidden")
	}
	if pending, err := e.Store.PendingID(source.ID); err != nil || pending == nil {
		t.Fatal("failed abort released source", err)
	}
	d.Fail = nil
	if _, err := e.AbortTransfer(context.Background(), source.ID); err != nil {
		t.Fatal("abort not retryable", err)
	}
}

func TestAbortCleansRecordedDestinationAfterDockerRename(t *testing.T) {
	e, d, _, source, _, journal := interruptedTransfer(t, "clone", true)
	c, ok := d.Snapshot(journal.ContainerName)
	if !ok {
		t.Fatal("destination missing")
	}
	d.Forget(journal.ContainerName)
	c.Name = "/renamed-destination"
	d.SetContainer(c)
	if _, err := e.AbortTransfer(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot("renamed-destination"); exists {
		t.Fatal("abort removed state but left recorded runtime alive")
	}
}

func TestAbortRejectsBusyOrForeignEndpoints(t *testing.T) {
	for _, unsafe := range []string{"busy", "foreign"} {
		t.Run(unsafe, func(t *testing.T) {
			e, d, _, source, _, journal := interruptedTransfer(t, "clone", true)
			ctx := context.Background()
			if unsafe == "busy" {
				lock, err := e.Store.Lock(ctx, source.Directory, source.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := lock.Lease("test"); err != nil {
					t.Fatal(err)
				}
				lock.Close()
			} else {
				c, ok := d.Snapshot(journal.ContainerName)
				if !ok {
					t.Fatal("destination missing")
				}
				c.Config.Labels[docker.Namespace+".installation"] = "foreign"
				d.SetContainer(c)
			}
			before := runtimeMutations(d)
			if _, err := e.AbortTransfer(ctx, source.ID); err == nil {
				t.Fatal("unsafe abort succeeded")
			}
			if pending, err := e.Store.ReadTransfer(source.Directory); err != nil || pending == nil || runtimeMutations(d) != before {
				t.Fatal("rejected abort mutated or released endpoints", err)
			}
		})
	}
}

func TestCorruptTransferIdentifiesTheBlockingJournal(t *testing.T) {
	e, _, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "state/transfers/broken.json")
	write(t, path, "broken")
	var failure *commanderror.Error
	if _, err := e.Locate(context.Background(), made.SessionID, ""); !errors.As(err, &failure) || failure.Code != "invalid_transfer_journal" || failure.Target != path {
		t.Fatal("corrupt journal lost its repair target", err)
	}
}

func TestCommittedTransferCannotBeAborted(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, made.SessionID)
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.Applied.SetupContainer {
			return errors.New("source cleanup failed")
		}
		return nil
	}
	if _, err := e.Transfer(ctx, TransferOptions{Mode: "relocate", Source: source.ID, Destination: t.TempDir()}); err == nil {
		t.Fatal("cleanup unexpectedly succeeded")
	}
	before := len(d.History())
	var blocked *commanderror.Error
	if _, err := e.AbortTransfer(ctx, source.ID); !errors.As(err, &blocked) || blocked.Code != "transfer_committed" {
		t.Fatal("committed destination was abortable", err)
	}
	if len(d.History()) != before {
		t.Fatal("rejected abort touched Docker")
	}
}
