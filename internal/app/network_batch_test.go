package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func TestNetworkAllHandlesMixedSessionsAndContinuesAfterFailures(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	records := map[string]store.Record{}
	for _, name := range []string{"a-bridge", "b-broken", "c-host", "d-missing", "e-primary", "f-external", "g-detached", "h-already"} {
		mode := "default"
		if name == "c-host" {
			mode = "host"
		} else if name == "e-primary" {
			mode = "mcp"
		}
		write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"`+mode+`"}`)
		q.LocalName = name
		made, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if name == "d-missing" || name == "g-detached" || name == "h-already" {
			if err := e.ChangeNetwork(ctx, made.SessionID, "", "mcp", true); err != nil {
				t.Fatal(err)
			}
		}
		if name == "d-missing" {
			forgetSession(t, e, made.SessionID)
		}
		r := sessionRecord(t, e, made.SessionID)
		if name == "f-external" || name == "g-detached" {
			c, _ := sessionSnapshot(t, e, made.SessionID)
			if err := e.Docker.AttachNetwork(ctx, c, e.owner(r), "mcp", name == "f-external"); err != nil {
				t.Fatal(err)
			}
		}
		if name == "a-bridge" {
			if _, err := e.Start(ctx, made.SessionID, ""); err != nil {
				t.Fatal(err)
			}
		}
		records[name] = sessionRecord(t, e, made.SessionID)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	cause := &docker.ExitError{Code: 27, Operation: "network"}
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"network", "connect", "mcp", records["b-broken"].Applied.ContainerID}) {
			return cause
		}
		return nil
	}
	firstCall := len(d.History())
	report, err := e.ChangeNetworkAll(ctx, "mcp", true)
	if !errors.Is(err, cause) || len(report.Results) != len(records) {
		t.Fatal(report, err)
	}
	wantConnect := map[string]BatchStatus{
		"a-bridge": BatchSucceeded, "b-broken": BatchFailed, "c-host": BatchSkipped, "d-missing": BatchSkipped,
		"e-primary": BatchUnchanged, "f-external": BatchSucceeded, "g-detached": BatchSucceeded, "h-already": BatchUnchanged,
	}
	checkStatuses := func(report BatchReport[NetworkFacts], want map[string]BatchStatus) {
		t.Helper()
		for name, status := range want {
			found := false
			for _, item := range report.Results {
				if item.Session == records[name].PublicName() {
					found = true
					if item.Status != status || status == BatchSkipped && item.Reason == "" || status == BatchFailed && item.Err == nil {
						t.Fatalf("%s: got %+v, want %s", name, item, status)
					}
				}
			}
			if !found {
				t.Fatal("session missing from report", name)
			}
		}
	}
	checkStatuses(report, wantConnect)
	for name, r := range records {
		if slices.Contains([]string{"c-host", "e-primary", "b-broken"}, name) {
			requireSavedNetworks(t, e, r.ID)
		} else {
			requireSavedNetworks(t, e, r.ID, "mcp")
		}
	}
	d.Fail = nil
	if _, err := e.ChangeNetworkAll(ctx, "mcp", true); err != nil {
		t.Fatal("retry did not complete failed work", err)
	}
	beforeRepeat := len(d.History())
	report, err = e.ChangeNetworkAll(ctx, "mcp", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.Results {
		if item.Status != BatchUnchanged && item.Status != BatchSkipped {
			t.Fatal("repeated connect was not idempotent", item)
		}
	}
	for _, args := range d.History()[beforeRepeat:] {
		if args[0] == "network" && args[1] != "inspect" {
			t.Fatal("repeated connect changed attachments", args)
		}
	}
	report, err = e.ChangeNetworkAll(ctx, "mcp", false)
	if err != nil {
		t.Fatal("incompatible sessions blocked disconnect", err)
	}
	wantDisconnect := map[string]BatchStatus{}
	for name := range records {
		wantDisconnect[name] = BatchSucceeded
	}
	for _, name := range []string{"c-host", "d-missing", "e-primary"} {
		wantDisconnect[name] = BatchSkipped
	}
	checkStatuses(report, wantDisconnect)
	report, err = e.ChangeNetworkAll(ctx, "mcp", false)
	if err != nil {
		t.Fatal("repeated disconnect failed", err)
	}
	for _, item := range report.Results {
		if item.Status != BatchUnchanged && item.Status != BatchSkipped {
			t.Fatal("repeated disconnect was not idempotent", item)
		}
	}
	for name, before := range records {
		if name == "d-missing" {
			requireSavedNetworks(t, e, before.ID, "mcp")
		} else {
			requireSavedNetworks(t, e, before.ID)
		}
		after := sessionRecord(t, e, before.ID)
		if !reflect.DeepEqual(before.Applied, after.Applied) || before.Settings.ManualStart != after.Settings.ManualStart {
			t.Fatal("network batch changed applied config or running intent", name)
		}
		c, exists := sessionSnapshot(t, e, before.ID)
		if exists != (name != "d-missing") || exists && c.State.Running != (name == "a-bridge") {
			t.Fatal("network batch changed container lifetime", name, c)
		}
	}
	for _, args := range d.History()[firstCall:] {
		if slices.Contains([]string{"start", "stop", "create", "rm", "build"}, args[0]) {
			t.Fatal("network batch changed runtime lifetime", args)
		}
	}
	beforeInspect := len(d.History())
	facts, err := e.NetworkFactsAll(ctx)
	if err != nil || len(facts.Results) != len(records) {
		t.Fatal(facts, err)
	}
	for _, item := range facts.Results {
		if item.Session == records["d-missing"].PublicName() {
			if item.Status != BatchSkipped || item.Result != nil {
				t.Fatal(item)
			}
		} else if item.Status != BatchSucceeded || item.Result == nil || item.Result.Name == "" {
			t.Fatal("inspection lost facts, including host-network facts", item)
		}
	}
	for _, args := range d.History()[beforeInspect:] {
		if args[0] != "container" {
			t.Fatal("batch inspection was not read-only", args)
		}
	}
}

func TestDisconnectAbsentNetworkIsUnchangedForSingleSession(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, made.SessionID)
	firstCall := len(d.History())
	if err := e.ChangeNetwork(ctx, made.SessionID, "", "absent", false); err != nil {
		t.Fatal(err)
	}
	if after := sessionRecord(t, e, made.SessionID); !reflect.DeepEqual(before, after) {
		t.Fatal("no-op disconnect wrote state")
	}
	for _, args := range d.History()[firstCall:] {
		if args[0] == "network" {
			t.Fatal("no-op disconnect called Docker network mutation", args)
		}
	}
}
