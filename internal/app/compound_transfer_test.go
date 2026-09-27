package app

import (
	"context"
	"reflect"
	"testing"
)

func TestNamedTransfersPreserveIdentityAndExplicitLifetimeRules(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, made.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, made.SessionID)
	forgetSession(t, e, made.SessionID)
	cloned, err := e.Transfer(ctx, TransferOptions{Mode: "clone", Source: made.SessionID, As: "other"})
	if err != nil {
		t.Fatal(err)
	}
	clone := sessionRecord(t, e, cloned.Destination)
	c, _ := sessionSnapshot(t, e, cloned.Destination)
	if clone.ID == source.ID || clone.Settings.LocalName != "other" || !reflect.DeepEqual(clone.Settings.Sources, source.Settings.Sources) || clone.Settings.ManualStart || c.State.Running || c.HostConfig.RestartPolicy.Name != "no" {
		t.Fatal("clone did not get independent automatic lifetime", clone.Settings.Binding, c.State)
	}
	if _, err = e.Start(ctx, cloned.Destination, ""); err != nil {
		t.Fatal(err)
	}
	moved, err := e.Transfer(ctx, TransferOptions{Mode: "relocate", Source: cloned.Destination, As: "third"})
	if err != nil {
		t.Fatal(err)
	}
	dest := sessionRecord(t, e, moved.Destination)
	c, _ = sessionSnapshot(t, e, moved.Destination)
	if dest.ID != clone.ID || dest.Settings.LocalName != "third" || !reflect.DeepEqual(dest.Settings.Sources, clone.Settings.Sources) || !dest.Settings.ManualStart || !c.State.Running || c.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Fatal("relocation lost identity or manual intent", dest.Settings.Binding, c.State)
	}
}
