package app

import (
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/store"
)

// creationRecord assembles the contract before materialization. Its caller owns
// identity allocation and clock reads; only a successful materialization supplies
// SetupContainer and permits publication of this record.
func creationRecord(s environment.Spec, imageID string, mounts []docker.Mount, id string, created, activity time.Time, previous *store.Record, seed CreationIdentity) store.Record {
	d := s.Harness.Definition
	record := store.Record{
		Version:  store.RecordVersion,
		ID:       id,
		Identity: s.Identity,
		Sources:  s.Sources,
		Created:  created,
		Activity: activity,
		Action:   "create",
		Applied:  s.FingerprintsFor(imageID),
		Inputs:   s.Inputs,
		ImageTag: docker.Namespace + "/session:" + id,
		ImageID:  imageID,
		Creation: docker.CreatePlan{
			Name: s.Identity.Name, Image: imageID, Network: s.Settings.Network,
			Mounts: mounts, Env: s.Env(), Ports: s.Settings.Ports,
			RawArgs: s.Settings.DockerArgs, Metadata: s.Metadata,
		},
		EnvSources: s.EnvSources,
		Definition: store.DefinitionInput{Name: d.Name, Origin: s.Harness.Origin, Hash: s.Harness.Hash},
		Stores:     d.Stores,
		Auth:       d.Auth,
		Config:     d.Config,
		Merge:      d.Merge,
		Prepare:    d.Prepare,
		Launch: store.Launch{
			Binary:   d.Binary,
			Args:     append(append([]string(nil), d.Launch.Args...), s.Settings.HarnessArgs...),
			Continue: d.Launch.Continue,
			Shell:    s.Settings.Shell,
		},
		Setup:           s.Setup,
		Ownership:       1,
		ManifestVersion: 1,
	}
	record.ManualStart = seed.ManualStart
	if previous != nil {
		record.ManualStart = previous.ManualStart
		record.Activity = previous.Activity
		record.Action = "recreate"
	} else {
		if !seed.Activity.IsZero() {
			record.Activity = seed.Activity
		}
		if seed.Action != "" {
			record.Action = seed.Action
		}
	}
	return record
}
