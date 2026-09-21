package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

func (e *Engine) recover(ctx context.Context, l *store.Locked, r *store.Record, desired *environment.Spec) (docker.Container, error) {
	unavailable := func(reason string, cause error) (docker.Container, error) {
		return docker.Container{}, commanderror.New("recovery_unavailable", "Cannot restore container: "+reason, r.Identity.Name, cause,
			commanderror.Next("Recreate with current configuration", "recreate", r.Identity.Name))
	}
	if err := l.RequireIdle(); err != nil {
		return docker.Container{}, err
	}
	image, err := e.Docker.InspectImage(ctx, r.ImageID)
	if err != nil {
		return unavailable("recorded image is unavailable", err)
	}
	if err = image.Verify(e.Store.Installation); err != nil {
		return docker.Container{}, err
	}
	if err = e.Docker.Network(ctx, r.Creation.Network); err != nil {
		return unavailable("recorded network is unavailable", err)
	}
	for _, m := range r.Creation.Mounts {
		if m.Kind == "volume" {
			if err = e.Docker.Volume(ctx, m.Source); err != nil {
				return unavailable("recorded volume is unavailable", err)
			}
			continue
		}
		if _, err = fsutil.Path(filepath.Dir(m.Source), filepath.Base(m.Source)); err != nil {
			return unavailable("recorded bind source is unsafe", err)
		}
		info, statErr := os.Stat(m.Source)
		if statErr != nil {
			return unavailable("recorded bind source is missing", statErr)
		}
		file := m.File
		for _, auth := range r.Auth {
			if auth.Target == m.Target && auth.Kind == "file" {
				file = true
			}
		}
		if (file && !info.Mode().IsRegular()) || (!file && !info.IsDir()) {
			return unavailable("recorded bind source has the wrong kind", nil)
		}
	}
	protected := []string{"/devbox"}
	for _, mount := range r.Creation.Mounts {
		protected = append(protected, mount.Target)
	}
	if err = docker.ValidateRaw(r.Creation.RawArgs, protected, r.Identity.Workspace, ""); err != nil {
		return unavailable("recorded raw Docker inputs are unavailable or invalid", err)
	}
	if err = e.Docker.CheckRawVolumes(ctx, r.Creation.RawArgs); err != nil {
		return unavailable("recorded raw Docker volume is unavailable", err)
	}
	if r.Definition.Origin != "builtin" {
		expected, err := fsutil.Path(e.Store.Home, filepath.Join("harnesses", r.Definition.Name, "harness.json"))
		if err != nil || expected != r.Definition.Origin {
			return unavailable("recorded definition source is unsafe", err)
		}
	}
	definition, hash, err := harness.Recorded(r.Definition.Name, r.Definition.Origin)
	if err != nil || environment.Fingerprint(e.Store.Installation, hash) != r.Definition.Hash {
		return unavailable("recorded environment source is missing or changed", err)
	}
	keys := make([]string, 0, len(definition.Env))
	for k := range definition.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r.Creation.Env = nil
	for _, k := range keys {
		r.Creation.Env = append(r.Creation.Env, k+"="+definition.Env[k])
	}
	var host config.Host
	if desired != nil {
		host = desired.Host
	} else {
		host = config.Snapshot()
	}
	sources := map[string][]byte{}
	for _, source := range r.EnvSources {
		if source.Kind == "file" {
			if err := e.validateEnvSource(*r, source); err != nil {
				return unavailable("recorded environment source path is unsafe", err)
			}
		}
		value, err := source.Restore(e.Store.Installation, host, sources)
		if err != nil {
			return unavailable(err.Error(), err)
		}
		r.Creation.Env = append(r.Creation.Env, value)
	}
	for i, hook := range r.Setup {
		data, err := os.ReadFile(hook.Path)
		if err != nil || environment.Digest(data) != hook.Hash {
			return unavailable("recorded setup input is missing or changed: "+hook.Path, err)
		}
		r.Setup[i].Data = data
	}
	if desired != nil {
		if err = e.syncRecordedConfig(l, r, *desired); err != nil {
			return docker.Container{}, err
		}
	}
	c, err := e.materialize(ctx, *r)
	if err != nil {
		return c, err
	}
	r.SetupContainer = c.ID
	if err = l.Save(*r); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		stopErr := e.Docker.Stop(cleanup, c, e.owner(*r))
		err = errors.Join(err, stopErr)
		if stopErr == nil {
			err = errors.Join(err, e.Docker.Remove(cleanup, c, e.owner(*r)))
		}
	}
	return c, err
}
