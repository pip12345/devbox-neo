package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/harness"
	"github.com/spf13/cobra"
)

func completionFile(t *testing.T, home, rel, body string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// Include directories, modes, and timestamps so creating locks or rewriting an
// unchanged config still counts as a completion side effect.
func completionSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String() + info.ModTime().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			value += string(b)
		}
		result[p] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func runCompletion(t *testing.T, args ...string) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	cmd := New()
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"__complete"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatal(args, err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	directive, err := strconv.Atoi(strings.TrimPrefix(lines[len(lines)-1], ":"))
	if err != nil {
		t.Fatal("missing completion directive", out.String())
	}
	if strings.Contains(stderr.String(), "Error") || strings.Contains(stderr.String(), "Warning") {
		t.Fatal("completion printed a diagnostic", stderr.String())
	}
	return lines[:len(lines)-1], cobra.ShellCompDirective(directive)
}

func TestCompletionUsesSelectedHomeWithoutInitialization(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DEVBOX_HOME", filepath.Join(userHome, "environment"))
	explicit := filepath.Join(userHome, "explicit")
	completionFile(t, os.Getenv("DEVBOX_HOME"), "profiles/env/config.json", `{"version":1}`)
	completionFile(t, explicit, "profiles/basic/config.json", `{"version":1}`)
	completionFile(t, explicit, "profiles/broken/config.json", `broken`)
	completionFile(t, explicit, "profiles/not valid/config.json", `{}`)
	completionFile(t, explicit, "sessions/devbox-example-0123456789ab.profile-basic/session.json", `broken`)
	completionFile(t, explicit, "harnesses/pi/harness.json", `invalid override`)
	custom, err := harness.Load(explicit, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	custom.Definition.Name = "custom"
	definition, err := json.Marshal(custom.Definition)
	if err != nil {
		t.Fatal(err)
	}
	completionFile(t, explicit, "harnesses/custom/harness.json", string(definition))
	if err := os.Symlink(filepath.Join(explicit, "profiles/basic"), filepath.Join(explicit, "profiles/linked")); err != nil {
		t.Fatal(err)
	}
	before := completionSnapshot(t, userHome)
	cases := []struct {
		args []string
		want []string
		dir  cobra.ShellCompDirective
	}{
		{[]string{"profile", "set", ""}, []string{"env"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "profile", "config", "b"}, []string{"basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "open", "--profile", "b"}, []string{"basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "project", "config", ".", "--profile", "b"}, []string{"basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "project", "create", ".", "--from-profile", "b"}, []string{"basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "session", "clone", ".", "--to", ""}, []string{".project", "basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "open", "--harness", ""}, []string{"custom", "opencode"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "project", "init", ".", "--harness", ""}, []string{"custom", "inherit", "opencode"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "session", "list", "--sort", ""}, []string{"last-active", "name"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "open", "--on-exit", ""}, []string{"running", "stop"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "session", "show", "devbox-"}, []string{"devbox-example-0123456789ab.profile-basic"}, cobra.ShellCompDirectiveDefault},
		{[]string{"--home", explicit, "session", "clone", "devbox-example-0123456789ab.profile-basic", ""}, nil, cobra.ShellCompDirectiveFilterDirs},
		{[]string{"--home", explicit, "project", "create", ""}, nil, cobra.ShellCompDirectiveFilterDirs},
		{[]string{"--home", explicit, "create", ""}, nil, cobra.ShellCompDirectiveFilterDirs},
		{[]string{"--home", explicit, "create", ".", ""}, nil, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "create", "--profile", "b"}, []string{"basic", "broken"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "create", "--harness", ""}, []string{"custom", "opencode"}, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "open", ".", "--", ""}, nil, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "exec", ".", "--", ""}, nil, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "session", "reset", "--all", ""}, nil, cobra.ShellCompDirectiveNoFileComp},
		{[]string{"--home", explicit, "session", "delete", "devbox-example-0123456789ab.profile-basic", ""}, nil, cobra.ShellCompDirectiveDefault},
	}
	for _, tt := range cases {
		got, dir := runCompletion(t, tt.args...)
		if !slices.Equal(got, tt.want) || dir != tt.dir {
			t.Fatalf("%v: got %v/%d, want %v/%d", tt.args, got, dir, tt.want, tt.dir)
		}
	}
	if after := completionSnapshot(t, userHome); !reflect.DeepEqual(before, after) {
		t.Fatal("completion mutated the home", before, after)
	}
}

