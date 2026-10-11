package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/commanderror"
)

func TestPrecedingTransfersBlockConversionWithoutRewritingIntent(t *testing.T) {
	for _, version := range []string{"3", "4"} {
		t.Run(version, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, "state/transfers")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "old-source.json")
			data := []byte(`{"version":` + version + `,"phase":"prepare","source_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			var failure *commanderror.Error
			if _, err := Pending(context.Background(), home); !errors.As(err, &failure) || failure.Code != "migration_pending_transfer" {
				t.Fatal("preceding transaction was converted or ignored", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
				t.Fatal("recovery intent changed", err)
			}
		})
	}
}

func TestCurrentTransferBlocksIdentityConversionButNotNormalInitialization(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "state/transfers/aaaaaaaaaaaa")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "journal.json"), []byte(`{"version":5}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireResolvedTransfers(home); err == nil {
		t.Fatal("unfinished current transaction did not block conversion")
	}
	if required, err := Pending(context.Background(), home); err != nil || required != nil {
		t.Fatal("current transfer became a format update", required, err)
	}
}
