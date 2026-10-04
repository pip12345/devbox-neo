package app

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

func TestCreationRecordParity(t *testing.T) {
	for _, mode := range []string{"create", "recreate", "recreate-manual", "prepared", "prepared-defaults"} {
		t.Run(mode, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			profile := filepath.Join(e.Store.Home, "profiles/test")
			write(t, filepath.Join(profile, "config.json"), `{"version":1,"harness":"pi","harness_args":["--verbose"],"env":["TOKEN=not-saved"],"ports":["8080:80"],"docker_args":["--memory=256m"],"shell":["bash","-l"]}`)
			write(t, filepath.Join(profile, "setup.sh"), "echo setup")
			s, err := e.Resolve(q)
			if err != nil {
				t.Fatal(err)
			}
			var previous *store.Record
			if strings.HasPrefix(mode, "recreate") {
				result, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				r := sessionRecord(t, e, result.SessionID)
				r.Settings.ManualStart = mode == "recreate-manual"
				previous = &r
			}
			seed := CreationIdentity{}
			fixed := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
			if strings.HasPrefix(mode, "prepared") || previous != nil {
				seed = CreationIdentity{ID: strings.Repeat("f", 32), Created: fixed}
			}
			if mode == "prepared" || previous != nil {
				seed.ManualStart = true
			}
			directory, lockedID := "prepared-directory", strings.Repeat("f", 32)
			if previous != nil {
				directory, lockedID = previous.Directory, previous.ID
			}
			l, err := e.Store.Lock(ctx, directory, lockedID)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			start := time.Now().UTC()
			r, c, err := e.createAs(ctx, l, s, previous, creationOptions{}, seed)
			end := time.Now().UTC()
			if err != nil {
				t.Fatal(err)
			}
			id, created, activity, action, manual := r.ID, r.Created, r.Activity, "create", false
			switch {
			case previous != nil:
				id, created, activity, action, manual = previous.ID, previous.Created, previous.Activity, "recreate", previous.Settings.ManualStart
			case mode == "prepared":
				id, created, manual = seed.ID, seed.Created, seed.ManualStart
			case mode == "prepared-defaults":
				id, created = seed.ID, seed.Created
			}
			if mode == "create" && (len(r.ID) != 32 || r.Created.Before(start) || r.Created.After(end)) {
				t.Fatal("creation identity/time not allocated at creation", r.ID, r.Created)
			}
			if previous == nil && (r.Activity.Before(start) || r.Activity.After(end)) {
				t.Fatal("default activity not set at creation", r.Activity)
			}
			root := filepath.Join(e.Store.Home, "sessions", directory)
			mounts := []docker.Mount{{Source: s.Identity.Workspace, Target: "/workspace"}, {Source: filepath.Join(root, sshshare.RelativeRoot), Target: sshshare.Mount}}
			d := s.Harness.Definition
			for _, st := range d.Stores {
				source := filepath.Join(root, "harnesses", d.Name, "stores", st.Name)
				if st.Scope != "environment" {
					source = filepath.Join(e.Store.Home, "cache/harnesses", d.Name, st.Name)
				}
				mounts = append(mounts, docker.Mount{Source: source, Target: st.Target})
			}
			for _, auth := range d.Auth {
				mounts = append(mounts, docker.Mount{Source: filepath.Join(e.Store.Home, "auth", d.Name, auth.Source), Target: auth.Target})
			}
			mounts = append(mounts, s.ExtraMounts...)
			want := store.Record{
				Version: store.RecordVersion, ID: id, Directory: directory, Created: created, Activity: activity, Action: action, Settings: store.Settings{Binding: s.Identity.Binding, Sources: s.Sources,
					ManualStart: manual}, Applied: store.AppliedState{Fingerprints: s.FingerprintsFor(c.Image), Inputs: s.Inputs,
					ImageTag: environment.ImageTag(s.Identity.Workspace, s.Identity.LocalName, id), ImageID: c.Image,
					Creation:   docker.CreatePlan{Name: strings.TrimPrefix(c.Name, "/"), Image: c.Image, Network: s.Settings.Network, Mounts: mounts, Env: s.Env(), Ports: s.Settings.Ports, RawArgs: s.Settings.DockerArgs, Metadata: s.Metadata},
					Definition: store.DefinitionInput{Name: d.Name, Origin: s.Harness.Origin, Hash: s.Harness.Hash},
					Stores:     d.Stores, Auth: d.Auth, Config: d.Config, Merge: d.Merge, Prepare: d.Prepare,
					Launch: store.Launch{Binary: d.Binary, Args: append(append([]string(nil), d.Launch.Args...), s.Settings.HarnessArgs...), Continue: d.Launch.Continue, Shell: s.Settings.Shell},
					Setup:  s.Setup, SetupContainer: c.ID, Ownership: 1, ManifestVersion: 1},
			}
			if !reflect.DeepEqual(r, want) {
				t.Fatalf("returned record changed:\ngot  %#v\nwant %#v", r, want)
			}
			wantJSON, err := json.MarshalIndent(want, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			saved := getFile(t, filepath.Join(root, "session.json"))
			if !bytes.Equal(saved, append(wantJSON, '\n')) || bytes.Contains(saved, []byte("not-saved")) {
				t.Fatal("saved record differs from the creation contract or contains env values")
			}
		})
	}
}
