package app

import (
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// creationRecord assembles the captured contract. The caller supplies storage
// location and commits the returned record only after materialization succeeds.
func creationRecord(s environment.Spec, imageID string, mounts []docker.Mount, id string, created, activity time.Time, previous *store.Record, seed CreationIdentity) store.Record {
	d := s.Harness.Definition
	r := store.Record{
		Version: store.RecordVersion, ID: id, Created: created, Activity: activity, Action: "create",
		Settings: store.Settings{Binding: s.Identity.Binding, Sources: s.Sources, ManualStart: seed.ManualStart},
		Applied: store.AppliedState{
			Fingerprints: s.FingerprintsFor(imageID), Inputs: s.Inputs,
			ImageTag: environment.ImageTag(s.Identity.Workspace, s.Identity.LocalName, id), ImageID: imageID,
			Creation: docker.CreatePlan{
				Name: s.Identity.Name, Image: imageID, Network: s.Settings.Network,
				Mounts: mounts, Env: s.Env(), Ports: s.Settings.Ports,
				RawArgs: s.Settings.DockerArgs, Metadata: s.Metadata,
			},
			Definition: store.DefinitionInput{Name: d.Name, Origin: s.Harness.Origin, Hash: s.Harness.Hash},
			Stores:     d.Stores, Auth: d.Auth, Config: d.Config, Merge: d.Merge, Prepare: d.Prepare,
			Launch: store.Launch{
				Binary:   d.Binary,
				Args:     append(append([]string(nil), d.Launch.Args...), s.Settings.HarnessArgs...),
				Continue: d.Launch.Continue, Shell: s.Settings.Shell,
			},
			Setup: s.Setup, BeforeOpen: s.BeforeOpen, Ownership: 1, ManifestVersion: 1,
		},
	}
	if previous != nil {
		r.Settings.ManualStart = previous.Settings.ManualStart
		r.Activity, r.Action = previous.Activity, "recreate"
	}
	return r
}
