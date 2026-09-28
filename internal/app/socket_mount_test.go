package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"devbox/internal/commanderror"
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

func TestSocketMountCreationAndRecordedRecovery(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		wantError     string
	}{
		{"socket", "unchanged", ""},
		{"socket", "socket", ""},
		{"socket", "file", "wrong kind"},
		{"socket", "directory", "wrong kind"},
		{"socket", "fifo", "wrong kind"},
		{"socket", "missing", "missing"},
		{"socket", "symlink", "unsafe"},
		{"file", "socket", "wrong kind"},
		{"directory", "socket", "wrong kind"},
	} {
		t.Run(tc.before+"-to-"+tc.after, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			// Keep the Unix socket pathname below the kernel's length limit.
			dir, err := os.MkdirTemp("", "dbx-socket-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			source := filepath.Join(dir, "service.sock")
			remove := mountSource(t, source, tc.before)
			body, err := json.Marshal(config.Layer{Version: 1, Mounts: []string{source + ":/run/service.sock"}})
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(q.Workspace, ".devbox/config.json")
			write(t, configPath, string(body))
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			record := sessionRecord(t, e, created.SessionID)
			found := false
			for _, mount := range record.Applied.Creation.Mounts {
				if mount.Target == "/run/service.sock" {
					found = true
					if mount.Socket != (tc.before == "socket") || mount.File != (tc.before == "file") || mount.Source != source {
						t.Fatalf("source type not recorded: %+v", mount)
					}
				}
			}
			if !found {
				t.Fatal("extra mount missing from creation record")
			}
			inputs := record.Applied.Inputs.Container.Mounts
			if len(inputs) != 1 || inputs[0].Socket != (tc.before == "socket") {
				t.Fatalf("source type missing from fingerprint inputs: %+v", inputs)
			}
			if tc.after != "unchanged" {
				remove()
				if tc.after == "symlink" {
					target := filepath.Join(dir, "other.sock")
					mountSource(t, target, "socket")
					if err := os.Symlink(target, source); err != nil {
						t.Fatal(err)
					}
				} else {
					mountSource(t, source, tc.after)
				}
			}
			// Recovery must check the committed source even if desired config no
			// longer mentions it; otherwise resolution could mask a recovery bug.
			write(t, configPath, `{"version":1}`)
			forgetSession(t, e, created.SessionID)
			creates := count(d, "create")
			_, err = e.Start(ctx, created.SessionID, "")
			if tc.wantError != "" {
				var recoveryError *commanderror.Error
				if !errors.As(err, &recoveryError) || recoveryError.Code != "recovery_unavailable" || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected recovery failure %q, got %v", tc.wantError, err)
				}
				if count(d, "create") != creates {
					t.Fatal("failed source verification created a container")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if count(d, "create") != creates+1 {
					t.Fatal("container was not recovered")
				}
				recovered := sessionRecord(t, e, created.SessionID)
				if !reflect.DeepEqual(recovered.Applied.Creation.Mounts, record.Applied.Creation.Mounts) {
					t.Fatal("recovery changed the recorded mounts")
				}
			}
			if sessionRecord(t, e, created.SessionID).Applied.Fingerprints.Container != record.Applied.Fingerprints.Container {
				t.Fatal("recovery changed committed container inputs")
			}
		})
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
