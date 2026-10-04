package migration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func TestCutoverLeavesUnlinkedAndOtherInstallationResources(t *testing.T) {
	e, d, old, _ := oldFixture(t)
	c, _ := d.Snapshot(old.Applied.Creation.Name)
	c.ID = strings.Repeat("f", 64)
	c.Name = "/unrelated"
	c.Config.Labels = map[string]string{oldNamespace + ".managed": "true", oldNamespace + ".ownership": "1", oldNamespace + ".installation": "another-installation", oldNamespace + ".session": strings.Repeat("f", 32)}
	d.SetContainer(c)
	unlinkedTag := oldNamespace + "/build:unlinked"
	d.Images[unlinkedTag] = d.Images[old.Applied.ImageID]
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot("unrelated"); !exists {
		t.Fatal("other installation container removed")
	}
	if _, exists := d.Images[unlinkedTag]; !exists {
		t.Fatal("unlinked tag removed by broad cleanup")
	}
}

func TestCutoverRefusesPendingTransfersBeforeMutation(t *testing.T) {
	e, d, old, _ := oldFixture(t)
	id, err := fsutil.ID()
	if err != nil {
		t.Fatal(err)
	}
	destination := environment.Identity{Binding: environment.Binding{Workspace: t.TempDir(), LocalName: "copy"}}
	destination.Name = environment.ResourceName(destination.Workspace, destination.LocalName, id)
	journal := store.Transfer{Version: 4, ID: id, Mode: "clone", Phase: "prepare", Source: environment.Identity{Binding: old.Settings.Binding, Name: old.Directory}, Destination: destination, SourceID: old.ID, DestinationID: id, SourceContainerID: old.Applied.SetupContainer, ContainerName: environment.ResourceName(destination.Workspace, destination.LocalName, "container"), Started: old.Created}
	if err := journal.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.Store.Home, "state/transfers"), 0700); err != nil {
		t.Fatal(err)
	}
	preceding := struct {
		store.Transfer
		Desired environment.Fingerprints `json:"desired"`
	}{journal, old.Applied.Fingerprints}
	preceding.Version = 3
	path := filepath.Join(e.Store.Home, "state/transfers", old.Directory+".json")
	if err := fsutil.JSON(path, preceding); err != nil {
		t.Fatal(err)
	}
	required, err := Pending(context.Background(), e.Store.Home)
	if err != nil || required == nil {
		t.Fatal(required, err)
	}
	if err := required.Apply(context.Background(), e.Docker); err == nil {
		t.Fatal("pending transfer accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil || header.Version != 3 {
		t.Fatal("blocked cutover changed the journal needed by the previous build", err)
	}
	if _, exists := d.Snapshot(old.Applied.Creation.Name); !exists {
		t.Fatal("pending cutover mutated Docker")
	}
}
