package docker_test

import (
	"context"
	"encoding/csv"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
)

func socketSource(t *testing.T) string {
	t.Helper()
	// Unix socket paths have a small kernel limit; test names can exceed it.
	dir, err := os.MkdirTemp("", "dbx-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	source := filepath.Join(dir, "service.sock")
	listener, err := net.Listen("unix", source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return source
}

func TestSocketMountParsingAndRendering(t *testing.T) {
	source := socketSource(t)
	alias := filepath.Join(filepath.Dir(source), "alias.sock")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ":ro", ":rw"} {
		for _, input := range []string{source, alias, "./service.sock", "~/service.sock"} {
			t.Run(input+suffix, func(t *testing.T) {
				mount, err := docker.ParseMount(input+":/run/service.sock"+suffix, filepath.Dir(source), filepath.Dir(source))
				if err != nil {
					t.Fatal(err)
				}
				if mount.Source != source || mount.Kind != "bind" || !mount.Socket || mount.File || mount.ReadOnly != (suffix == ":ro") {
					t.Fatalf("wrong socket mount: %+v", mount)
				}
				if err := docker.ValidateStoredMount(mount); err != nil {
					t.Fatal(err)
				}
				for _, raw := range []string{
					"--volume=" + source + ":/run/service.sock" + suffix,
					"--mount=type=bind,src=" + source + ",dst=/run/service.sock",
				} {
					if err := docker.ValidateRaw([]string{raw}, []string{"/workspace", "/devbox"}, "", ""); err != nil {
						t.Fatal(raw, err)
					}
				}
				d := &dockertest.Daemon{}
				runtime := docker.Runtime{Runner: d}
				plan := docker.CreatePlan{Name: "socket-test", Image: "image", Network: "default", Mounts: []docker.Mount{mount}}
				if _, err := runtime.Create(context.Background(), plan, docker.Owner{}); err != nil {
					t.Fatal(err)
				}
				args := d.History()[0]
				if suffix != "" {
					i := slices.Index(args, "--volume")
					if i < 0 || args[i+1] != source+":/run/service.sock"+suffix {
						t.Fatalf("wrong volume argv: %q", args)
					}
				} else {
					i := slices.Index(args, "--mount")
					if i < 0 {
						t.Fatalf("missing bind argv: %q", args)
					}
					fields, err := csv.NewReader(strings.NewReader(args[i+1])).Read()
					if err != nil || !slices.Equal(fields, []string{"type=bind", "src=" + source, "dst=/run/service.sock"}) {
						t.Fatalf("wrong bind argv: %q, %v", args, err)
					}
				}
			})
		}
	}
}

func TestSocketMountsPreserveSourceAndTargetRestrictions(t *testing.T) {
	source := socketSource(t)
	fifo := filepath.Join(filepath.Dir(source), "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{fifo, "/dev/null", source + ".missing"} {
		if _, err := docker.ParseMount(source+":/extra", "", ""); err == nil {
			t.Fatal("invalid bind source accepted", source)
		}
		if err := docker.ValidateRaw([]string{"--volume=" + source + ":/extra"}, nil, "", ""); err == nil {
			t.Fatal("invalid raw volume source accepted", source)
		}
	}
	mount, err := docker.ParseMount(source+":/devbox/service.sock", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := docker.ValidateExtraTargets([]docker.Mount{mount}, []string{"/devbox"}); err == nil {
		t.Fatal("socket shadowed managed target")
	}
	for _, flag := range []string{"--volume=" + source + ":/devbox/service.sock", "--mount=type=bind,src=" + source + ",dst=/devbox/service.sock"} {
		if err := docker.ValidateRaw([]string{flag}, []string{"/devbox"}, "", ""); err == nil {
			t.Fatal("raw socket shadowed managed target")
		}
	}
}

func TestStoredMountSourceTypes(t *testing.T) {
	for _, mount := range []docker.Mount{
		{Kind: "bind", Source: "/source", Target: "/target"},
		{Kind: "bind", Source: "/source", Target: "/target", File: true},
		{Kind: "bind", Source: "/source", Target: "/target", Socket: true},
		{Kind: "volume", Source: "shared", Target: "/target"},
	} {
		if err := docker.ValidateStoredMount(mount); err != nil {
			t.Fatal(mount, err)
		}
	}
	for _, mount := range []docker.Mount{
		{Kind: "bind", Source: "/source", Target: "/target", File: true, Socket: true},
		{Kind: "volume", Source: "shared", Target: "/target", Socket: true},
		{Kind: "volume", Source: "shared", Target: "/target", File: true},
	} {
		if err := docker.ValidateStoredMount(mount); err == nil {
			t.Fatal("invalid source type accepted", mount)
		}
	}
}
