package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func TestStatusRetainsDetailsWithInvalidConfig(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			result, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			saved := sessionRecord(t, e, result.SessionID)
			if missing {
				forgetSession(t, e, result.SessionID)
			}
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
			details, err := e.Status(ctx, result.SessionID, "")
			if err != nil || details.Record == nil {
				t.Fatal(details, err)
			}
			if !reflect.DeepEqual(*details.Record, saved) || details.Exists == missing || details.ConfigError == "" || details.Desired != "" || details.Active == nil {
				t.Fatal("config error hid saved details or container state", details)
			}
		})
	}
}

func TestStatusDefaultMatchesSavedNameAndID(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	key, err := store.WorkspaceKey(r.Settings.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "state/workspaces", key+".json")
	check := func(want bool) {
		t.Helper()
		for _, target := range []struct{ path, name string }{{made.SessionID, ""}, {q.Workspace, q.LocalName}} {
			details, err := e.Status(ctx, target.path, target.name)
			if err != nil || details.Default != want || details.DefaultError != "" {
				t.Fatal("wrong default selection", details.Default, details.DefaultError, err)
			}
		}
	}
	check(false)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("status seeded absent default state", err)
	}
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	check(true)
	details, err := e.Status(ctx, q.Workspace, "")
	if err != nil || !details.Default {
		t.Fatal("default-selected folder status lost default flag", details, err)
	}
	for _, selected := range []store.DefaultSession{
		{ID: strings.Repeat("a", 32)},
		{ID: strings.Repeat("b", 32)},
	} {
		data, err := json.Marshal(map[string]any{"version": 2, "workspace": q.Workspace, "default_session": selected})
		if err != nil {
			t.Fatal(err)
		}
		write(t, path, string(data))
		check(false)
	}
	if err := e.Store.ClearDefault(ctx, q.Workspace); err != nil {
		t.Fatal(err)
	}
	check(false)
	write(t, path, "broken")
	details, err = e.Status(ctx, made.SessionID, "")
	if err != nil || details.Record == nil || details.Record.ID != r.ID || details.Default || details.DefaultError == "" || details.Desired != environment.NoChange {
		t.Fatal("broken default hid exact-session details", details, err)
	}
	if string(getFile(t, path)) != "broken" {
		t.Fatal("status repaired invalid default state")
	}
}

func TestStatusReportsLiveLeasesWithoutReaping(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, sessionRecord(t, e, result.SessionID).Directory, sessionRecord(t, e, result.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	live, liveErr := lock.Lease("exec")
	stale, staleErr := lock.Lease("shell")
	lock.Close()
	if liveErr != nil || staleErr != nil {
		t.Fatal(liveErr, staleErr)
	}
	stale.Process.Boot = "previous-boot"
	b, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "state/leases", result.SessionID, stale.ID+".json")
	write(t, path, string(b))
	details, err := e.Status(ctx, result.SessionID, "")
	if err != nil || len(details.Active) != 1 || details.Active[0].ID != live.ID || details.Desired != environment.NoChange {
		t.Fatal("status lost live commands or configuration check", details, err)
	}
	if string(getFile(t, path)) != string(b) {
		t.Fatal("status mutated stale lease")
	}
}

func TestStatusAllLeavesPendingTransfersUnclassified(t *testing.T) {
	e, _, q := fixture(t)
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, result.SessionID)
	destination, err := environment.Identify(t.TempDir(), q.LocalName)
	if err != nil {
		t.Fatal(err)
	}
	destination.Name = environment.ResourceName(destination.Workspace, destination.LocalName, "destination")
	journal := store.Transfer{Version: 3, ContainerName: environment.ResourceName(destination.Workspace, destination.LocalName, "container"), SourceContainerID: source.Applied.SetupContainer, ID: strings.Repeat("a", 32), Mode: "clone", Phase: "prepare", Source: environment.Identity{Binding: source.Settings.Binding, Name: source.Directory}, Destination: destination, SourceID: source.ID, DestinationID: strings.Repeat("b", 32), Started: time.Now().UTC(), Desired: source.Applied.Fingerprints}
	if err := journal.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "state/transfers", source.Directory+".json"), string(b))
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	report, err := e.StatusAll(context.Background(), "")
	if err != nil || len(report.Sessions) != 2 {
		t.Fatal("pending endpoints were not retained", report, err)
	}
	for _, view := range report.Sessions {
		if view.Pending == nil || view.Desired != "" || view.ConfigError != "" {
			t.Fatal("pending endpoint was resolved", view)
		}
		details, err := e.Status(context.Background(), view.Target, "")
		if err != nil || details.Pending == nil || details.Desired != "" || details.ConfigError != "" {
			t.Fatal("single status resolved a pending endpoint", details, err)
		}
		if (details.Record != nil) != (view.Target == source.ID) {
			t.Fatal("incorrect record for pending endpoint", details)
		}
	}
}

