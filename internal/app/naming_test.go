package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionNamesAreIndependentOfDevelopmentOwnershipNamespace(t *testing.T) {
	e, d, q := fixture(t)
	opened, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(opened.Name, "devbox-") || strings.HasPrefix(opened.Name, "devbox-rewrite-") {
		t.Fatal("unexpected container/session prefix", opened.Name)
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", opened.Name, "session.json")); err != nil {
		t.Fatal("session directory does not use the container name", err)
	}
	container, ok := d.Snapshot(opened.Name)
	if !ok || container.Config.Labels["devbox-rewrite.managed"] != "true" || container.Config.Labels["devbox.managed"] != "" {
		t.Fatal("renaming changed the development ownership namespace")
	}
	r := record(t, e, opened.Name)
	if r.ImageTag != "devbox-rewrite/session:"+r.ID {
		t.Fatal("renaming changed the image namespace", r.ImageTag)
	}
	sum := sha256.Sum256([]byte(r.Identity.Workspace + "\x00" + r.Identity.LocalName))
	for _, prefix := range []string{"devbox-rewrite-", "devbox-"} {
		oldName := prefix + hex.EncodeToString(sum[:12]) + "." + strings.ReplaceAll(r.Identity.LocalName, ":", "-")
		r.Identity.Name, r.Creation.Name = oldName, oldName
		if err := r.Validate(oldName); err == nil {
			t.Fatal("old naming accepted without the required clean reset")
		}
	}
}
