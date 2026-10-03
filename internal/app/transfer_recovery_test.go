package app

import (
	"context"
	"devbox/internal/commanderror"
	"devbox/internal/harness"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransferChecksHarnessPolicyBeforeMutation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	h, err := harness.Load(e.Store.Home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	h.Definition.Session.Clone = false
	h.Definition.Session.Relocate = false
	b, err := json.Marshal(h.Definition)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "harnesses/pi/harness.json"), string(b))
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := count(d, "stop") + count(d, "create")
	for _, mode := range []string{"clone", "relocate"} {
		if _, err = e.Transfer(ctx, TransferOptions{Mode: mode, Source: opened.SessionID, Destination: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "does not support") {
			t.Fatal(err)
		}
	}
	if count(d, "stop")+count(d, "create") != before {
		t.Fatal("policy preflight changed containers")
	}
}
func TestTransferJournalSurvivesSourceDeletion(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, opened.SessionID)
	opts := TransferOptions{Mode: "relocate", Source: q.Workspace, LocalName: q.LocalName, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.Applied.SetupContainer {
			return errors.New("interruption")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored interruption")
	}
	d.Fail = nil
	c, exists, err := e.inspect(ctx, source)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if err = e.Docker.Remove(ctx, c, e.owner(source)); err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, source.Directory, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Delete(); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	report, err := e.List(ctx, "")
	views := report.Sessions
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, view := range views {
		if view.Target == opened.SessionID && view.Pending != nil {
			seen = true
		}
	}
	if !seen {
		t.Fatal("cleanup journal vanished from inventory")
	}
	details, err := e.Status(ctx, opened.SessionID, "")
	if err != nil || details.Pending == nil || details.Record == nil || details.Desired != "" {
		t.Fatal("pending cleanup not inspectable", err)
	}
	var pendingError *commanderror.Error
	if _, err = e.Open(ctx, q); !errors.As(err, &pendingError) || pendingError.Code != "pending_transfer" {
		t.Fatal("source name became available during cleanup", err)
	}
	if _, err = e.Create(ctx, q); !errors.As(err, &pendingError) || pendingError.Code != "pending_transfer" {
		t.Fatal("create reused a reserved source during cleanup", err)
	}
	// Destination stores remain authoritative. Cleanup releases the endpoints
	// without rebuilding missing runtime or recopying the deleted source.
	dest := sessionRecord(t, e, result.Destination)
	history := filepath.Join(e.Store.Home, "sessions", dest.Directory, "harnesses/pi/stores/home/history")
	write(t, history, "new destination history")
	forgetSession(t, e, result.Destination)
	delete(d.Images, dest.Applied.ImageID)
	delete(d.Images, dest.Applied.ImageTag)
	creates, builds := count(d, "create"), count(d, "build")
	configPath := filepath.Join(e.Store.Home, "profiles/test/config.json")
	write(t, configPath, "invalid current config must not block committed cleanup")
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.Read(ctx, source.Directory); !os.IsNotExist(err) {
		t.Fatal("source returned", err)
	}
	if count(d, "create") != creates || count(d, "build") != builds || string(getFile(t, history)) != "new destination history" {
		t.Fatal("cleanup rebuilt runtime or recopied committed stores")
	}
	if _, exists := sessionSnapshot(t, e, result.Destination); exists {
		t.Fatal("transfer cleanup rebuilt destination runtime")
	}
	write(t, configPath, `{"version":1,"harness":"pi","network":"host"}`)
	if _, err := e.Recreate(ctx, Request{Workspace: result.Destination}, false); err != nil {
		t.Fatal("explicit destination recreation failed", err)
	}
	if _, exists := sessionSnapshot(t, e, result.Destination); !exists {
		t.Fatal("destination not recreated")
	}
}
func TestTransferRetriesPreparedButUncommittedDestination(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, opened.SessionID)
	rel := "harnesses/pi/stores/home/sessions/history.json"
	write(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, opened.SessionID).Directory, rel), "first snapshot")
	opts := TransferOptions{Mode: "clone", Source: opened.SessionID, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "stop" && args[len(args)-1] != source.Applied.SetupContainer {
			return errors.New("destination stop unavailable")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored failure")
	}
	prepared := sessionRecord(t, e, result.Destination)
	journal, err := pendingTransfer(e, opened.SessionID)
	if err != nil || journal == nil || journal.Phase != "prepare" {
		t.Fatal(journal, err)
	}
	write(t, filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, opened.SessionID).Directory, rel), "new authoritative snapshot")
	d.Fail = nil
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	current := sessionRecord(t, e, result.Destination)
	if current.ID != prepared.ID || current.Applied.SetupContainer == prepared.Applied.SetupContainer {
		t.Fatal("retry did not retain identity and replace preparation")
	}
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, rel))
	if err != nil || string(data) != "new authoritative snapshot" {
		t.Fatal("stale snapshot won", err)
	}
}
func TestTransferRetryRetainsExplicitDestinationName(t *testing.T) {
	for _, committed := range []bool{false, true} {
		for _, retryTo := range []string{"exact", "folder"} {
			t.Run(map[bool]string{false: "prepare", true: "committed"}[committed]+retryTo, func(t *testing.T) {
				e, d, q := fixture(t)
				ctx := context.Background()
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				source := sessionRecord(t, e, made.SessionID)
				options := TransferOptions{Mode: "clone", Source: made.SessionID, Destination: t.TempDir(), As: "Review"}
				if committed {
					options.Mode, options.Destination = "relocate", q.Workspace
				}
				configPath := filepath.Join(q.Sources[0].Path, "config.json")
				d.Fail = func(args []string) error {
					if (!committed && args[0] == "build") || (committed && args[0] == "rm" && args[len(args)-1] == source.Applied.SetupContainer) {
						return errors.New("transfer interrupted")
					}
					return nil
				}
				if _, err = e.Transfer(ctx, options); err == nil {
					t.Fatal("expected interrupted transfer")
				}
				d.Fail = nil
				journal, err := pendingTransfer(e, made.SessionID)
				if err != nil || journal == nil || journal.Destination.LocalName != options.As || (journal.Phase == "committed") != committed {
					t.Fatal(journal, err)
				}
				for _, selector := range []string{"", ".invalid", strings.Repeat("x", 65)} {
					invalid := *journal
					invalid.Destination.LocalName = selector
					if err := invalid.Validate(); err == nil {
						t.Fatal("accepted inconsistent journal selector", selector)
					}
				}
				bad := options
				bad.As = "Other"
				if _, err = e.Transfer(ctx, bad); err == nil || !strings.Contains(err.Error(), "different destination or selection") {
					t.Fatal("accepted an unrecorded selector", err)
				}
				if committed {
					write(t, configPath, "committed recovery must not resolve current config")
				}
				if retryTo == "folder" {
					options.Source, options.LocalName = source.Settings.Workspace, source.Settings.LocalName
				}
				result, err := e.Transfer(ctx, options)
				if err != nil || result.Destination != journal.DestinationID || sessionRecord(t, e, result.Destination).ID != journal.DestinationID {
					t.Fatal("retry changed or rejected its destination", result, err)
				}
				if pending, err := pendingTransfer(e, made.SessionID); err != nil || pending != nil {
					t.Fatal("retry did not release its reservation", pending, err)
				}
			})
		}
	}
}

