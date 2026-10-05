package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStoppedAccessRefusesMissingStoresBeforeSync(t *testing.T) {
	for _, tc := range []struct {
		name, action, store string
		changedHarness      bool
	}{
		{"config sync cannot replace a lost root", "open", "config", false},
		{"non-config store without nested mounts", "start", "data", false},
		{"incompatible definition still checks recorded stores", "exec", "config", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			configPath := filepath.Join(q.Sources[0].Path, "config.json")
			write(t, configPath, `{"harness":"opencode"}`)
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			before := sessionRecord(t, e, made.SessionID)
			root := filepath.Join(e.Store.Home, "sessions", before.Directory, "harnesses/opencode/stores", tc.store)
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			if tc.changedHarness {
				write(t, configPath, `{"harness":"pi"}`)
			}
			calls := len(d.History())
			switch tc.action {
			case "open":
				_, err = e.Open(ctx, openRequest(q))
			case "start":
				_, err = e.Start(ctx, before.ID, "")
			case "exec":
				err = e.Exec(ctx, before.ID, "", []string{"true"}, false)
			}
			if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "saved harness store") {
				t.Fatal("missing store accepted", err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("missing history replaced", err)
			}
			for _, args := range d.History()[calls:] {
				if args[0] != "container" {
					t.Fatal("failed startup mutated Docker", args)
				}
			}
			if after := sessionRecord(t, e, before.ID); !reflect.DeepEqual(before, after) {
				t.Fatal("failed startup advanced saved state")
			}
		})
	}
}

func TestTransferRefusesMissingActiveHarnessStoresBeforeMutation(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, made.SessionID)
			// An unrelated retained harness must not hide the active harness's loss.
			root := filepath.Join(e.Store.Home, "sessions", r.Directory, "harnesses/pi")
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			seedThird(t, e.Store.Home)
			write(t, filepath.Join(filepath.Dir(root), "third/stores/state/history"), "retained")
			calls := len(d.History())
			_, err = e.Transfer(ctx, TransferOptions{Mode: mode, Source: r.ID, Destination: t.TempDir()})
			if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "saved harness store") {
				t.Fatal(err)
			}
			if pending, err := pendingTransfer(e, r.ID); err != nil || pending != nil {
				t.Fatal("failed preflight reserved endpoints", pending, err)
			}
			for _, args := range d.History()[calls:] {
				if args[0] != "container" {
					t.Fatal("failed preflight mutated Docker", args)
				}
			}
		})
	}
}

func TestCommittedMoveRetainsSourceWhenDestinationStoresAreMissing(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, made.SessionID)
	opts := TransferOptions{Mode: "relocate", Source: source.ID, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.Applied.SetupContainer {
			return errors.New("interrupted cleanup")
		}
		return nil
	}
	if _, err := e.Transfer(ctx, opts); err == nil {
		t.Fatal("expected interruption")
	}
	d.Fail = nil
	journal, err := pendingTransfer(e, source.ID)
	if err != nil || journal == nil || journal.Phase != "committed" {
		t.Fatal(journal, err)
	}
	dest := sessionRecord(t, e, journal.Destination.Name)
	root := filepath.Join(e.Store.Home, "sessions", dest.Directory, "harnesses/pi/stores/home")
	write(t, filepath.Join(root, "history"), "new destination history")
	backup := filepath.Join(t.TempDir(), "store")
	if err := os.Rename(root, backup); err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, dest.ID)
	calls := len(d.History())
	if _, err := e.Transfer(ctx, opts); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost destination store accepted", err)
	}
	if len(d.History()) != calls {
		t.Fatal("cleanup touched Docker before checking committed history")
	}
	if _, err := e.Store.Read(ctx, source.Directory); err != nil {
		t.Fatal("source removed after destination data loss", err)
	}
	if pending, err := pendingTransfer(e, source.ID); err != nil || pending == nil {
		t.Fatal("lost retry reservation", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("empty history fabricated", err)
	}
	if err := os.Rename(backup, root); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transfer(ctx, opts); err != nil {
		t.Fatal("cleanup failed after store restored", err)
	}
	if got := string(getFile(t, filepath.Join(root, "history"))); got != "new destination history" {
		t.Fatal("source recopied over committed history", got)
	}
	if _, err := e.Store.Read(ctx, source.Directory); !os.IsNotExist(err) {
		t.Fatal("source not removed", err)
	}
}
