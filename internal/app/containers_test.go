package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestContainerViewsUseBatchedInventoryAndBrokenConfigDoesNotHideState(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := q
	secondRequest.Workspace = t.TempDir()
	second, err := e.Create(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	owned, _ := sessionSnapshot(t, e, first.SessionID)
	owned.Created = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	d.SetContainer(owned)
	foreign, _ := sessionSnapshot(t, e, first.SessionID)
	foreign.Name = "/foreign"
	foreign.ID = strings.Repeat("f", 64)
	foreign.Config.Labels[docker.Namespace+".installation"] = "foreign"
	d.SetContainer(foreign)
	before := len(d.History())
	report, err := e.List(ctx, "")
	views := report.Sessions
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || len(d.History())-before != 2 {
		t.Fatal("list did not use one inventory and one batched inspect")
	}
	for _, view := range views {
		if view.Target == first.SessionID && (!view.CreatedAt.Equal(owned.Created) || view.LastActivity.IsZero() || view.LastAction == "" || view.LocalName != q.LocalName || view.Harness == "") {
			t.Fatal("list lost live creation time or recorded details", view)
		}
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	view, err := e.Status(ctx, first.SessionID, "")
	if err != nil || !view.Exists || view.ConfigError == "" {
		t.Fatal("broken desired config hid live status", err)
	}
	if err = e.Logs(ctx, second.SessionID, "", false, "10"); err != nil {
		t.Fatal(err)
	}
}
func TestContainerDeletionPreservesRecoveryAndPreflightsWholeSet(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	initial := sessionRecord(t, e, first.SessionID)
	marker := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, first.SessionID).Directory, "harnesses/pi/stores/home/marker")
	write(t, marker, "keep")
	lock, err := e.Store.Lock(ctx, sessionRecord(t, e, second.SessionID).Directory, sessionRecord(t, e, second.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("test")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if _, err = e.DeleteContainers(ctx, Selection{All: true}, false); err == nil {
		t.Fatal("active bulk target was deleted")
	}
	if _, exists := sessionSnapshot(t, e, first.SessionID); !exists {
		t.Fatal("bulk operation mutated before complete preflight")
	}
	lock, _ = e.Store.Lock(ctx, sessionRecord(t, e, second.SessionID).Directory, sessionRecord(t, e, second.SessionID).ID)
	lock.Release(lease.ID)
	lock.Close()
	removed, err := e.DeleteContainers(ctx, Selection{Stopped: true}, false)
	if err != nil || len(removed) != 2 {
		t.Fatal(removed, err)
	}
	if string(getFile(t, marker)) != "keep" {
		t.Fatal("container deletion removed state")
	}
	if _, ok := d.Images[initial.Applied.ImageTag]; !ok {
		t.Fatal("container deletion removed session image")
	}
	if _, err = e.Recreate(ctx, RecreateRequest{Target: first.SessionID}, false); err != nil {
		t.Fatal("retained record cannot recreate", err)
	}
	if sessionRecord(t, e, first.SessionID).ID != initial.ID {
		t.Fatal("recreation changed session identity")
	}
}
func getFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestRecreateAllPreflightsAndPreservesRunningIntent(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	a, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	b, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, a.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, a.SessionID).Applied.SetupContainer
	result, err := e.RecreateAll(ctx, true, RecreateOptions{})
	if err != nil || len(result) != 2 {
		t.Fatal(result, err)
	}
	first, _ := sessionSnapshot(t, e, a.SessionID)
	second, _ := sessionSnapshot(t, e, b.SessionID)
	if first.ID == before || !first.State.Running || second.State.Running {
		t.Fatal("bulk recreation lost running intent")
	}
}
func TestSecondaryNetworksDoNotChangeCreationContract(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := sessionRecord(t, e, result.SessionID)
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "extra", true); err != nil {
		t.Fatal(err)
	}
	before := len(d.History())
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "extra", true); err != nil {
		t.Fatal(err)
	}
	for _, args := range d.History()[before:] {
		if args[0] == "network" && args[1] == "connect" {
			t.Fatal("duplicate attachment was not a no-op")
		}
	}
	facts, err := e.NetworkFacts(ctx, result.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Primary != "bridge" || facts.Host != docker.HostAlias || len(facts.Networks) != 2 {
		t.Fatal(facts)
	}
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "bridge", false); err == nil {
		t.Fatal("primary network disconnected")
	}
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "extra", false); err != nil {
		t.Fatal(err)
	}
	if sessionRecord(t, e, result.SessionID).Applied.Fingerprints != original.Applied.Fingerprints {
		t.Fatal("secondary network changed durable fingerprints")
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	if _, err = e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "extra", true); err == nil {
		t.Fatal("host-network container accepted secondary network")
	}
}
func TestExactTargetIgnoresObsoleteGlobalConfiguration(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	q.Sources = q.Sources[1:]
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"version":1,"default_profile":"test","ignore_project":true}`)
	q.Workspace = first.SessionID
	again, err := e.Open(ctx, openRequest(q))
	if err != nil || again.SessionID != first.SessionID {
		t.Fatal("exact target switched slots", err)
	}
}
func TestOwnedContainerWithoutRecordCanBeDeletedButNeverAdopted(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	runtimeName := sessionRecord(t, e, result.SessionID).Applied.Creation.Name
	recordPath, _ := e.Store.RecordPath(sessionRecord(t, e, result.SessionID).Directory)
	if err = os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Open(ctx, openRequest(q)); err == nil {
		t.Fatal("recordless container was adopted")
	}
	if _, err = e.DeleteContainers(ctx, Selection{Targets: []string{result.SessionID}}, false); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(runtimeName); exists {
		t.Fatal("owned orphan was not deleted")
	}
	if _, err = os.Stat(filepath.Dir(recordPath)); err != nil {
		t.Fatal("container deletion removed retained files", err)
	}
}

func TestExplicitProfileLocateIgnoresUnrelatedCorruptRecords(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	other := environment.ResourceName("/unrelated", "project", "broken")
	write(t, filepath.Join(e.Store.Home, "sessions", other, "session.json"), "broken")
	r, err := e.Locate(ctx, q.Workspace, q.LocalName)
	if err != nil || r.ID != first.SessionID {
		t.Fatal("unrelated corrupt record blocked explicit selection", err)
	}
	report, err := e.List(ctx, "")
	views := report.Sessions
	if err != nil || len(views) != 2 {
		t.Fatal(views, err)
	}
}
