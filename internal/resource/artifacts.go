package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"devbox/internal/artifact"
	"devbox/internal/filesync"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

var SetupArtifacts = []string{"harness-config", "setup.sh", "before-open.sh", "Dockerfile"}

type artifactSeeds struct {
	files    map[string]harness.File
	names    []string
	warnings []string
}

// File generation uses an explicit harness definition, not the config's
// persistent harness setting. General scripts and Dockerfiles need no harness.
func planArtifacts(h *harness.Effective, requested []string) (artifactSeeds, error) {
	seeds := artifactSeeds{files: map[string]harness.File{}}
	for _, name := range requested {
		switch name {
		case "harness-config":
			if h == nil {
				return seeds, fmt.Errorf("choose which harness's config files to add with --artifact-harness")
			}
			seeds.warnings = append(seeds.warnings, h.Warnings...)
			desiredFiles := map[string]artifact.File{}
			for path, file := range h.Defaults {
				desiredFiles[path] = artifact.File{Data: file.Data, Mode: file.Mode}
				// Built-in guidance stays inherited so updates reach configs that
				// did not explicitly replace the managed Devbox skill.
				if path != "skills/devbox/SKILL.md" {
					seeds.files[filepath.Join(h.Definition.Name, path)] = file
				}
			}
			if err := filesync.Validate(desiredFiles, h.Definition.Merge); err != nil {
				return seeds, err
			}
		case "Dockerfile":
			seeds.files[name] = harness.File{Data: []byte("ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n\n# Runs as the prepared development user. Use sudo for system packages.\n# Devbox installs the selected harness after config customization.\n"), Mode: 0600}
		case "setup.sh":
			seeds.files[name] = harness.File{Data: []byte("#!/bin/bash\nset -euo pipefail\n\n# Runs once per container as devuser; use sudo for system changes.\n"), Mode: 0700}
		case "before-open.sh":
			seeds.files[name] = harness.File{Data: []byte("#!/bin/bash\nset -euo pipefail\n\n# Runs on each normal open, before attaching the harness.\n"), Mode: 0700}
		default:
			return seeds, fmt.Errorf("unsupported artifact %q; available artifacts: %v", name, SetupArtifacts)
		}
	}
	for name := range seeds.files {
		seeds.names = append(seeds.names, name)
	}
	sort.Strings(seeds.names)
	return seeds, nil
}

// Preflight all selected paths before publishing config settings or artifacts.
// Existing regular files belong to the user; setup adds only missing files.
func (s artifactSeeds) preflight(root string) error {
	for _, name := range s.names {
		path, err := fsutil.Path(root, name)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("existing artifact is not a regular file: %s", path)
		}
	}
	return nil
}

func (s artifactSeeds) publish(ctx context.Context, root string, result *Result) error {
	for _, name := range s.names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fsutil.Dir(root, filepath.Dir(name), 0700); err != nil {
			return err
		}
		path := filepath.Join(root, name)
		file := s.files[name]
		err := fsutil.WriteNew(path, file.Data, privateMode(file.Mode))
		if os.IsExist(err) {
			result.Skipped = append(result.Skipped, path)
			continue
		}
		if err != nil {
			return err
		}
		result.Created = append(result.Created, path)
	}
	return nil
}
