package migration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func precedingSessionFixture(t *testing.T) (*app.Engine, *dockertest.Daemon, store.Record) {
	t.Helper()
	e, d, old, _ := oldFixture(t)
	item, _, err := readConversionCandidate(e.Store.Home, old.Directory)
	if err != nil {
		t.Fatal(err)
	}
	r := item.record
	r.Version, r.ID, r.Directory, r.StateTree = 7, old.ID, old.Directory, ""
	r.Applied.Creation = old.Applied.Creation
	r.Applied.ImageTag = docker.Namespace + "/session:legacy-main-" + old.ID
	image := d.Images[old.Applied.ImageID]
	image.Config.Labels = docker.ImageLabels(e.Store.Installation)
	delete(d.Images, old.Applied.ImageTag)
	d.Images[r.Applied.ImageID], d.Images[r.Applied.ImageTag] = image, image
	c, _ := d.Snapshot(old.Applied.Creation.Name)
	c.Config.Labels = map[string]string{docker.Namespace + ".managed": "true", docker.Namespace + ".ownership": "1", docker.Namespace + ".installation": e.Store.Installation, docker.Namespace + ".session": old.ID}
	d.SetContainer(c)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "state_tree")
	applied := raw["applied"].(map[string]any)
	applied["setup_container"] = applied["container_id"]
	delete(applied, "container_id")
	path := filepath.Join(e.Store.Home, "sessions", r.Directory, "session.json")
	if err := fsutil.JSON(path, raw); err != nil {
		t.Fatal(err)
	}
	return e, d, r
}

func TestSessionModelConversionPreservesBaselinesAndOwnedImages(t *testing.T) {
	e, d, old := precedingSessionFixture(t)
	ctx := context.Background()
	if _, err := Run(ctx, e.Store.Home, e.Docker, true); err != nil {
		t.Fatal(err)
	}
	r, err := e.Store.Read(ctx, old.ID[:12])
	if err != nil || r.ID != old.ID[:12] || r.StateTree != store.InitialStateTree || r.Applied.Fingerprints != old.Applied.Fingerprints {
		t.Fatal("conversion changed the applied explanation baseline", r, err)
	}
	if data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", r.ID, r.StateTree, "harnesses/pi/stores/home/history")); err != nil || string(data) != "saved history" {
		t.Fatal("history not relocated", err)
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", old.Directory)); !os.IsNotExist(err) {
		t.Fatal("old root retained", err)
	}
	if _, exists := d.Snapshot(old.Applied.Creation.Name); exists {
		t.Fatal("old bind-backed runtime retained")
	}
	if image, exists := d.Images[r.Applied.ImageTag]; !exists || image.ID != old.Applied.ImageID {
		t.Fatal("owned image was unnecessarily lost")
	}
	selected, err := e.Store.ReadDefault(ctx, old.Settings.Workspace)
	if err != nil || selected == nil || selected.ID != r.ID {
		t.Fatal("default association not converted", selected, err)
	}
	if _, err := e.Recreate(ctx, app.RecreateRequest{Target: r.PublicName()}, false); err != nil {
		t.Fatal("converted state could not be recreated", err)
	}
}

func TestConversionDoesNotOverwriteAnExistingDestination(t *testing.T) {
	e, d, old := precedingSessionFixture(t)
	root := filepath.Join(e.Store.Home, "sessions", old.ID[:12])
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	before := len(d.History())
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err == nil {
		t.Fatal("occupied destination accepted")
	}
	if len(d.History()) != before {
		t.Fatal("filesystem preflight touched runtime")
	}
}

func TestConversionResumesAfterDirectoryAndTreeMoves(t *testing.T) {
	e, _, old := precedingSessionFixture(t)
	root := filepath.Join(e.Store.Home, "sessions", old.Directory)
	if err := os.Rename(filepath.Join(root, "harnesses"), filepath.Join(root, store.InitialStateTree, "harnesses")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, filepath.Join(e.Store.Home, "sessions", old.ID[:12])); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err != nil {
		t.Fatal("partial relocation could not resume", err)
	}
	if r, err := e.Store.Read(context.Background(), old.ID[:12]); err != nil || r.StateTree != store.InitialStateTree {
		t.Fatal(r, err)
	}
}
