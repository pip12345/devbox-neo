package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/resource"
)

func TestWorkspaceProjectSourceIsUsedByEveryAccessPath(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	dir := filepath.Join(q.Workspace, ".devbox")
	write(t, filepath.Join(dir, "config.json"), `{"env":["TOKEN=value"],"ports":["8080:80"]}`)
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, made.Name)
	if !strings.HasSuffix(made.Name, ".profile-test.project") || len(r.Sources) != 2 || r.Sources[1].Path != dir {
		t.Fatal(r.Identity, r.Sources)
	}
	if got, err := e.Open(ctx, q); err != nil || got.Name != made.Name {
		t.Fatal(got, err)
	}
	if status, err := e.Status(ctx, q.Workspace, q.Profile); err != nil || status.ConfigError != "" || status.Name != made.Name {
		t.Fatal(status, err)
	}
	s := resource.Service{Home: e.Store.Home, SelectedProfile: q.Profile}
	owner, err := s.Project(q.Workspace)
	if err != nil || owner.Root != dir {
		t.Fatal(owner, err)
	}
	view, err := s.ShowProject(made.Name, "")
	if err != nil || view.Path != filepath.Join(dir, "config.json") {
		t.Fatal(view, err)
	}
	// Missing-container recovery restores env from the workspace's project config.
	d.Forget(made.Name)
	if _, err = e.Start(ctx, q.Workspace, q.Profile); err != nil {
		t.Fatal(err)
	}
	if err = e.Stop(ctx, made.Name, "", false); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Open(ctx, Request{Workspace: made.Name}); err == nil {
		t.Fatal("missing recorded project source was ignored")
	}
}

func TestGenericSourcesComposeWithoutProfileOrProjectRoles(t *testing.T) {
	e, _, q := fixture(t)
	a, b := t.TempDir(), t.TempDir()
	write(t, filepath.Join(a, "config.json"), `{"harness":"pi","ports":["8080:80"]}`)
	write(t, filepath.Join(b, "config.json"), `{"env":["FLAG=yes"]}`)
	sources := []config.Source{{Label: "first", Path: a}, {Label: "second", Path: b}}
	r, err := artifact.PreviewSelection(e.Store.Home, q.Workspace, artifact.Selection{Sources: sources}, config.Layer{}, nil, config.Host{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Harness != "pi" || !slices.Equal(r.Settings.Ports, []string{"8080:80"}) || !reflect.DeepEqual(r.Selection.Sources, sources) {
		t.Fatal(r)
	}
	write(t, filepath.Join(b, "config.json"), `{"inherit":false,"harness":"opencode"}`)
	r, err = artifact.PreviewSelection(e.Store.Home, q.Workspace, artifact.Selection{Sources: sources}, config.Layer{}, nil, config.Host{})
	if err != nil || r.Settings.Harness != "opencode" || len(r.Settings.Ports) != 0 || len(r.Selection.Sources) != 1 {
		t.Fatal(r, err)
	}
}

func TestSourceCutoffSkipsBrokenPrecedingInputs(t *testing.T) {
	e, _, q := fixture(t)
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"inherit":false,"harness":"pi"}`)
	write(t, filepath.Join(e.Store.Home, "profiles/test/Dockerfile"), "invalid and unreadable input")
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, made.Name)
	if r.Identity.Slot != "project" || r.Identity.Profile != "" || len(r.Sources) != 1 || len(r.Inputs.Image.Stages) != 0 {
		t.Fatal(r.Identity, r.Sources, r.Inputs.Image)
	}
}

func TestProfileAndProjectScriptsRunInOrderAndStopOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			e, d, q := fixture(t)
			profile := filepath.Join(e.Store.Home, "profiles/test")
			project := filepath.Join(q.Workspace, ".devbox")
			write(t, filepath.Join(project, "config.json"), `{}`)
			for _, root := range []string{profile, project} {
				label := "project"
				if root == profile {
					label = "profile"
				}
				write(t, filepath.Join(root, "setup.sh"), label+" setup")
				write(t, filepath.Join(root, "before-open.sh"), label+" open")
			}
			var hooks []string
			d.Attached = func(_ context.Context, c docker.Command) error {
				if c.Stdin != nil {
					data, _ := io.ReadAll(c.Stdin)
					hooks = append(hooks, string(data))
					if fail && string(data) == "profile open" {
						return os.ErrPermission
					}
				}
				return nil
			}
			made, err := e.Create(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(hooks, []string{"profile setup", "project setup"}) {
				t.Fatal(hooks)
			}
			_, err = e.Open(context.Background(), Request{Workspace: made.Name})
			if (err != nil) != fail {
				t.Fatal(err)
			}
			want := []string{"profile setup", "project setup", "profile open"}
			if !fail {
				want = append(want, "project open")
			}
			if !slices.Equal(hooks, want) {
				t.Fatal(hooks)
			}
		})
	}
}

func TestProjectAndProfileOnlyRemainDistinct(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	plain, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{}`)
	combined, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if combined.Name == plain.Name {
		t.Fatal("shared identity")
	}
	if r, err := e.Locate(ctx, q.Workspace, q.Profile); err != nil || r.Identity.Name != combined.Name {
		t.Fatal(r.Identity, err)
	}
	e.IgnoreProject = true
	if r, err := e.Locate(ctx, q.Workspace, q.Profile); err != nil || r.Identity.Name != plain.Name {
		t.Fatal(r.Identity, err)
	}
}

func TestTransferUsesDestinationWorkspaceProject(t *testing.T) {
	e, _, q := fixture(t)
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"ports":["8080:80"]}`)
	destination := t.TempDir()
	write(t, filepath.Join(destination, ".devbox/config.json"), `{"ports":["9090:90"]}`)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := e.Transfer(context.Background(), TransferOptions{Mode: "clone", Source: made.Name, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, copied.Destination)
	if r.Sources[1].Path != filepath.Join(destination, ".devbox") || !slices.Equal(r.Creation.Ports, []string{"9090:90"}) {
		t.Fatal(r.Identity, r.Sources, r.Creation.Ports)
	}
}
