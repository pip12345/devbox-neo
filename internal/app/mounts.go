package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"devbox/internal/docker"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"devbox/internal/store"
)

func (e *Engine) start(ctx context.Context, c docker.Container, r store.Record) error {
	if err := c.Verify(e.owner(r)); err != nil {
		return err
	}
	if err := prepareMountParents(r); err != nil {
		return err
	}
	if err := e.syncRestart(ctx, c, r); err != nil {
		return err
	}
	return e.Docker.Start(ctx, c, e.owner(r))
}

// Mounts hide image directories. Nested mount parents must therefore exist in
// their host backing source before Docker starts. This also restores parents
// removed by reset, without loading desired configuration or changing ownership.
func prepareMountParents(r store.Record) error {
	d := harness.Definition{Stores: r.Applied.Stores, Auth: r.Applied.Auth}
	sources := map[string]string{}
	for _, mount := range r.Applied.Creation.Mounts {
		sources[mount.Target] = mount.Source
	}
	for _, parent := range d.MountParents() {
		if parent.Mount == "" {
			continue
		}
		source, ok := sources[parent.Mount]
		if !ok {
			return fmt.Errorf("mount parent has no backing source")
		}
		// Only descendants may be created. A missing backing root can mean lost
		// durable state and must not be replaced with an empty directory.
		if _, err := fsutil.Path(source, "."); err != nil {
			return err
		}
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("mount parent source is unavailable: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("mount parent source is not a directory")
		}
		rel := "."
		if parent.Target != parent.Mount {
			rel = strings.TrimPrefix(parent.Target, parent.Mount+"/")
		}
		if _, err = fsutil.Dir(source, rel, 0700); err != nil {
			return fmt.Errorf("prepare mount parent %s: %w", parent.Target, err)
		}
	}
	return nil
}
