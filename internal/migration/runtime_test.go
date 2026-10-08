package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func oldFixture(t *testing.T) (*app.Engine, *dockertest.Daemon, store.Record, string) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &dockertest.Daemon{}
	e := &app.Engine{Store: s, Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000}
	root, workspace := filepath.Join(s.Home, "configs/base"), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docker"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"version":1,"harness":"pi","env":["TOKEN=private-value"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docker/Dockerfile"), []byte("FROM ${DEVBOX_BASE}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	q := app.CreateRequest{Workspace: workspace, LocalName: "main", Sources: []config.Reference{{Label: "base", Kind: config.ReferenceFixed, Path: root}}}
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Find(ctx, made.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(s.Home, "sessions", r.Directory, "harnesses/pi/stores/home/history")
	if err := os.WriteFile(history, []byte("saved history"), 0600); err != nil {
		t.Fatal(err)
	}
	spec, err := e.Resolve(app.ResolveRequest{Workspace: q.Workspace, LocalName: q.LocalName, Sources: q.Sources, Host: q.Host})
	if err != nil {
		t.Fatal(err)
	}
	var contexts []map[string]environment.FileInput
	for _, stage := range spec.Build.Stages {
		files := map[string]environment.FileInput{}
		for name, file := range stage.Context {
			files[name] = environment.FileInput{FileState: environment.FileState{Hash: environment.Fingerprint(s.Installation, file.Data), Mode: uint32(file.Mode), Directory: file.Directory}, Source: filepath.Join(root, "docker", name)}
		}
		contexts = append(contexts, files)
	}
	files := map[string]environment.FileInput{}
	for name, file := range spec.Files {
		files[name] = environment.FileInput{FileState: environment.FileState{Hash: environment.Fingerprint(s.Installation, file.Data), Mode: uint32(file.Mode & 0111)}, Source: file.Source}
	}
	oldTag := oldNamespace + "/session:" + r.ID
	image := d.Images[r.Applied.ImageID]
	image.Config.Labels = map[string]string{oldNamespace + ".managed": "true", oldNamespace + ".ownership": "1", oldNamespace + ".installation": s.Installation}
	delete(d.Images, r.Applied.ImageTag)
	d.Images[r.Applied.ImageID], d.Images[oldTag] = image, image
	c, _ := d.Snapshot(r.Applied.Creation.Name)
	c.Config.Labels = map[string]string{oldNamespace + ".managed": "true", oldNamespace + ".ownership": "1", oldNamespace + ".installation": s.Installation, oldNamespace + ".session": r.ID}
	d.SetContainer(c)
	r.Version, r.Applied.ImageTag = 6, oldTag
	r.Applied.Fingerprints = legacyFingerprints(r, contexts, files)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	inputs := raw["applied"].(map[string]any)["inputs"].(map[string]any)
	stages := inputs["image"].(map[string]any)["stages"].([]any)
	for i, context := range contexts {
		stage := stages[i].(map[string]any)
		delete(stage, "context_hash")
		stage["context"] = context
	}
	runtime := inputs["runtime"].(map[string]any)
	delete(runtime, "files_hash")
	runtime["files"] = files
	applied := raw["applied"].(map[string]any)
	applied["setup"] = spec.Setup
	applied["env_sources"] = []any{map[string]any{"kind": "file", "path": filepath.Join(root, "config.json"), "field": "env", "index": 0, "raw_hash": strings.Repeat("a", 64), "value_hash": strings.Repeat("b", 64)}}
	data, err = json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Home, "sessions", r.Directory, "session.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return e, d, r, history
}

func TestRuntimeCutoverPreservesStateAndRebuildsNormally(t *testing.T) {
	e, d, old, history := oldFixture(t)
	ctx := context.Background()
	path := filepath.Join(e.Store.Home, "sessions", old.Directory, "session.json")
	before, _ := os.ReadFile(path)
	if _, err := e.Store.Read(ctx, old.Directory); err == nil {
		t.Fatal("ordinary reader accepted old format")
	}
	preview, err := Run(ctx, e.Store.Home, e.Docker, false)
	if err != nil || len(preview.Sessions) != 1 || preview.Sessions[0].ImageTag != old.Applied.ImageTag {
		t.Fatal(preview, err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("preview rewrote state")
	}
	if _, ok := d.Snapshot(old.Applied.Creation.Name); !ok {
		t.Fatal("preview removed runtime")
	}
	if _, err := Run(ctx, e.Store.Home, e.Docker, true); err != nil {
		t.Fatal(err)
	}
	current, err := e.Store.Read(ctx, old.Directory)
	if err != nil || current.ID != old.ID || current.Directory != old.Directory {
		t.Fatal(current, err)
	}
	data, _ := os.ReadFile(path)
	for _, forbidden := range []string{"env_sources", `"files":`, `"context":`, "private-value", oldNamespace} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatal("old inventory/recovery data retained", forbidden)
		}
	}
	if selected, err := e.Store.ReadDefault(ctx, old.Settings.Workspace); err != nil || selected == nil || selected.ID != old.ID {
		t.Fatal("default lost", err)
	}
	if after, _ := os.ReadFile(history); string(after) != "saved history" {
		t.Fatal("history lost")
	}
	if _, exists := d.Snapshot(old.Applied.Creation.Name); exists {
		t.Fatal("old container retained")
	}
	if _, ok := d.Images[old.Applied.ImageTag]; ok {
		t.Fatal("old tag retained")
	}
	// Untagging can retain a dangling image. Explicit recreation builds a new
	// owned image from current sources rather than adopting the old image.
	applied, err := e.RecreateAll(ctx, false, app.RecreateOptions{})
	if err != nil || len(applied) != 1 || applied[0] != old.ID {
		t.Fatal("bulk recreation skipped the migrated session", applied, err)
	}
	if image := d.Images[old.Applied.ImageID]; image.Config.Labels[docker.Namespace+".managed"] != "" {
		t.Fatal("old image relabeled/adopted")
	}
	if _, err := Run(ctx, e.Store.Home, e.Docker, true); err != nil {
		t.Fatal("completed retry failed", err)
	}
}

