package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/filesync"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

var InitArtifacts = []string{"harness-config", "setup.sh", "entrypoint.sh", "Dockerfile"}

type InitOptions struct {
	Harness         string
	Artifacts       []string
	ChooseHarness   func([]string, []harness.Issue) (string, error)
	ChooseArtifacts func([]string) ([]string, error)
}

func (s Service) Init(ctx context.Context, o Owner, options InitOptions) (Result, error) {
	result := Result{Path: filepath.Join(o.Root, "config.json")}
	lock, err := s.lock(ctx, o.Root)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	original, layer, err := readLayer(o)
	if err != nil {
		return result, err
	}
	host := config.Snapshot()
	selected := options.Harness
	keepSelection := false
	if selected == "" && layer.Harness != nil {
		selected, _, err = config.ExpandString(*layer.Harness, host)
		if err != nil {
			return result, err
		}
		keepSelection = selected != ""
	}
	if selected == "" {
		registry, err := harness.Enumerate(s.Home)
		if err != nil {
			return result, err
		}
		choices := []string{}
		if o.Kind == "project" {
			choices = append(choices, "inherit")
		}
		for _, h := range registry.Valid {
			choices = append(choices, h.Definition.Name)
		}
		if options.ChooseHarness == nil {
			command := append(o.Command("init"), "--harness", "<name>")
			return result, commanderror.New("harness_required", "No harness selected; choose one with --harness. No configuration was changed.", o.Root, nil, commanderror.Step{Command: command, Reason: "Select an available harness: " + fmt.Sprint(choices)})
		}
		selected, err = options.ChooseHarness(choices, registry.Invalid)
		if err != nil {
			return result, err
		}
		if !slices.Contains(choices, selected) {
			return result, fmt.Errorf("invalid harness selection")
		}
	}
	desired := original
	if selected == "inherit" {
		if o.Kind != "project" {
			return result, fmt.Errorf("harness inheritance is project-only")
		}
		desired, err = patch(original, "harness", nil, true)
		if err != nil {
			return result, err
		}
		proposed, err := config.ParseLayer(desired, true)
		if err != nil {
			return result, err
		}
		resolved, err := artifact.PreviewProject(s.Home, o.Workspace, proposed, host)
		if err != nil {
			return result, fmt.Errorf("harness inheritance is unavailable: %w\nInitialize a lower configuration layer or select a harness with --harness NAME.", err)
		}
		selected = resolved.Settings.Harness
		if selected == "" {
			return result, fmt.Errorf("harness inheritance is unavailable.\nInitialize a lower configuration layer or select a harness with --harness NAME.")
		}
		if layer.Harness == nil {
			desired = original
		}
	} else if !keepSelection && (layer.Harness == nil || *layer.Harness != selected) {
		desired, err = patch(original, "harness", selected, false)
		if err != nil {
			return result, err
		}
	}
	h, err := harness.Load(s.Home, selected)
	if err != nil {
		return result, err
	}
	result.Harness = selected
	requested := options.Artifacts
	if options.ChooseArtifacts != nil {
		requested, err = options.ChooseArtifacts(append([]string(nil), InitArtifacts...))
		if err != nil {
			return result, err
		}
	}
	files := map[string]harness.File{}
	for _, name := range requested {
		switch name {
		case "harness-config":
			result.Warnings = append(result.Warnings, h.Warnings...)
			desiredFiles := map[string]artifact.File{}
			for p, f := range h.Defaults {
				desiredFiles[p] = artifact.File{Data: f.Data, Mode: f.Mode}
				// Keep Devbox guidance inherited unless the user explicitly supplies an override.
				if p == "skills/devbox/SKILL.md" {
					continue
				}
				files[filepath.Join(selected, p)] = f
			}
			if err = filesync.Validate(desiredFiles, h.Definition.Merge); err != nil {
				return result, err
			}
		case "Dockerfile":
			files[name] = harness.File{Data: []byte("FROM debian:bookworm-slim\n\n# Add base packages here. Devbox installs its runtime and selected harness afterward.\n"), Mode: 0600}
		case "setup.sh":
			files[name] = harness.File{Data: []byte("#!/bin/bash\nset -euo pipefail\n\n# Runs once per container as devuser; use sudo for system changes.\n"), Mode: 0700}
		case "entrypoint.sh":
			files[name] = harness.File{Data: []byte("#!/bin/bash\nset -euo pipefail\n\n# Runs on each normal open, before attaching the harness.\n"), Mode: 0700}
		default:
			return result, fmt.Errorf("unsupported init artifact %q.\nAvailable artifacts: %v", name, InitArtifacts)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	// Preflight all paths before making changes. Existing regular artifacts are
	// user-owned source files; initialization never refreshes or overwrites them.
	for _, name := range names {
		p, err := fsutil.Path(o.Root, name)
		if err != nil {
			return result, err
		}
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return result, err
		}
		if err == nil && !info.Mode().IsRegular() {
			return result, fmt.Errorf("existing artifact is not a regular file: %s", p)
		}
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if _, err = fsutil.Dir(o.Root, filepath.Dir(name), 0700); err != nil {
			return result, err
		}
		p := filepath.Join(o.Root, name)
		f := files[name]
		err = fsutil.WriteNew(p, f.Data, privateMode(f.Mode))
		if os.IsExist(err) {
			result.Skipped = append(result.Skipped, p)
			continue
		}
		if err != nil {
			return result, err
		}
		result.Created = append(result.Created, p)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if string(desired) != string(original) {
		if err = fsutil.Write(result.Path, desired, 0600); err != nil {
			return result, err
		}
	}
	if o.Kind == "profile" {
		result.Next = []commanderror.Step{o.step("set", "Use this profile by default"), {Command: []string{"devbox-neo", "open", "<folder>", "--profile", o.Name}, Reason: "Open a workspace explicitly with this profile"}}
	} else {
		result.Next = []commanderror.Step{{Command: []string{"devbox-neo", "open", o.Workspace}, Reason: "Open this project"}}
	}
	return result, nil
}
