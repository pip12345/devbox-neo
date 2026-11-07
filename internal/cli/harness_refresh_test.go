package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"github.com/spf13/cobra"
)

func assertHarnessRefreshBuilds(t *testing.T, d *dockertest.Daemon, start int) {
	t.Helper()
	var builds [][]string
	for _, args := range d.History()[start:] {
		if args[0] == "build" {
			builds = append(builds, args)
		}
	}
	if len(builds) != 2 || slices.Contains(builds[0], "--no-cache") || !slices.Contains(builds[1], "--no-cache") {
		t.Fatal("harness refresh did not preserve earlier-stage cache eligibility", builds)
	}
}

func TestRecreateHarnessFlag(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "all"}[all], func(t *testing.T) {
			e, _, name := namedCLIFixture(t)
			d := e.Docker.Runner.(*dockertest.Daemon)
			before := sessionRecord(t, e, name)
			start := len(d.History())
			localName := ""
			cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &localName)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			args := []string{name, "--harness"}
			if all {
				args = []string{"--all", "--harness"}
			}
			cmd.SetArgs(args)
			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertHarnessRefreshBuilds(t, d, start)
			if after := sessionRecord(t, e, name); after.Applied.ContainerID == before.Applied.ContainerID {
				t.Fatal("CLI harness refresh kept the old container")
			}
		})
	}
}

func TestFrontendRefreshHarness(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "all"}[all], func(t *testing.T) {
			f, out, _, name := frontendFixture(t, strings.NewReader("2\n6\ny\n"))
			d := f.e.Docker.Runner.(*dockertest.Daemon)
			before := sessionRecord(t, f.e, name)
			start := len(d.History())
			target := name
			if all {
				target = ""
			}
			if err := f.recreate(target); err != nil {
				t.Fatal(err, out.String())
			}
			assertHarnessRefreshBuilds(t, d, start)
			if after := sessionRecord(t, f.e, name); after.Applied.ContainerID == before.Applied.ContainerID {
				t.Fatal("menu harness refresh kept the old container")
			}
			for _, text := range []string{"✓ Refresh harness (replace container)", "Earlier image stages may use cache.", "Container-local changes will be lost."} {
				if !strings.Contains(out.String(), text) {
					t.Fatal("menu did not explain refresh consequences", text, out.String())
				}
			}
		})
	}
}

func TestFrontendRefreshHarnessCancellationDoesNotMutate(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("2\n6\nn\n0\n"))
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	before := sessionRecord(t, f.e, name)
	start := len(d.History())
	if err := f.recreate(name); err != nil {
		t.Fatal(err, out.String())
	}
	for _, args := range d.History()[start:] {
		if args[0] == "build" || args[0] == "stop" || args[0] == "rm" || args[0] == "create" {
			t.Fatal("cancelled refresh mutated runtime", args)
		}
	}
	if after := sessionRecord(t, f.e, name); after.Applied.ContainerID != before.Applied.ContainerID {
		t.Fatal("cancelled refresh changed the recorded container")
	}
}
