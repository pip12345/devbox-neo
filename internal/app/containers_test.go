package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestContainerViewsUseBatchedInventoryAndBrokenConfigDoesNotHideState(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := q
	secondRequest.Workspace = t.TempDir()
	second, err := e.Open(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := d.Snapshot(first.Name)
	foreign.Name = "/foreign"
	foreign.ID = strings.Repeat("f", 64)
	foreign.Config.Labels[docker.Namespace+".installation"] = "foreign"
	d.SetContainer(foreign)
	before := len(d.History())
	views, err := e.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || len(d.History())-before != 2 {
		t.Fatal("list did not use one inventory and one batched inspect")
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	view, err := e.Status(ctx, first.Name, "")
	if err != nil || !view.Exists || view.ConfigError == "" {
		t.Fatal("broken desired config hid live status", err)
	}
	if err = e.Logs(ctx, second.Name, "", false, "10"); err != nil {
		t.Fatal(err)
	}
}
func TestContainerDeletionPreservesRecoveryAndPreflightsWholeSet(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	second, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	initial := record(t, e, first.Name)
	marker := filepath.Join(e.Store.Home, "sessions", first.Name, "harnesses/pi/stores/home/marker")
	write(t, marker, "keep")
	lock, err := e.Store.Lock(ctx, second.Name)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("test", "running")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if _, err = e.DeleteContainers(ctx, Selection{All: true}, false); err == nil {
		t.Fatal("active bulk target was deleted")
	}
	if _, exists := d.Snapshot(first.Name); !exists {
		t.Fatal("bulk operation mutated before complete preflight")
	}
	lock, _ = e.Store.Lock(ctx, second.Name)
	lock.Release(lease.ID)
	lock.Close()
	removed, err := e.DeleteContainers(ctx, Selection{Stopped: true}, false)
	if err != nil || len(removed) != 2 {
		t.Fatal(removed, err)
	}
	if string(getFile(t, marker)) != "keep" {
		t.Fatal("container deletion removed state")
	}
	if _, ok := d.Images[initial.ImageTag]; !ok {
		t.Fatal("container deletion removed session image")
	}
	if _, err = e.Start(ctx, first.Name, ""); err != nil {
		t.Fatal("retained record cannot recover", err)
	}
	if record(t, e, first.Name).ID != initial.ID {
		t.Fatal("recovery changed session identity")
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
	e, d, q := fixture(t)
	ctx := context.Background()
	a, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	b, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, a.Name, ""); err != nil {
		t.Fatal(err)
	}
	before := record(t, e, a.Name).SetupContainer
	result, err := e.RecreateAll(ctx, true, "")
	if err != nil || len(result) != 2 {
		t.Fatal(result, err)
	}
	first, _ := d.Snapshot(a.Name)
	second, _ := d.Snapshot(b.Name)
	if first.ID == before || !first.State.Running || second.State.Running {
		t.Fatal("bulk recreation lost running intent")
	}
}
func TestSecondaryNetworksDoNotChangeCreationContract(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := record(t, e, result.Name)
	if err = e.ChangeNetwork(ctx, result.Name, "", "extra", true); err != nil {
		t.Fatal(err)
	}
	before := len(d.History())
	if err = e.ChangeNetwork(ctx, result.Name, "", "extra", true); err != nil {
		t.Fatal(err)
	}
	for _, args := range d.History()[before:] {
		if args[0] == "network" && args[1] == "connect" {
			t.Fatal("duplicate attachment was not a no-op")
		}
	}
	facts, err := e.NetworkFacts(ctx, result.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Primary != "bridge" || facts.Host != docker.HostAlias || len(facts.Networks) != 2 {
		t.Fatal(facts)
	}
	if err = e.ChangeNetwork(ctx, result.Name, "", "bridge", false); err == nil {
		t.Fatal("primary network disconnected")
	}
	if err = e.ChangeNetwork(ctx, result.Name, "", "extra", false); err != nil {
		t.Fatal(err)
	}
	if record(t, e, result.Name).Applied != original.Applied {
		t.Fatal("secondary network changed durable fingerprints")
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	if _, err = e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	if err = e.ChangeNetwork(ctx, result.Name, "", "extra", true); err == nil {
		t.Fatal("host-network container accepted secondary network")
	}
}
func TestExactRootTargetKeepsItsSlotWhenDefaultsChange(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	q.Profile = ""
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	first, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "config.json"), `{"version":1,"default_profile":"test","ignore_project_overrides":true}`)
	q.Workspace = first.Name
	again, err := e.Open(ctx, q)
	if err != nil || again.Name != first.Name {
		t.Fatal("exact target switched slots", err)
	}
}
func TestOwnedContainerWithoutRecordCanBeDeletedButNeverAdopted(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	recordPath, _ := e.Store.RecordPath(result.Name)
	if err = os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Open(ctx, q); err == nil {
		t.Fatal("recordless container was adopted")
	}
	if _, err = e.DeleteContainers(ctx, Selection{Targets: []string{result.Name}}, false); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(result.Name); exists {
		t.Fatal("owned orphan was not deleted")
	}
	if _, err = os.Stat(filepath.Dir(recordPath)); err != nil {
		t.Fatal("container deletion removed retained files", err)
	}
}

func TestExplicitProfileLocateIgnoresUnrelatedCorruptRecords(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	first, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	other := environment.ContainerName("/unrelated", "project")
	write(t, filepath.Join(e.Store.Home, "sessions", other, "session.json"), "broken")
	r, err := e.Locate(ctx, q.Workspace, q.Profile)
	if err != nil || r.Identity.Name != first.Name {
		t.Fatal("unrelated corrupt record blocked explicit selection", err)
	}
	views, err := e.List(ctx, true)
	if err != nil || len(views) != 2 {
		t.Fatal(views, err)
	}
}
