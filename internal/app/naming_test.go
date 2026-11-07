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
	if !ok || c.Config.Labels[docker.Namespace+".session"] != r.ID {
		t.Fatal("runtime ownership differs from record")
	}
	allocation := strings.TrimPrefix(r.Applied.ImageTag, docker.Namespace+"/session:"+r.ID+"-")
	if len(allocation) != 32 || r.Applied.ImageTag != environment.ImageTag(r.ID, allocation) ||
		r.Applied.Creation.Name != environment.ResourceName(r.Settings.Binding, r.ID, allocation) {
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
	moved, err := e.SetWorkspace(ctx, after.PublicName(), "", t.TempDir())
	if err != nil || !reflect.DeepEqual(moved.Applied, before) {
		t.Fatal("workspace edit renamed or changed the runtime", err)
	}
	if _, exists := d.Snapshot(before.Creation.Name); !exists {
		t.Fatal("metadata edits renamed the live container")
	}
	if _, err := e.Recreate(ctx, RecreateRequest{Target: moved.PublicName(), Options: RecreateOptions{ForceContainer: true}}, false); err != nil {
		t.Fatal(err)
	}
	replaced := sessionRecord(t, e, r.ID)
	if !strings.HasPrefix(replaced.Applied.Creation.Name, replaced.PublicName()+"-") ||
		len(strings.TrimPrefix(replaced.Applied.Creation.Name, replaced.PublicName()+"-")) != 8 ||
		replaced.Applied.ImageTag != before.ImageTag || replaced.ID != r.ID || replaced.StateTree != r.StateTree {
		t.Fatal("replacement lost readable hints, image alias or saved identity", replaced.Applied)
	}
}