func TestRuntimeCutoverRetriesPartialCleanup(t *testing.T) {
	e, d, old, _ := oldFixture(t)
	d.Fail = func(args []string) error {
		if args[0] == "image" && args[1] == "rm" {
			return errors.New("cleanup unavailable")
		}
		return nil
	}
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err == nil {
		t.Fatal("cleanup failure ignored")
	}
	if _, exists := d.Snapshot(old.Applied.Creation.Name); exists {
		t.Fatal("container removal did not precede failed tag cleanup")
	}
	d.Fail = nil
	if _, err := Run(context.Background(), e.Store.Home, e.Docker, true); err != nil {
		t.Fatal("partial retry failed", err)
	}
}

func TestRuntimeCutoverRefusesForeignOwnershipAndActiveWork(t *testing.T) {
	for _, mode := range []string{"foreign container", "foreign image", "active", "corrupt fingerprint", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			e, d, old, _ := oldFixture(t)
			ctx := context.Background()
			switch mode {
			case "foreign container":
				c, _ := d.Snapshot(old.Applied.Creation.Name)
				c.Config.Labels[oldNamespace+".installation"] = "foreign"
				d.SetContainer(c)
			case "foreign image":
				i := d.Images[old.Applied.ImageTag]
				i.Config.Labels[oldNamespace+".installation"] = "foreign"
				d.Images[old.Applied.ImageTag] = i
			case "active":
				lock, err := e.Store.Lock(ctx, old.Directory, old.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := lock.Lease("exec"); err != nil {
					t.Fatal(err)
				}
				lock.Close()
			case "corrupt fingerprint":
				p := filepath.Join(e.Store.Home, "sessions", old.Directory, "session.json")
				data, _ := os.ReadFile(p)
				data = bytes.Replace(data, []byte(old.Applied.Fingerprints.Image), []byte(strings.Repeat("a", 64)), 1)
				if err := os.WriteFile(p, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if _, err := Run(ctx, e.Store.Home, e.Docker, true); err == nil {
				t.Fatal("unsafe cutover accepted")
			}
			if _, exists := d.Snapshot(old.Applied.Creation.Name); !exists {
				t.Fatal("failed preflight removed runtime")
			}
		})
	}
}

func TestCutoverPreservesIncompleteAllocationsButRejectsCorruptRecords(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing record", true: "corrupt record"}[corrupt], func(t *testing.T) {
			e, d, old, history := oldFixture(t)
			root := filepath.Join(e.Store.Home, "sessions", "dbx-incomplete")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, "partial-data")
			if err := os.WriteFile(marker, []byte("keep for explicit cleanup"), 0600); err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if err := os.WriteFile(filepath.Join(root, "session.json"), []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Run(context.Background(), e.Store.Home, e.Docker, true)
			if corrupt {
				if err == nil {
					t.Fatal("corrupt record ignored")
				}
				if _, exists := d.Snapshot(old.Applied.Creation.Name); !exists {
					t.Fatal("failed preflight removed runtime")
				}
			} else {
				if err != nil {
					t.Fatal("incomplete allocation blocked migration", err)
				}
				if _, err := e.Store.Read(context.Background(), old.Directory); err != nil {
					t.Fatal("saved session not converted", err)
				}
				if _, err := os.Stat(filepath.Join(root, "session.json")); !os.IsNotExist(err) {
					t.Fatal("incomplete allocation adopted", err)
				}
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "keep for explicit cleanup" {
				t.Fatal("incomplete allocation modified", err)
			}
			if data, err := os.ReadFile(history); err != nil || string(data) != "saved history" {
				t.Fatal("history modified", err)
			}
		})
	}
}

func TestCutoverRequiresExistingHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	if _, err := Run(context.Background(), home, docker.Runtime{}, true); err == nil {
		t.Fatal("absent home accepted")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("cutover initialized a home")
	}
}
