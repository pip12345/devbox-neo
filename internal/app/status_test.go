package app

import (
	"context"
	"encoding/json"
	"errors"
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

func TestStatusAllLeavesPendingTransfersUnclassified(t *testing.T) {
	e, _, q := fixture(t)
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	source := record(t, e, result.Name)
	destination, err := environment.Identify(t.TempDir(), q.Profile, false)
	if err != nil {
		t.Fatal(err)
	}
	journal := store.Transfer{Version: 1, ID: strings.Repeat("a", 32), Mode: "clone", Phase: "prepare", Source: source.Identity, Destination: destination, SourceID: source.ID, DestinationID: strings.Repeat("b", 32), Started: time.Now().UTC(), Desired: source.Applied}
	if err := journal.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "state/transfers", source.Identity.Name+".json"), string(b))
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	report, err := e.StatusAll(context.Background(), "")
	if err != nil || len(report.Sessions) != 2 {
		t.Fatal("pending endpoints were not retained", report, err)
	}
	for _, view := range report.Sessions {
		if view.Pending == nil || view.Desired != "" || view.ConfigError != "" {
			t.Fatal("pending endpoint was resolved", view)
		}
	}
}

func TestStatusAllClassifiesEachContainerWithoutMutations(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	names := map[string]string{}
	for _, profile := range []string{"clean", "runtime", "container", "image", "invalid", "corrupt", "recordless", "mismatch", "missing"} {
		q.Profile = profile
		write(t, filepath.Join(e.Store.Home, "profiles", profile, "config.json"), `{"version":1,"harness":"pi","on_exit":"running"}`)
		result, err := createAndOpen(ctx, e, q)
		if err != nil {
			t.Fatal(err)
		}
		names[profile] = result.Name
	}
	write(t, filepath.Join(e.Store.Home, "profiles/runtime/entrypoint.sh"), "echo updated")
	write(t, filepath.Join(e.Store.Home, "profiles/container/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	write(t, filepath.Join(e.Store.Home, "profiles/image/Dockerfile"), "FROM debian:bookworm-slim\nRUN echo updated\n")
	write(t, filepath.Join(e.Store.Home, "profiles/invalid/config.json"), "broken")
	corrupt, _ := e.Store.RecordPath(names["corrupt"])
	write(t, corrupt, "broken")
	recordless, _ := e.Store.RecordPath(names["recordless"])
	if err := os.Remove(recordless); err != nil {
		t.Fatal(err)
	}
	mismatch, _ := d.Snapshot(names["mismatch"])
	mismatch.Image = "sha256:" + strings.Repeat("f", 64)
	d.SetContainer(mismatch)
	d.Forget(names["missing"])
	foreign, _ := d.Snapshot(names["clean"])
	foreign.Name = "/foreign"
	foreign.ID = strings.Repeat("f", 64)
	foreign.Config.Labels[docker.Namespace+".installation"] = "foreign"
	d.SetContainer(foreign)

	beforeRecords := map[string]string{}
	for _, name := range names {
		p, _ := e.Store.RecordPath(name)
		if b, err := os.ReadFile(p); err == nil {
			beforeRecords[name] = string(b)
		}
	}
	before := len(d.History())
	report, err := e.StatusAll(ctx, "")
	views := report.Sessions
	if err != nil || len(views) != 8 || len(report.UnmatchedContainers) != 1 || report.UnmatchedContainers[0].Name != names["recordless"] {
		t.Fatal("wrong environment scope or missing unmatched warning", report, err)
	}
	calls := d.History()[before:]
	if len(calls) != 2 || !reflect.DeepEqual(calls[0][:2], []string{"container", "ls"}) || !reflect.DeepEqual(calls[1][:2], []string{"container", "inspect"}) {
		t.Fatal("status did more than batched Docker inventory", calls)
	}
	byName := map[string]View{}
	for i, view := range views {
		if i > 0 && views[i-1].Name >= view.Name {
			t.Fatal("status is not sorted by name")
		}
		if view.Name == names["missing"] {
			if view.Exists || view.Running || view.Desired != environment.NoChange {
				t.Fatal("missing container was treated as a config error", view)
			}
		} else if !view.Exists || !view.Running {
			t.Fatal("desired state hid live state", view)
		}
		byName[view.Name] = view
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
		report, err = e.StatusAll(ctx, profile)
		if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Name != names[profile] {
			t.Fatal("profile filter lost an environment", profile, report, err)
		}
	}
	report, err = e.StatusAll(ctx, "recordless")
	if err != nil || len(report.Sessions) != 0 || len(report.UnmatchedContainers) != 1 {
		t.Fatal("profile filter lost the unmatched container", report, err)
	}
	report, err = e.StatusAll(ctx, "absent")
	if err != nil || report.Sessions == nil || len(report.Sessions) != 0 || report.UnmatchedContainers == nil || len(report.UnmatchedContainers) != 0 {
		t.Fatal("empty selection must retain empty arrays", report, err)
	}
	d.Fail = func([]string) error { return errors.New("Docker unavailable") }
	if _, err = e.StatusAll(ctx, ""); err == nil {
		t.Fatal("unavailable Docker appeared successful")
	}
}
