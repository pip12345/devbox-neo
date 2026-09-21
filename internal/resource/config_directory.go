package resource

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

// SetupOptions separates the persistent harness choice from the files a config
// contributes. An overlay may supply several harness trees and select none.
type SetupOptions struct {
	Harness         *string
	Artifacts       []string
	ArtifactHarness string
}

func (s Service) ConfigDirectory(reference, cwd, userHome string) (Owner, error) {
	path, err := config.ConfigPath(s.Home, cwd, userHome, reference)
	if err != nil {
		return Owner{}, err
	}
	root, err := config.CanonicalPath(path)
	if err != nil {
		return Owner{}, err
	}
	return Owner{Kind: "config", Name: reference, Root: root}, nil
}

// CheckConfigCreation is the pre-prompt check. CreateConfig repeats it under
// the owner lock and publishes without replacement to close the creation race.
func (s Service) CheckConfigCreation(o Owner) error {
	if o.Kind != "config" {
		return fmt.Errorf("expected a config directory")
	}
	root, err := fsutil.Path(o.Root, ".")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(root, "config.json")); err == nil {
		return commanderror.New("config_exists", "Config already exists.", o.Name, nil, o.step("edit", "Edit the existing config"))
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s Service) CreateConfig(ctx context.Context, o Owner, options SetupOptions) (Result, error) {
	result := Result{Path: filepath.Join(o.Root, "config.json")}
	lock, err := s.lock(ctx, o.Root)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	if err = s.CheckConfigCreation(o); err != nil {
		return result, err
	}
	data, seeds, selected, err := s.prepareConfigSetup(o, []byte("{\n  \"version\": 1\n}\n"), options)
	result.Warnings = seeds.warnings
	result.Harness = selected
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if _, err = fsutil.Dir(o.Root, ".", 0700); err != nil {
		return result, err
	}
	if err = fsutil.WriteNew(result.Path, data, 0600); err != nil {
		if os.IsExist(err) {
			return result, commanderror.New("config_exists", "Config already exists.", o.Name, err, o.step("edit", "Edit the existing config"))
		}
		return result, err
	}
	result.Created = append(result.Created, result.Path)
	if err = seeds.publish(ctx, o.Root, &result); err != nil {
		return result, commanderror.New("config_setup_incomplete", "Config created, but adding optional files failed.", o.Name, err, o.step("edit", "Finish adding optional files"))
	}
	result.Next = []commanderror.Step{commanderror.Next("Create a session using this config", "create", "<folder>", "--config", o.Root)}
	return result, nil
}

// EditConfig is the direct setup operation used by flags and artifact menus.
// Interactive setting changes still use SetConfigField's snapshot comparison.
// No prompts occur while the shared configuration-owner lock is held.
func (s Service) EditConfig(ctx context.Context, o Owner, options SetupOptions) (Result, error) {
	result := Result{Path: filepath.Join(o.Root, "config.json")}
	if o.Kind != "config" {
		return result, fmt.Errorf("expected a config directory")
	}
	if options.Harness == nil && len(options.Artifacts) == 0 {
		return result, fmt.Errorf("specify --harness or --artifact, or open config edit in a terminal")
	}
	lock, err := s.lock(ctx, o.Root)
	if err != nil {
		return result, err
	}
	defer fsutil.Unlock(lock)
	original, _, err := readLayer(o)
	if err != nil {
		return result, err
	}
	data, seeds, selected, err := s.prepareConfigSetup(o, original, options)
	result.Warnings = seeds.warnings
	result.Harness = selected
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if !bytes.Equal(original, data) {
		if err = fsutil.Write(result.Path, data, 0600); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, result.Path)
	}
	if err = seeds.publish(ctx, o.Root, &result); err != nil {
		return result, commanderror.New("config_setup_incomplete", "Adding optional files failed; completed changes were kept.", o.Name, err, o.step("edit", "Finish adding optional files"))
	}
	return result, nil
}

func (s Service) prepareConfigSetup(o Owner, original []byte, options SetupOptions) ([]byte, artifactSeeds, string, error) {
	var seeds artifactSeeds
	if options.ArtifactHarness != "" && !slices.Contains(options.Artifacts, "harness-config") {
		return nil, seeds, "", fmt.Errorf("--artifact-harness requires --artifact harness-config")
	}
	data := original
	if options.Harness != nil {
		if _, err := harness.Load(s.Home, *options.Harness); err != nil {
			return nil, seeds, "", err
		}
		var err error
		data, err = patch(original, "harness", *options.Harness, false)
		if err != nil {
			return nil, seeds, "", err
		}
	}
	layer, err := config.ParseLayer(data)
	if err != nil {
		return nil, seeds, "", err
	}
	selected := ""
	if layer.Harness != nil {
		selected = *layer.Harness
	}
	var target *harness.Effective
	if slices.Contains(options.Artifacts, "harness-config") {
		name := options.ArtifactHarness
		if name == "" && layer.Harness != nil {
			name, _, err = config.ExpandString(*layer.Harness, config.Snapshot())
			if err != nil {
				return nil, seeds, selected, err
			}
		}
		if name != "" {
			loaded, err := harness.Load(s.Home, name)
			if err != nil {
				return nil, seeds, selected, err
			}
			target = &loaded
		}
	}
	seeds, err = planArtifacts(target, options.Artifacts)
	if err == nil {
		err = seeds.preflight(o.Root)
	}
	return data, seeds, selected, err
}
