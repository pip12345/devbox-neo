package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func precedingTransfer(t *testing.T, committed bool) (*app.Engine, *dockertest.Daemon, app.TransferOptions, store.Transfer, string) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &dockertest.Daemon{}
	e := &app.Engine{Store: s, Docker: docker.Runtime{Runner: d}, UID: 1000, GID: 1000}
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"harness":"pi"}`), 0600); err != nil {
		t.Fatal(err)
	}
	q := app.Request{Workspace: t.TempDir(), LocalName: "main", Sources: []config.Reference{{Label: "base", Kind: config.ReferenceFixed, Path: configDir}}}
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Locate(ctx, made.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := app.TransferOptions{Mode: "clone", Source: r.ID, Destination: t.TempDir()}
	if committed {
		opts.Mode = "relocate"
	}
	d.Fail = func(args []string) error {
		if (!committed && args[0] == "build") || (committed && args[0] == "rm" && args[len(args)-1] == r.Applied.SetupContainer) {
			return errors.New("interrupted transfer")
		}
		return nil
	}
	if _, err := e.Transfer(ctx, opts); err == nil {
		t.Fatal("transfer unexpectedly completed")
	}
	d.Fail = nil
	j, err := s.ReadTransfer(r.Directory)
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	old := struct {
		store.Transfer
		Desired environment.Fingerprints `json:"desired"`
	}{*j, r.Applied.Fingerprints}
	old.Version = 3
	if err := fsutil.JSON(filepath.Join(s.Home, "state/transfers", r.Directory+".json"), old); err != nil {
		t.Fatal(err)
	}
	return e, d, opts, *j, configDir
}

func TestTransferMigrationPreservesCommitmentAndDoesNotTouchRuntime(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare", true: "committed"}[committed], func(t *testing.T) {
			e, d, opts, original, configDir := precedingTransfer(t, committed)
			ctx := context.Background()
			path := filepath.Join(e.Store.Home, "state/transfers", original.Source.Name+".json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Store.ReadTransfer(original.Source.Name); err == nil {
				t.Fatal("ordinary reader accepted preceding format")
			}
			required, err := Pending(ctx, e.Store.Home)
			if err != nil || required == nil {
				t.Fatal(required, err)
			}
			unchanged, _ := os.ReadFile(path)
			if string(unchanged) != string(before) {
				t.Fatal("detection changed journal")
			}
			calls := len(d.History())
			if err := required.Apply(ctx, e.Docker); err != nil {
				t.Fatal(err)
			}
			if len(d.History()) != calls {
				t.Fatal("migration touched Docker")
			}
			converted, err := e.Store.ReadTransfer(original.Source.Name)
			if err != nil || converted == nil || *converted != original {
				t.Fatal("migration changed transfer intent", converted, err)
			}
			if required, err := Pending(ctx, e.Store.Home); err != nil || required != nil {
				t.Fatal("completed conversion still pending", required, err)
			}
			body := `{"harness":"pi","network":"host"}`
			if committed {
				body = "broken configuration must not block committed cleanup"
			}
			if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Transfer(ctx, opts); err != nil {
				t.Fatal("converted transfer cannot recover", err)
			}
		})
	}
}

func TestTransferMigrationSortsCompleteEndpointLockSet(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(s.Home, "state/transfers")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	// Source-sorted journals have interleaving destinations. Concatenating each
	// endpoint pair cannot produce sorted directories, regardless of map order.
	var originals []store.Transfer
	for i, names := range [][2]string{{"b-source", "z-destination"}, {"c-source", "a-destination"}} {
		source, err := environment.Identify(t.TempDir(), "main")
		if err != nil {
			t.Fatal(err)
		}
		destination, err := environment.Identify(t.TempDir(), "main")
		if err != nil {
			t.Fatal(err)
		}
		source.Name, destination.Name = names[0], names[1]
		j := store.Transfer{
			Version: 4, ID: fmt.Sprintf("%032x", i+1), Mode: "clone", Phase: "prepare",
			Source: source, Destination: destination,
			SourceID: fmt.Sprintf("%032x", i+10), DestinationID: fmt.Sprintf("%032x", i+20),
			ContainerName: "dbx-project-111111111111.main", SourceContainerID: strings.Repeat("a", 64),
			Started: time.Now().UTC(),
		}
		if i == 1 {
			j.Mode, j.Phase = "relocate", "committed"
			j.DestinationID = j.SourceID
			j.Running, j.ManualStart = true, true
		}
		old := struct {
			store.Transfer
			Desired environment.Fingerprints `json:"desired"`
		}{j, environment.Fingerprints{Image: strings.Repeat("a", 64), Container: strings.Repeat("b", 64), Runtime: strings.Repeat("c", 64)}}
		old.Version = 3
		if err := fsutil.JSON(filepath.Join(root, source.Name+".json"), old); err != nil {
			t.Fatal(err)
		}
		originals = append(originals, j)
	}
	if err := migrateTransfers(ctx, s.Home); err != nil {
		t.Fatal(err)
	}
	for _, original := range originals {
		converted, err := s.ReadTransfer(original.Source.Name)
		if err != nil || converted == nil || *converted != original {
			t.Fatal("migration changed transfer endpoints or commitment", converted, err)
		}
	}
}

func TestTransferMigrationPreflightsCorruptionAndLiveLeases(t *testing.T) {
	for _, invalid := range []string{"journal", "lease"} {
		t.Run(invalid, func(t *testing.T) {
			e, _, _, original, _ := precedingTransfer(t, false)
			ctx := context.Background()
			path := filepath.Join(e.Store.Home, "state/transfers", original.Source.Name+".json")
			if invalid == "journal" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(path), "broken.json"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				lock, err := e.Store.Lock(ctx, original.Source.Name, original.SourceID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := lock.Lease("test"); err != nil {
					t.Fatal(err)
				}
				lock.Close()
			}
			if err := migrateTransfers(ctx, e.Store.Home); err == nil {
				t.Fatal("unsafe migration accepted")
			} else if invalid == "journal" && !strings.Contains(err.Error(), "journal") {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var header struct {
				Version int `json:"version"`
			}
			if err := json.Unmarshal(data, &header); err != nil || header.Version != 3 {
				t.Fatal("preflight published a partial conversion", err)
			}
		})
	}
}