func TestTransferCancellationRestoresSource(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, opened.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			cancel()
			return context.Canceled
		}
		return nil
	}
	_, err = e.Transfer(run, TransferOptions{Mode: "relocate", Source: opened.SessionID, Destination: t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c, _ := sessionSnapshot(t, e, opened.SessionID)
	if !c.State.Running {
		t.Fatal("cancelled operation did not restart source")
	}
}
func TestTransferCopiesOpaqueLinksNotAuthOrCaches(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, opened.SessionID).Directory, "harnesses/pi/stores/home")
	write(t, filepath.Join(base, "sessions/keep.json"), "keep")
	write(t, filepath.Join(base, "auth.json"), "excluded auth overlay placeholder")
	external := filepath.Join(t.TempDir(), "outside")
	write(t, external, "not traversed")
	if err = os.Symlink(external, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(e.Store.Home, "cache/harnesses/pi/transfer-marker")
	write(t, cache, "shared")
	result, err := e.Transfer(ctx, TransferOptions{Mode: "clone", Source: opened.SessionID, Destination: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.Destination).Directory, "harnesses/pi/stores/home")
	link, err := os.Readlink(filepath.Join(target, "link"))
	if err != nil || link != external {
		t.Fatal("opaque link not preserved", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "auth.json")); err == nil && string(b) == "excluded auth overlay placeholder" {
		t.Fatal("auth overlay copied")
	}
	if b, err := os.ReadFile(cache); err != nil || string(b) != "shared" {
		t.Fatal("shared cache changed", err)
	}
}
