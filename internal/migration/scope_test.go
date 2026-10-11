package migration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/fsutil"
)

func TestCutoverLeavesUnlinkedAndOtherInstallationResources(t *testing.T) {
	e, d, old, _ := oldFixture(t)
	c, _ := d.Snapshot(old.Applied.Creation.Name)
	c.ID = strings.Repeat("f", 64)
	c.Name = "/unrelated"
	c.Mounts = nil
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
	path := filepath.Join(e.Store.Home, "state/transfers", old.Directory+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.JSON(path, map[string]any{"version": 3, "phase": "prepare", "source_id": old.ID}); err != nil {
		t.Fatal(err)
	}
	if required, err := Pending(context.Background(), e.Store.Home); err == nil || required != nil {
		t.Fatal("pending transfer was offered an identity conversion", required, err)
	}
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err == nil {
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
		t.Fatal("blocked conversion changed previous recovery intent", err)
	}
	if _, exists := d.Snapshot(old.Applied.Creation.Name); !exists {
		t.Fatal("pending conversion mutated Docker")
	}
}
