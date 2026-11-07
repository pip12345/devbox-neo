package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestExistingOpaqueDockerNameRemainsUsable(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	c, exists := d.Snapshot(r.Applied.Creation.Name)
	if !exists {
		t.Fatal("created runtime is missing")
	}
	d.Forget(r.Applied.Creation.Name)
	allocation := strings.TrimPrefix(r.Applied.ImageTag, "dbx/session:"+r.ID+"-")
	r.Applied.Creation.Name = "dbx-" + r.ID + "-" + allocation
	c.Name = "/" + r.Applied.Creation.Name
	d.SetContainer(c)
	lock, err := e.Store.Lock(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = lock.Save(r)
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, r.PublicName(), ""); err != nil {
		t.Fatal("naming change blocked an existing runtime", err)
	}
	if current := sessionRecord(t, e, r.ID); !reflect.DeepEqual(current.Applied, r.Applied) {
		t.Fatal("access renamed or rewrote an existing runtime")
	}
	if _, exists := d.Snapshot(r.Applied.Creation.Name); !exists {
		t.Fatal("access replaced the existing container")
	}
}

func TestOccupiedReadableDockerNameFailsWithoutReplacingResources(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	spec, err := e.Resolve(resolveRequest(q))
	if err != nil {
		t.Fatal(err)
	}
	id, attempt := "111111111111", strings.Repeat("a", 32)
	name := environment.ResourceName(spec.Identity.Binding, id, attempt)
	d.SetContainer(docker.Container{ID: strings.Repeat("b", 64), Name: "/" + name})
	lock, err := e.Store.Lock(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, _, err := e.createAs(ctx, lock, spec, nil, creationOptions{}, CreationIdentity{ID: id, Attempt: attempt}); err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatal("occupied name was adopted or overwritten", err)
	}
	for _, args := range d.History() {
		if args[0] == "build" || args[0] == "create" || args[0] == "rm" {
			t.Fatal("occupied-name check mutated runtime resources", args)
		}
	}
	if c, exists := d.Snapshot(name); !exists || c.ID != strings.Repeat("b", 64) {
		t.Fatal("occupied container changed")
	}
}
