package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"devbox/internal/commanderror"
)

func requireSavedNetworks(t *testing.T, e *Engine, id string, want ...string) {
	t.Helper()
	if got := sessionRecord(t, e, id).Settings.SecondaryNetworks; !slices.Equal(got, want) {
		t.Fatalf("saved networks = %v, want %v", got, want)
	}
}

func TestManagedNetworksSurviveReplacementButDockerAttachmentsDoNot(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, created.SessionID)
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
	if sessionRecord(t, e, created.SessionID).Applied.Fingerprints != before.Applied.Fingerprints {
		t.Fatal("network choice changed config fingerprints")
	}
	c, _ := sessionSnapshot(t, e, created.SessionID)
	if err := e.Docker.AttachNetwork(ctx, c, e.owner(before), "external", true); err != nil {
		t.Fatal(err)
	}
	start := len(d.History())
	req := recreateRequest(q)
	req.Options.ForceContainer = true
	if _, err := e.Recreate(ctx, req, false); err != nil {
		t.Fatal(err)
	}
	after := sessionRecord(t, e, created.SessionID)
	if after.ID != before.ID || after.Applied.ContainerID == before.Applied.ContainerID {
		t.Fatal("replacement lost session identity or kept old container")
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
	facts, err := e.NetworkFacts(ctx, created.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := facts.Networks["managed"]; !ok {
		t.Fatal("managed network was not restored")
	}
	if _, ok := facts.Networks["external"]; ok {
		t.Fatal("direct Docker attachment was adopted")
	}
	connected := false
	for _, args := range d.History()[start:] {
		if slices.Equal(args, []string{"network", "connect", "managed", after.Applied.ContainerID}) {
			connected = true
		}
		if (args[0] == "start" || args[0] == "exec") && !connected {
			t.Fatal("container started or setup ran before saved networks were restored")
		}
	}
}

func TestExplicitConnectSavesExistingDockerAttachment(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	c, _ := sessionSnapshot(t, e, created.SessionID)
	if err := e.Docker.AttachNetwork(ctx, c, e.owner(r), "external", true); err != nil {
		t.Fatal(err)
	}
	start := len(d.History())
	for range 2 {
		if err := e.ChangeNetwork(ctx, created.SessionID, "", "external", true); err != nil {
			t.Fatal(err)
		}
	}
	requireSavedNetworks(t, e, created.SessionID, "external")
	for _, args := range d.History()[start:] {
		if args[0] == "network" && args[1] == "connect" {
			t.Fatal("existing attachment was connected again")
		}
	}
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "bridge", true); err != nil {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID, "external")
}

func TestDisconnectForgetsSavedNetworkWithoutContainerOrAttachment(t *testing.T) {
	for _, missingContainer := range []bool{false, true} {
		t.Run(map[bool]string{false: "detached", true: "missing-container"}[missingContainer], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, created.SessionID)
			c, _ := sessionSnapshot(t, e, created.SessionID)
			if missingContainer {
				forgetSession(t, e, created.SessionID)
			} else if err := e.Docker.AttachNetwork(ctx, c, e.owner(r), "managed", false); err != nil {
				t.Fatal(err)
			}
			d.Fail = func(args []string) error {
				if args[0] == "network" {
					return errors.New("network has been deleted")
				}
				return nil
			}
			if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", false); err != nil {
				t.Fatal(err)
			}
			requireSavedNetworks(t, e, created.SessionID)
			d.Fail = nil
			req := recreateRequest(q)
			req.Options.ForceContainer = true
			if _, err := e.Recreate(ctx, req, false); err != nil {
				t.Fatal(err)
			}
			facts, err := e.NetworkFacts(ctx, created.SessionID, "")
			if err != nil || len(facts.Networks) != 1 {
				t.Fatal("disconnected network was restored", facts, err)
			}
		})
	}
}

func TestSavedNetworkPreflightPreservesOldRuntime(t *testing.T) {
	for _, host := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-network", true: "host-conflict"}[host], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
				t.Fatal(err)
			}
			before := sessionRecord(t, e, created.SessionID)
			wantCode := "secondary_network_missing"
			cause := errors.New("missing network")
			if host {
				wantCode = "secondary_network_conflict"
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
			} else {
				d.Fail = func(args []string) error {
					if slices.Equal(args, []string{"network", "inspect", "managed"}) {
						return cause
					}
					return nil
				}
			}
			mutations := runtimeMutations(d)
			req := recreateRequest(q)
			req.Options.ForceContainer = true
			_, err = e.Recreate(ctx, req, false)
			var actionable *commanderror.Error
			if !errors.As(err, &actionable) || actionable.Code != wantCode || len(actionable.Next) != 1 ||
				!slices.Equal(actionable.Next[0].Command, []string{"dbx", "network", "disconnect", "managed", created.Session}) {
				t.Fatal("missing repair guidance", err)
			}
			if !host && !errors.Is(err, cause) {
				t.Fatal("lost Docker error cause", err)
			}
			c, exists := sessionSnapshot(t, e, created.SessionID)
			if runtimeMutations(d) != mutations || !exists || !c.State.Running || c.ID != before.Applied.ContainerID {
				t.Fatal("preflight changed existing runtime")
			}
			requireSavedNetworks(t, e, created.SessionID, "managed")
		})
	}
}

