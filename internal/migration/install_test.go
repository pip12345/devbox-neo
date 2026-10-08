package migration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/app"
	"devbox/internal/environment"
	"devbox/internal/harness"
)

func TestMigratedSessionDetectsHarnessInstallerChanges(t *testing.T) {
	e, d, old, _ := oldFixture(t)
	ctx := context.Background()
	h, err := harness.Load(e.Store.Home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	h.Definition.Install.Shell, h.Definition.Install.Script = "", "install.sh"
	definition, err := json.Marshal(h.Definition)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(e.Store.Home, "harnesses", "pi")
	if err := os.MkdirAll(filepath.Join(root, "install"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "harness.json"), definition, 0600); err != nil {
		t.Fatal(err)
	}
	installer := filepath.Join(root, "install", "install.sh")
	if err := os.WriteFile(installer, []byte("true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, e.Store.Home, e.Docker, true); err != nil {
		t.Fatal(err)
	}
	converted, err := e.Store.Read(ctx, old.Directory)
	if err != nil || converted.Applied.Inputs.Image.Definition.Hash != old.Applied.Inputs.Image.Definition.Hash {
		t.Fatal("migration replaced the applied definition with current inputs", err)
	}
	if _, err := e.Recreate(ctx, app.RecreateRequest{Target: old.Directory}, false); err != nil {
		t.Fatal(err)
	}
	baseline, err := e.Store.Read(ctx, old.Directory)
	if err != nil {
		t.Fatal(err)
	}
	status, err := e.Status(ctx, old.Directory, "")
	if err != nil || status.ConfigError != "" || status.Desired != environment.NoChange {
		t.Fatal("recreated baseline is not current", status.View, err)
	}
	if err := os.WriteFile(installer, []byte("true\n# changed installer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err = e.Status(ctx, old.Directory, "")
	if err != nil || status.ConfigError != "" || status.Desired != environment.RebuildAndRecreate {
		t.Fatal("installer edit did not require image rebuilding", status.View, err)
	}
	found := false
	for _, change := range status.PendingInputChanges {
		if change.Scope == environment.ImageScope && change.Field == "harness_definition" && change.Code == "file_content_changed" {
			found = true
		}
	}
	if !found {
		t.Fatal("installer change has no image diagnostic", status.PendingInputChanges)
	}
	if status.Record.Applied.Fingerprints != baseline.Applied.Fingerprints {
		t.Fatal("status advanced the applied baseline")
	}
	builds := func() int {
		n := 0
		for _, call := range d.Calls {
			if len(call) > 0 && call[0] == "build" {
				n++
			}
		}
		return n
	}
	before := builds()
	if _, err := e.Recreate(ctx, app.RecreateRequest{Target: old.Directory}, false); err != nil {
		t.Fatal(err)
	}
	if builds() <= before {
		t.Fatal("installer edit reused the previous image")
	}
	status, err = e.Status(ctx, old.Directory, "")
	if err != nil || status.ConfigError != "" || status.Desired != environment.NoChange {
		t.Fatal("installer recreation did not commit current inputs", status.View, err)
	}
	if status.Record.Applied.Inputs.Image.Definition.Hash == baseline.Applied.Inputs.Image.Definition.Hash {
		t.Fatal("installer recreation retained the old definition digest")
	}
}
