package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/harness"
)

const thirdDefinition = `{
 "version":1,"name":"third","binary":"bash",
 "install":{"shell":"","path":[]},"launch":{"args":[],"continue_args":[]},
 "stores":[{"name":"state","scope":"environment","target":"/home/devuser/.third-runtime/state"},{"name":"shared","scope":"cache","target":"/home/devuser/.third-cache"}],
 "config":{"store":"state","path":"preferences"},
 "auth":[{"source":"tokens","target":"/home/devuser/.third-runtime/state/private/tokens","kind":"directory","create":true}],
 "config_merge":[{"path":"settings.json","strategy":"json-keys","owned_keys":["controlled"]}],
 "session":{"relocate":true,"clone":true},"prepare":[]
}`

func seedThird(t *testing.T, home string) {
	t.Helper()
	write(t, filepath.Join(home, "harnesses/third/harness.json"), thirdDefinition)
	write(t, filepath.Join(home, "harnesses/third/defaults/settings.json"), `{"controlled":true}`)
}
func TestBuiltinAndCustomHarnessesShareLifecycleAndStorage(t *testing.T) {
	for _, name := range []string{"claude", "pi", "opencode", "third"} {
		t.Run(name, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			if name == "third" {
				seedThird(t, e.Store.Home)
			}
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"`+name+`"}`)
			effective, err := harness.Load(e.Store.Home, name)
			if err != nil {
				t.Fatal(err)
			}
			result, err := createAndOpen(ctx, e, q)
			if err != nil {
				t.Fatal(err)
			}
			first := sessionRecord(t, e, result.SessionID)
			markers := []string{}
			for _, declared := range first.Applied.Stores {
				source := ""
				for _, mount := range first.Applied.Creation.Mounts {
					if mount.Target == declared.Target {
						source = mount.Source
					}
				}
				expected := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses", name, "stores", declared.Name)
				if declared.Scope == "cache" {
					expected = filepath.Join(e.Store.Home, "cache/harnesses", name, declared.Name)
				}
				if source != expected {
					t.Fatalf("wrong store mapping %s: %s", declared.Name, source)
				}
				marker := filepath.Join(source, "preservation-check")
				write(t, marker, "preserved")
				markers = append(markers, marker)
			}
			for _, auth := range first.Applied.Auth {
				source := filepath.Join(e.Store.Home, "auth", name, auth.Source)
				found := false
				for _, mount := range first.Applied.Creation.Mounts {
					if mount.Target == auth.Target {
						found = mount.Source == source
					}
				}
				if !found {
					t.Fatal("auth did not use its managed source")
				}
				if auth.Kind == "directory" {
					source = filepath.Join(source, "preservation-check")
				}
				write(t, source, "{}\n\n")
				markers = append(markers, source)
			}
			for path, file := range effective.Defaults {
				source := filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, result.SessionID).Directory, "harnesses", name, "stores", first.Applied.Config.Store, first.Applied.Config.Path, path)
				if _, err := os.Stat(source); err != nil {
					t.Fatal("managed config did not use declared store/subpath", err)
				}
				if len(first.Applied.Merge) == 0 {
					b, _ := os.ReadFile(source)
					if string(b) != string(file.Data) {
						t.Fatal("ordinary config was changed")
					}
				}
			}
			if _, err = e.Open(ctx, q); err != nil {
				t.Fatal(err)
			}
			if count(d, "create") != 1 {
				t.Fatal("reopen replaced runtime")
			}
			if _, err = e.Recreate(ctx, q, false); err != nil {
				t.Fatal(err)
			}
			after := sessionRecord(t, e, result.SessionID)
			if after.ID != first.ID || after.Applied.SetupContainer == first.Applied.SetupContainer {
				t.Fatal("wrong replacement identity")
			}
			for _, marker := range markers {
				b, err := os.ReadFile(marker)
				if err != nil || (strings.TrimSpace(string(b)) != "preserved" && strings.TrimSpace(string(b)) != "{}") {
					t.Fatal("state/auth/cache lost", err)
				}
			}
			forgetSession(t, e, result.SessionID)
			if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
				t.Fatal("recorded recovery failed", err)
			}
		})
	}
}
