package resource

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
)

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
			return result, commanderror.New("harness_required", "No harness selected.", o.Root, nil, commanderror.Step{Command: command, Reason: "Select harness (available: " + strings.Join(choices, ", ") + ")"})
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
		proposed, err := config.ParseLayer(desired)
		if err != nil {
			return result, err
		}
		resolved, err := artifact.PreviewSelection(s.Home, o.Workspace, artifact.Selection{Profile: s.SelectedProfile, IgnoreProject: s.IgnoreProject}, config.Layer{}, &proposed, host)
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
		requested, err = options.ChooseArtifacts(append([]string(nil), SetupArtifacts...))
		if err != nil {
			return result, err
		}
	}
	seeds, err := planArtifacts(&h, requested)
	result.Warnings = append(result.Warnings, seeds.warnings...)
	if err != nil {
		return result, err
	}
	if err = seeds.preflight(o.Root); err != nil {
		return result, err
	}
	if err = seeds.publish(ctx, o.Root, &result); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if string(desired) != string(original) {
		if err = fsutil.Write(result.Path, desired, 0600); err != nil {
			return result, err
		}
	}
	workspace := o.Name
	if o.Kind == "profile" {
		workspace = "<folder>"
	}
	result.Next = []commanderror.Step{commanderror.Next("Create", "create", workspace)}
	if o.Kind == "profile" {
		result.Next = append(result.Next, o.step("set", "Use this profile as default (optional)"))
	}
	return result, nil
}