func TestCompletionFreshAndRejectedHomes(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("DEVBOX_HOME", "")
	t.Setenv("PATH", t.TempDir())
	for _, home := range []string{"", filepath.Join(userHome, "fresh"), filepath.Join(userHome, ".devbox")} {
		before := completionSnapshot(t, userHome)
		for _, command := range [][]string{{"open", ""}, {"shell", ""}, {"profile", "set", ""}} {
			args := command
			if home != "" {
				args = append([]string{"--home", home}, command...)
			}
			got, _ := runCompletion(t, args...)
			if len(got) != 0 {
				t.Fatal("unexpected candidates", home, got)
			}
		}
		if !reflect.DeepEqual(before, completionSnapshot(t, userHome)) {
			t.Fatal("completion initialized a home", home)
		}
	}
	completionFile(t, filepath.Join(userHome, ".devbox-neo"), "profiles/default/config.json", `{}`)
	got, _ := runCompletion(t, "profile", "set", "")
	if !slices.Equal(got, []string{"default"}) {
		t.Fatal("default home not used", got)
	}
}

func TestContainerCompletionOnlyInspectsSelectedInstallation(t *testing.T) {
	home := t.TempDir()
	installation := strings.Repeat("a", 32)
	completionFile(t, home, "state/installation-id", installation+"\n")
	owned := docker.Container{ID: "owned-id", Name: "/devbox-owned"}
	owned.Config.Labels = docker.Owner{Installation: installation}.Labels()
	foreign := docker.Container{ID: "foreign-id", Name: "/devbox-foreign"}
	foreign.Config.Labels = docker.Owner{Installation: strings.Repeat("b", 32)}.Labels()
	daemon := &dockertest.Daemon{Containers: map[string]docker.Container{"owned": owned, "foreign": foreign}}
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())
	cmd.Flags().String("home", home, "")
	before := completionSnapshot(t, home)
	source := completionContainers(docker.Runtime{Runner: daemon})
	if got := source(cmd); !slices.Equal(got, []string{"devbox-owned"}) {
		t.Fatal("incorrect installation completion", got)
	}
	if len(daemon.Calls) != 2 || !slices.Equal(daemon.Calls[0][:2], []string{"container", "ls"}) || !slices.Equal(daemon.Calls[1], []string{"container", "inspect", "owned-id"}) {
		t.Fatal("completion did more than batched inventory", daemon.Calls)
	}
	daemon.Fail = func([]string) error { return errors.New("Docker unavailable") }
	if got := source(cmd); len(got) != 0 {
		t.Fatal("failed Docker returned candidates", got)
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("container completion wrote host state")
	}
	blocked := completionContainers(docker.Runtime{Runner: completionBlockedRunner{t: t}})
	if got := blocked(cmd); len(got) != 0 {
		t.Fatal(got)
	}
}

type completionBlockedRunner struct{ t *testing.T }

func (r completionBlockedRunner) Run(ctx context.Context, _ docker.Command) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Second {
		r.t.Fatal("completion Docker work is not bounded")
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestCompletionTargetPositionsAndFiltering(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	source := func(*cobra.Command) []string { return []string{"beta", "alpha", "alpha", "bad\nrow"} }
	for _, tt := range []struct {
		index int
		multi bool
		args  []string
		want  []string
		dir   cobra.ShellCompDirective
	}{
		{0, false, nil, []string{"alpha", "beta"}, cobra.ShellCompDirectiveDefault},
		{0, false, []string{"alpha"}, nil, cobra.ShellCompDirectiveNoFileComp},
		{0, true, []string{"alpha"}, []string{"beta"}, cobra.ShellCompDirectiveDefault},
		{1, false, nil, nil, cobra.ShellCompDirectiveNoFileComp},
		{1, false, []string{"network"}, []string{"alpha", "beta"}, cobra.ShellCompDirectiveDefault},
	} {
		got, dir := completeTarget(source, tt.index, tt.multi)(cmd, tt.args, "")
		if !slices.Equal(got, tt.want) || dir != tt.dir {
			t.Fatal(tt, got, dir)
		}
	}
}