func TestStatusAllClassifiesEachContainerWithoutMutations(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	names := map[string]string{}
	records := map[string]store.Record{}
	workspaces := map[string]string{}
	for _, profile := range []string{"clean", "runtime", "container", "image", "invalid", "corrupt", "recordless", "mismatch", "missing"} {
		q.Workspace = t.TempDir()
		workspaces[profile] = q.Workspace
		q.LocalName = profile
		q.Sources[0].Path = filepath.Join(e.Store.Home, "profiles", profile)
		write(t, filepath.Join(e.Store.Home, "profiles", profile, "config.json"), `{"version":1,"harness":"pi"}`)
		result, err := createAndOpen(ctx, e, q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
			t.Fatal(err)
		}
		names[profile] = result.SessionID
		records[profile] = sessionRecord(t, e, result.SessionID)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/runtime/before-open.sh"), "echo updated")
	write(t, filepath.Join(e.Store.Home, "profiles/container/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	write(t, filepath.Join(e.Store.Home, "profiles/image/docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nRUN echo updated\n")
	write(t, filepath.Join(e.Store.Home, "profiles/invalid/config.json"), "broken")
	corrupt, _ := e.Store.RecordPath(records["corrupt"].Directory)
	bad := records["corrupt"]
	bad.Applied.Fingerprints.Image = "invalid"
	data, _ := json.Marshal(bad)
	write(t, corrupt, string(data))
	recordless, _ := e.Store.RecordPath(records["recordless"].Directory)
	if err := os.Remove(recordless); err != nil {
		t.Fatal(err)
	}
	mismatch, _ := d.Snapshot(records["mismatch"].Applied.Creation.Name)
	mismatch.Image = "sha256:" + strings.Repeat("f", 64)
	d.SetContainer(mismatch)
	d.Forget(records["missing"].Applied.Creation.Name)
	foreign, _ := d.Snapshot(records["clean"].Applied.Creation.Name)
	foreign.Name = "/foreign"
	foreign.ID = strings.Repeat("f", 64)
	foreign.Config.Labels[docker.Namespace+".installation"] = "foreign"
	d.SetContainer(foreign)

	beforeRecords := map[string]string{}
	for _, r := range records {
		p, _ := e.Store.RecordPath(r.Directory)
		if b, err := os.ReadFile(p); err == nil {
			beforeRecords[r.Directory] = string(b)
		}
	}
	before := len(d.History())
	report, err := e.StatusAll(ctx, "")
	views := report.Sessions
	if err != nil || len(views) != 9 || len(report.UnmatchedContainers) != 1 || report.UnmatchedContainers[0].ContainerName != records["recordless"].Applied.Creation.Name {
		t.Fatal("wrong environment scope or missing unmatched warning", report, err)
	}
	calls := d.History()[before:]
	if len(calls) != 2 || !reflect.DeepEqual(calls[0][:2], []string{"container", "ls"}) || !reflect.DeepEqual(calls[1][:2], []string{"container", "inspect"}) {
		t.Fatal("status did more than batched Docker inventory", calls)
	}
	byName := map[string]View{}
	for i, view := range views {
		if i > 0 && views[i-1].Target >= view.Target {
			t.Fatal("status is not sorted by name")
		}
		if view.Uncommitted {
			continue
		}
		if view.Target == names["missing"] {
			if view.Exists || view.Running || view.Desired != environment.NoChange {
				t.Fatal("missing container was treated as a config error", view)
			}
		} else if !view.Exists || !view.Running {
			t.Fatal("desired state hid live state", view)
		}
		byName[view.Target] = view
	}
	for profile, change := range map[string]environment.Change{"clean": environment.NoChange, "runtime": environment.RuntimeSync, "container": environment.Recreate, "image": environment.RebuildAndRecreate} {
		view := byName[names[profile]]
		if view.Desired != change || view.Error != "" || view.ConfigError != "" {
			t.Fatal(profile, view)
		}
	}
	if view := byName[names["invalid"]]; view.ConfigError == "" || view.Error != "" || view.Desired != "" {
		t.Fatal("invalid desired configuration was not independent", view)
	}
	for _, profile := range []string{"corrupt", "mismatch"} {
		if view := byName[names[profile]]; view.Error == "" || view.Desired != "" {
			t.Fatal("unverifiable container reported a drift result", profile, view)
		}
	}
	for name, contents := range beforeRecords {
		p, _ := e.Store.RecordPath(name)
		if got := string(getFile(t, p)); got != contents {
			t.Fatal("status changed a session record", name)
		}
	}
	for _, profile := range []string{"container", "corrupt", "missing"} {
		report, err = e.StatusAll(ctx, workspaces[profile])
		if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Target != names[profile] {
			t.Fatal("folder filter lost an environment", profile, report, err)
		}
	}
	report, err = e.StatusAll(ctx, workspaces["recordless"])
	if err != nil || len(report.Sessions) != 0 || len(report.UnmatchedContainers) != 1 {
		t.Fatal("folder filter lost the unmatched container", report, err)
	}
	report, err = e.StatusAll(ctx, t.TempDir())
	if err != nil || report.Sessions == nil || len(report.Sessions) != 0 || report.UnmatchedContainers == nil || len(report.UnmatchedContainers) != 0 {
		t.Fatal("empty selection must retain empty arrays", report, err)
	}
	d.Fail = func([]string) error { return errors.New("Docker unavailable") }
	if _, err = e.StatusAll(ctx, ""); err == nil {
		t.Fatal("unavailable Docker appeared successful")
	}
}
