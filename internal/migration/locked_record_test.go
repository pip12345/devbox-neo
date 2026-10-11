package migration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"devbox/internal/config"
	"devbox/internal/fsutil"
)

func TestConversionUsesRecordSavedWhileWaitingForItsLock(t *testing.T) {
	e, _, old := precedingSessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lockRoot := filepath.Join(e.Store.Home, "state/locks/sessions")
	// The fixture was built using current code before being converted into a
	// preceding record. Remove its unused new-ID lock to observe discovery.
	newLock := filepath.Join(lockRoot, old.ID[:12]+".lock")
	if err := os.Remove(newLock); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	held, err := fsutil.Lock(ctx, filepath.Join(lockRoot, old.ID+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, e.Store.Home, e.Docker, true)
		done <- err
	}()
	defer func() {
		if held != nil {
			fsutil.Unlock(held)
		}
		cancel()
	}()
	// New-ID lock creation occurs after the unlocked record discovery. The
	// old-ID lock held above keeps the converter from its locked reread.
	for {
		if _, err := os.Stat(newLock); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			t.Fatalf("conversion did not wait for the old owner: %v", err)
		case <-ctx.Done():
			t.Fatal("conversion never completed discovery")
		case <-time.After(time.Millisecond):
		}
	}
	path := filepath.Join(e.Store.Home, "sessions", old.Directory, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	latest := []config.Reference{{Label: "latest", Kind: config.ReferenceFixed, Path: filepath.Join(e.Store.Home, "configs/latest")}}
	settings := raw["settings"].(map[string]any)
	settings["sources"], settings["manual_start"] = latest, true
	if err := fsutil.JSON(path, raw); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.Unlock(held); err != nil {
		t.Fatal(err)
	}
	held = nil
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := e.Store.Read(context.Background(), old.ID[:12])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Settings.Sources, latest) || !current.Settings.ManualStart {
		t.Fatal("conversion overwrote the completed save", current.Settings)
	}
	if current.Applied.Fingerprints != old.Applied.Fingerprints {
		t.Fatal("conversion advanced the applied baseline")
	}
}
