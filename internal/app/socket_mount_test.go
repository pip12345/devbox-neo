package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"devbox/internal/config"
)

func mountSource(t *testing.T, source, kind string) func() {
	t.Helper()
	switch kind {
	case "socket":
		listener, err := net.Listen("unix", source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		return func() { listener.Close() }
	case "file":
		if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	case "directory":
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
	case "fifo":
		if err := syscall.Mkfifo(source, 0600); err != nil {
			t.Fatal(err)
		}
	case "missing":
		return func() {}
	default:
		t.Fatal("unknown source kind", kind)
	}
	return func() {
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecreateResolvesCurrentMountSourceType(t *testing.T) {
	for _, after := range []string{"socket", "file", "directory", "fifo", "missing"} {
		t.Run(after, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			dir, err := os.MkdirTemp("", "dbx-socket-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			source := filepath.Join(dir, "service.sock")
			remove := mountSource(t, source, "socket")
			body, err := json.Marshal(config.Layer{Version: 1, Mounts: []string{source + ":/run/service.sock"}})
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(q.Workspace, ".devbox/config.json"), string(body))
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			before := sessionRecord(t, e, created.SessionID)
			if !before.Applied.Inputs.Container.Mounts[0].Socket {
				t.Fatal("socket source type not recorded")
			}
			remove()
			mountSource(t, source, after)
			forgetSession(t, e, created.SessionID)
			creates := count(d, "create")
			_, err = e.Recreate(ctx, recreateRequest(q), false)
			if after == "missing" || after == "fifo" {
				if err == nil || count(d, "create") != creates {
					t.Fatal("invalid current bind source was materialized", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, created.SessionID)
			m := r.Applied.Inputs.Container.Mounts[0]
			if r.ID != before.ID || m.Socket != (after == "socket") || m.File != (after == "file") || count(d, "create") != creates+1 {
				t.Fatal("rebuild did not use current source type", m)
			}
		})
	}
}

func TestRemovedMountDoesNotBlockRecreate(t *testing.T) {
	e, _, q := fixture(t)
	source := filepath.Join(t.TempDir(), "removed")
	mountSource(t, source, "file")
	body, _ := json.Marshal(config.Layer{Version: 1, Mounts: []string{source + ":/run/source"}})
	configPath := filepath.Join(q.Workspace, ".devbox/config.json")
	write(t, configPath, string(body))
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	write(t, configPath, `{"version":1}`)
	forgetSession(t, e, made.SessionID)
	if _, err := e.Recreate(context.Background(), recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	if len(sessionRecord(t, e, made.SessionID).Applied.Inputs.Container.Mounts) != 0 {
		t.Fatal("old mount was reconstructed")
	}
}

func TestRecordedManagedMountsCannotBecomeSockets(t *testing.T) {
	e, _, q := fixture(t)
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	for i := range r.Applied.Creation.Mounts {
		mount := &r.Applied.Creation.Mounts[i]
		mount.Socket = true
		if err := r.Validate(r.Directory); err == nil {
			t.Fatal("managed mount accepted as a socket", mount.Target)
		}
		mount.Socket = false
	}
}
