package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func TestNestedMountParentsUseBackingSourcesAndRestoreMissingParents(t *testing.T) {
	e, daemon, q := fixture(t)
	ctx := context.Background()
	seedThird(t, e.Store.Home)
	effective, err := harness.Load(e.Store.Home, "third")
	if err != nil {
		t.Fatal(err)
	}
	d := effective.Definition
	d.Auth = append(d.Auth,
		harness.Auth{Source: "nested.json", Target: d.Auth[0].Target + "/nested/file.json", Kind: "file", Create: true},
		harness.Auth{Source: "cache.json", Target: d.Stores[1].Target + "/private/file.json", Kind: "file", Create: true},
	)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "harnesses/third/harness.json"), string(b))
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"third"}`)
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, opened.Name)
	roots := map[string]string{}
	for _, m := range r.Creation.Mounts {
		roots[m.Target] = m.Source
	}
	state := roots[d.Stores[0].Target]
	auth := roots[d.Auth[0].Target]
	cache := roots[d.Stores[1].Target]
	for _, p := range []string{filepath.Join(state, "private"), filepath.Join(auth, "nested"), filepath.Join(cache, "private")} {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatal("parent not prepared privately", p, err)
		}
	}
	if _, err = os.Stat(filepath.Join(state, "private/tokens/nested")); !os.IsNotExist(err) {
		t.Fatal("nested auth parent created in hidden store instead of auth source", err)
	}
	marker := filepath.Join(auth, "untouched")
	write(t, marker, "keep")
	if err = os.Chmod(marker, 0640); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(state, "private")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(state, "private")); !os.IsNotExist(err) {
		t.Fatal("fixture did not remove parent", err)
	}
	// The daemon boundary must not see a start until host-backed ancestors exist.
	daemon.Fail = func(args []string) error {
		if args[0] == "start" {
			_, err := os.Stat(filepath.Join(state, "private"))
			return err
		}
		return nil
	}
	if _, err = e.Start(ctx, opened.Name, ""); err != nil {
		t.Fatal("recorded start did not restore parents", err)
	}
	info, err := os.Stat(marker)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("parent preparation changed existing content", err)
	}
	if b, err = os.ReadFile(marker); err != nil || string(b) != "keep" {
		t.Fatal("parent preparation overwrote content", err)
	}
}

func TestMountParentPreparationRejectsMissingRootsAndSymlinks(t *testing.T) {
	for _, kind := range []string{"missing-root", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			e, daemon, q := fixture(t)
			ctx := context.Background()
			seedThird(t, e.Store.Home)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"third"}`)
			opened, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := record(t, e, opened.Name)
			state := filepath.Join(e.Store.Home, "sessions", opened.Name, "harnesses/third/stores/state")
			outside := t.TempDir()
			if kind == "missing-root" {
				err = os.RemoveAll(state)
			} else {
				err = os.RemoveAll(filepath.Join(state, "private"))
				if err == nil {
					err = os.Symlink(outside, filepath.Join(state, "private"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := count(daemon, "start")
			if _, err = e.Start(ctx, opened.Name, ""); err == nil {
				t.Fatal("unsafe parent accepted", r.Identity.Name)
			}
			if count(daemon, "start") != before {
				t.Fatal("Docker started before parent validation")
			}
			if kind == "missing-root" {
				if _, err = os.Stat(state); !os.IsNotExist(err) {
					t.Fatal("missing durable root recreated", err)
				}
			}
			files, err := os.ReadDir(outside)
			if err != nil || len(files) != 0 {
				t.Fatal("symlink target modified", err)
			}
			if kind == "symlink" {
				if err = prepareMountParents(r); err == nil || !strings.Contains(err.Error(), "symlink") {
					t.Fatal("missing symlink diagnostic", err)
				}
			}
		})
	}
}
