package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
)

func TestFailedAccessDiagnosesActualNetworkIDs(t *testing.T) {
	for _, action := range []string{"open", "start", "shell", "exec"} {
		for _, secondary := range []bool{false, true} {
			t.Run(action+map[bool]string{false: "/primary", true: "/secondary"}[secondary], func(t *testing.T) {
				e, d, q := fixture(t)
				ctx := context.Background()
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"primary"}`)
				created, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				name := "primary"
				if secondary {
					name = "secondary"
					if err := e.ChangeNetwork(ctx, created.SessionID, "", name, true); err != nil {
						t.Fatal(err)
					}
				}
				before := sessionRecord(t, e, created.SessionID)
				c, _ := sessionSnapshot(t, e, created.SessionID)
				endpoint := c.NetworkSettings.Networks[name]
				endpoint.NetworkID = "removed-network-id"
				c.NetworkSettings.Networks[name] = endpoint
				d.Containers[before.Applied.Creation.Name] = c
				// The same network name remains inspectable, as when Compose has
				// recreated it. The old endpoint ID is what prevents startup.
				startFailure := &docker.ExitError{Code: 23, Operation: "start"}
				networkFailure := errors.New("old network ID unavailable")
				d.Fail = func(args []string) error {
					if args[0] == "start" {
						return startFailure
					}
					if slices.Equal(args, []string{"network", "inspect", endpoint.NetworkID}) {
						return networkFailure
					}
					return nil
				}
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
				firstCall := len(d.History())
				err = accessAction(ctx, e, q, created.SessionID, action)
				var actionable *commanderror.Error
				if !errors.As(err, &actionable) || actionable.Code != "container_network_unavailable" || actionable.Target != created.Session ||
					!errors.Is(err, startFailure) || !errors.Is(err, networkFailure) || !strings.Contains(actionable.Message, name) {
					t.Fatal(err, actionable)
				}
				if len(actionable.Next) != 2 || !slices.Equal(actionable.Next[0].Command, []string{"docker", "network", "inspect", endpoint.NetworkID}) ||
					!slices.Equal(actionable.Next[1].Command, []string{"dbx", "recreate", "--container", created.Session}) {
					t.Fatal(actionable.Next)
				}
				var exit interface{ ExitCode() int }
				if !errors.As(err, &exit) || exit.ExitCode() != 23 {
					t.Fatal("lost startup exit status", err)
				}
				if after := sessionRecord(t, e, created.SessionID); !reflect.DeepEqual(before, after) {
					t.Fatal("diagnosis changed saved state")
				}
				for _, args := range d.History()[firstCall:] {
					if slices.Contains([]string{"create", "rm", "stop", "exec"}, args[0]) || args[0] == "network" && args[1] != "inspect" {
						t.Fatal("diagnosis mutated runtime", args)
					}
				}
			})
		}
	}
}

func TestUnrelatedStartFailureIsNotReportedAsNetworkFailure(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("unrelated startup failure")
	d.Fail = func(args []string) error {
		if args[0] == "start" {
			return cause
		}
		return nil
	}
	if _, err := e.Start(ctx, created.SessionID, ""); err != cause {
		t.Fatal("network diagnosis replaced unrelated error", err)
	}
}

func TestCancelledStartDoesNotProbeNetworks(t *testing.T) {
	e, d, q := fixture(t)
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	c, _ := sessionSnapshot(t, e, created.SessionID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(d.History())
	if err := e.diagnoseStartNetworks(ctx, c, r, context.Canceled); err != context.Canceled || len(d.History()) != before {
		t.Fatal("cancelled startup performed network diagnosis", err)
	}
}

func TestNetworkRefreshFailureReportsCommittedChangeWithoutStopping(t *testing.T) {
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
			if _, err := e.Start(ctx, created.SessionID, ""); err != nil {
				t.Fatal(err)
			}
			before := sessionRecord(t, e, created.SessionID)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
			cause := &docker.ExitError{Code: 17, Operation: "exec"}
			d.Fail = func(args []string) error {
				if args[0] == "exec" {
					return cause
				}
				return nil
			}
			firstCall := len(d.History())
			err = e.ChangeNetwork(ctx, created.SessionID, "", "managed", connect)
			var actionable *commanderror.Error
			if !errors.As(err, &actionable) || actionable.Code != "network_refresh_failed" || actionable.Target != created.Session ||
				!errors.Is(err, cause) || !strings.Contains(actionable.Message, "applied and saved") {
				t.Fatal(err, actionable)
			}
			if len(actionable.Next) != 1 || !slices.Equal(actionable.Next[0].Command, []string{"dbx", "network", "inspect", created.Session}) {
				t.Fatal(actionable.Next)
			}
			if connect {
				requireSavedNetworks(t, e, created.SessionID, "managed")
			} else {
				requireSavedNetworks(t, e, created.SessionID)
			}
			c, _ := sessionSnapshot(t, e, created.SessionID)
			_, attached := c.NetworkSettings.Networks["managed"]
			if !c.State.Running || attached != connect {
				t.Fatal("refresh failure changed running state or attachment", c)
			}
			if after := sessionRecord(t, e, created.SessionID); !reflect.DeepEqual(before.Applied, after.Applied) || before.Settings.ManualStart != after.Settings.ManualStart {
				t.Fatal("network change altered applied config or running intent")
			}
			d.Fail = nil
			retry := len(d.History())
			if err := e.Exec(ctx, created.SessionID, "", []string{"true"}, false); err != nil {
				t.Fatal("access did not retry the refresh", err)
			}
			refreshed := false
			for _, args := range d.History()[firstCall:] {
				if slices.Contains([]string{"start", "stop", "create", "rm"}, args[0]) {
					t.Fatal("network change or refresh retry disrupted runtime", args)
				}
			}
			for _, args := range d.History()[retry:] {
				if args[0] == "cp" && strings.HasSuffix(args[2], ":/devbox") {
					refreshed = true
				}
				if args[0] == "network" {
					t.Fatal("refresh retry reconciled attachments", args)
				}
			}
			if !refreshed {
				t.Fatal("access did not refresh network facts")
			}
		})
	}
}
