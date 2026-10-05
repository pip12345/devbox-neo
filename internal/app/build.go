package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"time"

	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
)

// Every source is staged separately: COPY cannot accidentally see files from
// another config directory, and each source retains its own ignore policy.
func (e *Engine) buildStage(ctx context.Context, stage environment.ImageStage, tag string, args map[string]string, force bool) (docker.Image, error) {
	dir, err := os.MkdirTemp(e.Store.Home, ".build-*")
	if err != nil {
		return docker.Image{}, err
	}
	defer os.RemoveAll(dir)
	root, err := fsutil.Dir(dir, "context", 0700)
	if err != nil {
		return docker.Image{}, err
	}
	names := make([]string, 0, len(stage.Context))
	for name := range stage.Context {
		names = append(names, name)
	}
	sort.Strings(names)
	defer func() {
		for _, name := range names {
			if stage.Context[name].Directory {
				_ = os.Chmod(filepath.Join(root, name), 0700)
			}
		}
	}()
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return docker.Image{}, err
		}
		file := stage.Context[name]
		if file.Directory {
			if _, err = fsutil.Dir(root, name, 0700); err != nil {
				return docker.Image{}, err
			}
			continue
		}
		if _, err = fsutil.Dir(root, filepath.Dir(name), 0700); err != nil {
			return docker.Image{}, err
		}
		p, err := fsutil.Path(root, name)
		if err != nil {
			return docker.Image{}, err
		}
		if err = fsutil.Write(p, file.Data, file.Mode); err != nil {
			return docker.Image{}, err
		}
	}
	for i := len(names) - 1; i >= 0; i-- {
		if file := stage.Context[names[i]]; file.Directory {
			if err = os.Chmod(filepath.Join(root, names[i]), file.Mode); err != nil {
				return docker.Image{}, err
			}
		}
	}
	file := filepath.Join(dir, "Dockerfile")
	if err = fsutil.Write(file, stage.Dockerfile, 0600); err != nil {
		return docker.Image{}, err
	}
	if err = fsutil.Write(file+".dockerignore", stage.Ignore, 0600); err != nil {
		return docker.Image{}, err
	}
	return e.Docker.Build(ctx, docker.BuildPlan{Directory: root, Dockerfile: file, Tag: tag, NoCache: force, Installation: e.Store.Installation, Arguments: args}, e.Streams.Err)
}

func (e *Engine) build(ctx context.Context, s environment.Spec, id string, force bool) (image docker.Image, err error) {
	occupant, exists, err := e.Docker.TaggedImage(ctx, environment.ImageTag(s.Identity.Workspace, s.Identity.LocalName, id))
	if err != nil {
		return image, err
	}
	if exists {
		if err = occupant.Verify(e.Store.Installation); err != nil {
			return image, err
		}
	}
	type temporary struct{ tag, id string }
	var intermediates []temporary
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for i := len(intermediates) - 1; i >= 0; i-- {
			entry := intermediates[i]
			err = errors.Join(err, e.Docker.Untag(cleanup, entry.tag, entry.id, e.Store.Installation))
		}
	}()
	ref := ""
	var previous docker.Image
	build := func(stage environment.ImageStage, final, verifyParent bool) (docker.Image, error) {
		tag := environment.ImageTag(s.Identity.Workspace, s.Identity.LocalName, id)
		if !final {
			nonce, err := fsutil.ID()
			if err != nil {
				return docker.Image{}, err
			}
			tag = docker.Namespace + "/build:" + nonce
		}
		args := maps.Clone(s.Build.Arguments)
		if ref != "" {
			args["DEVBOX_BASE"] = ref
		}
		next, err := e.buildStage(ctx, stage, tag, args, force)
		if err != nil {
			return next, err
		}
		if !final {
			intermediates = append(intermediates, temporary{tag, next.ID})
		}
		if verifyParent {
			if err = next.Extends(previous); err != nil {
				return next, fmt.Errorf("Dockerfile %s must extend DEVBOX_BASE: %w", stage.Source, err)
			}
		}
		ref, previous = tag, next
		return next, nil
	}
	if _, err = build(environment.ImageStage{Dockerfile: s.Build.Prepared}, false, false); err != nil {
		return image, err
	}
	for i, stage := range s.Build.Stages {
		if _, err = build(stage, false, true); err != nil {
			return image, err
		}
		// Finalization restores the contract after the last custom stage.
		if i+1 < len(s.Build.Stages) {
			if _, err = build(environment.ImageStage{Dockerfile: s.Build.Boundary}, false, true); err != nil {
				return image, err
			}
		}
	}
	return build(environment.ImageStage{Dockerfile: s.Build.Runtime, Context: s.Build.InstallContext}, true, true)
}
