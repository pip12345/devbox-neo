package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestSessionNameAndDirectoryShareOneCompleteID(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	if len(r.ID) != 12 || r.Directory != r.ID || !environment.IsSessionID(r.ID) || made.Session != r.PublicName() {
		t.Fatal("session identity was split", r.ID, r.Directory, made.Session)
	}
	if !strings.Contains(r.PublicName(), "-"+r.ID+"."+q.LocalName) {
		t.Fatal("public name cannot locate directory", r.PublicName())
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", r.ID, "session.json")); err != nil {
		t.Fatal(err)
	}
	c, ok := d.Snapshot(r.Applied.Creation.Name)
	if !ok || c.Config.Labels[docker.Namespace+".session"] != r.ID || r.Applied.ImageTag != environment.ImageTag(r.Applied.Creation.Name) {
		t.Fatal("runtime ownership/alias differs from record")
	}
	before := r.Applied
	changed, err := e.Rename(ctx, r.PublicName(), "", "work", false)
	if err != nil {
		t.Fatal(err)
	}
	after := sessionRecord(t, e, r.ID)
	if changed.Session != after.PublicName() || changed.Session == r.PublicName() || after.ID != r.ID || after.StateTree != r.StateTree || !reflect.DeepEqual(after.Applied, before) {
		t.Fatal("rename changed identity/storage/runtime", changed, after)
	}
	if _, err := e.Locate(ctx, r.PublicName(), ""); err == nil {
		t.Fatal("old label remained a hidden selector alias")
	}
	if _, err := e.Locate(ctx, after.PublicName(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Locate(ctx, c.Name, ""); err == nil {
		t.Fatal("Docker name became a session selector")
	}
}
