package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/commanderror"
)

func TestRequiredMigrationDetectionAndApplication(t *testing.T) {
	e, d, old, history := oldFixture(t)
	ctx := context.Background()
	before := len(d.History())
	required, err := Pending(ctx, e.Store.Home)
	if err != nil || required == nil || required.Name == "" || required.Warning == "" {
		t.Fatal(required, err)
	}
	if len(d.History()) != before {
		t.Fatal("detection accessed Docker")
	}
	if err := required.Apply(ctx, e.Docker); err != nil {
		t.Fatal(err)
	}
	if required, err := Pending(ctx, e.Store.Home); err != nil || required != nil {
		t.Fatal("completed migration still blocks", required, err)
	}
	if r, err := e.Store.Read(ctx, old.Directory); err != nil || r.ID != old.ID {
		t.Fatal("identity lost", err)
	}
	if data, err := os.ReadFile(history); err != nil || string(data) != "saved history" {
		t.Fatal("history lost", err)
	}
}

func TestMigrationDetectionDoesNotInitializeMissingHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	if req, err := Pending(context.Background(), home); err != nil || req != nil {
		t.Fatal(req, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("detector initialized home")
	}
}

func TestUnsupportedFormatCannotBypassMigrationGate(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions/old/session.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":5}`), 0600); err != nil {
		t.Fatal(err)
	}
	var blocked *commanderror.Error
	if _, err := Pending(context.Background(), home); !errors.As(err, &blocked) || blocked.Code != "migration_unavailable" {
		t.Fatal("unsupported state was permitted", err)
	}
}
