package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/sshshare"
)

func TestTransfersExcludeSSHRuntimeAndUseDestinationMount(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			result, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(e.Store.Home, "sessions", result.Name, sshshare.RelativeRoot)
			write(t, filepath.Join(source, "config"), "old connection config")
			write(t, filepath.Join(source, "c", "old", "config"), "old socket reference")
			transferred, err := e.Transfer(ctx, TransferOptions{Mode: mode, Source: result.Name, Destination: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(e.Store.Home, "sessions", transferred.Destination, sshshare.RelativeRoot)
			entries, err := os.ReadDir(destination)
			if err != nil || len(entries) != 0 {
				t.Fatal("transferred live SSH runtime", entries, err)
			}
			r := record(t, e, transferred.Destination)
			found := false
			for _, mount := range r.Creation.Mounts {
				if mount.Target == sshshare.Mount {
					found = true
					if mount.Source != destination {
						t.Fatal("destination reused source socket mount")
					}
				}
			}
			if !found {
				t.Fatal("destination lacks own socket mount")
			}
		})
	}
}