func TestNetworkMutationFailuresRetainSavedIntent(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("network mutation failed")
	fail := func(verb string) {
		d.Fail = func(args []string) error {
			if args[0] == "network" && args[1] == verb {
				return cause
			}
			return nil
		}
	}
	fail("connect")
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID)
	d.Fail = nil
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
		t.Fatal(err)
	}
	fail("disconnect")
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", false); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
	fail("connect")
	before := sessionRecord(t, e, created.SessionID)
	req := recreateRequest(q)
	req.Options.ForceContainer = true
	if _, err := e.Recreate(ctx, req, false); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
	if sessionRecord(t, e, created.SessionID).Applied.ContainerID != before.Applied.ContainerID {
		t.Fatal("failed network restoration committed a new container")
	}
	if containers, err := e.Docker.Inventory(ctx, e.Store.Installation); err != nil || len(containers) != 0 {
		t.Fatal("failed replacement leaked container", containers, err)
	}
	d.Fail = nil
	if _, err := e.Recreate(ctx, req, false); err != nil {
		t.Fatal(err)
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
}

func TestNetworkSaveFailureCanBeRetried(t *testing.T) {
	for _, connect := range []bool{true, false} {
		t.Run(map[bool]string{true: "connect", false: "disconnect"}[connect], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !connect {
				if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(e.Store.Home, "sessions", created.SessionID, "session.json")
			d.Fail = func(args []string) error {
				if len(args) == 4 && args[0] == "network" {
					if err := os.Rename(path, path+".saved"); err != nil {
						return err
					}
					return os.Mkdir(path, 0700)
				}
				return nil
			}
			err = e.ChangeNetwork(ctx, created.SessionID, "", "managed", connect)
			var actionable *commanderror.Error
			if !errors.As(err, &actionable) || actionable.Code != "network_save_failed" {
				t.Fatal(err)
			}
			d.Fail = nil
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+".saved", path); err != nil {
				t.Fatal(err)
			}
			if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", connect); err != nil {
				t.Fatal(err)
			}
			if connect {
				requireSavedNetworks(t, e, created.SessionID, "managed")
			} else {
				requireSavedNetworks(t, e, created.SessionID)
			}
		})
	}
}

func TestBulkRecreatePreflightsSavedNetworksBeforeAnyReplacement(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ChangeNetwork(ctx, second.SessionID, "", "managed", true); err != nil {
		t.Fatal(err)
	}
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"network", "inspect", "managed"}) {
			return errors.New("network unavailable")
		}
		return nil
	}
	before := runtimeMutations(d)
	if _, err := e.RecreateAll(ctx, true, RecreateOptions{ForceContainer: true}); err == nil {
		t.Fatal("bulk preflight ignored missing saved network")
	}
	if runtimeMutations(d) != before {
		t.Fatal("bulk recreation mutated runtime before all networks passed preflight")
	}
	for _, id := range []string{first.SessionID, second.SessionID} {
		if _, exists := sessionSnapshot(t, e, id); !exists {
			t.Fatal("preflight removed a container")
		}
	}
}

func TestRuntimeApplicationAndAccessDoNotReconnectNetworks(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	c, _ := sessionSnapshot(t, e, created.SessionID)
	if err := e.Docker.AttachNetwork(ctx, c, e.owner(r), "managed", false); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","harness_args":["--verbose"]}`)
	start := len(d.History())
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	for _, args := range d.History()[start:] {
		if args[0] == "create" || args[0] == "network" && args[1] == "connect" {
			t.Fatal("ordinary access/runtime-only application reconciled saved networks")
		}
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
}

func TestSavedNetworkBecomingPrimaryIsNotConnectedTwice(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ChangeNetwork(ctx, created.SessionID, "", "managed", true); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"managed"}`)
	start := len(d.History())
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	for _, args := range d.History()[start:] {
		if args[0] == "network" && args[1] == "connect" {
			t.Fatal("primary network was attached twice")
		}
	}
	requireSavedNetworks(t, e, created.SessionID, "managed")
}
