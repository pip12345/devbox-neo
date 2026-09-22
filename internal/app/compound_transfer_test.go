package app

import (
	"context"
	"reflect"
	"testing"
)

func TestNamedTransfersPreserveIdentityAndExplicitLifetimeRules(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, made.Name, ""); err != nil {
		t.Fatal(err)
	}
	source := record(t, e, made.Name)
	d.Forget(made.Name)
	cloned, err := e.Transfer(ctx, TransferOptions{Mode: "clone", Source: made.Name, As: "other"})
	if err != nil {
		t.Fatal(err)
	}
	clone := record(t, e, cloned.Destination)
	c, _ := d.Snapshot(cloned.Destination)
	if clone.ID == source.ID || clone.Identity.LocalName != "other" || !reflect.DeepEqual(clone.Sources, source.Sources) || clone.ManualStart || c.State.Running || c.HostConfig.RestartPolicy.Name != "no" {
		t.Fatal("clone did not get independent automatic lifetime", clone.Identity, c.State)
	}
	if _, err = e.Start(ctx, cloned.Destination, ""); err != nil {
		t.Fatal(err)
	}
	moved, err := e.Transfer(ctx, TransferOptions{Mode: "relocate", Source: cloned.Destination, As: "third"})
	if err != nil {
		t.Fatal(err)
	}
	dest := record(t, e, moved.Destination)
	c, _ = d.Snapshot(moved.Destination)
	if dest.ID != clone.ID || dest.Identity.LocalName != "third" || !reflect.DeepEqual(dest.Sources, clone.Sources) || !dest.ManualStart || !c.State.Running || c.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Fatal("relocation lost identity or manual intent", dest.Identity, c.State)
	}
}
