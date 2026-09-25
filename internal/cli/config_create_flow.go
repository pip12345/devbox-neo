package cli

import (
	"slices"
	"strings"

	"devbox/internal/cliui"
	"devbox/internal/resource"
)

// createConfig is the interactive workflow used by both the standalone command
// and config selection. Only the destination may be supplied by the caller;
// setup, validation, publication and partial results have one implementation.
func createConfig(m menu, s *resource.Service, input, cwd, userHome string) (resource.Owner, resource.Result, bool, error) {
	var owner resource.Owner
	var result resource.Result
	var options resource.SetupOptions
	locate := func(value string) error {
		candidate, err := s.ConfigDirectory(value, cwd, userHome)
		if err == nil {
			err = s.CheckConfigCreation(candidate)
		}
		if err == nil {
			owner = candidate
		}
		return err
	}
	if input != "" {
		if err := locate(input); err != nil {
			return owner, result, false, err
		}
	}
	proceed := false
	err := m.Run(func() (cliui.Screen, error) {
		name, selectedHarness, files := "Unset", "Unset", "None"
		if owner.Root != "" {
			name = owner.Name
		}
		if options.Harness != nil {
			selectedHarness = *options.Harness
		}
		if len(options.Artifacts) > 0 {
			labels := slices.Clone(options.Artifacts)
			for i, artifact := range labels {
				if artifact == "harness-config" {
					labels[i] = "Harness config files (" + options.ArtifactHarness + ")"
				}
			}
			files = strings.Join(labels, ", ")
		}
		blocked := ""
		if owner.Root == "" {
			blocked = "Set a config name or directory first."
		}
		return cliui.Screen{Title: "Create config", Back: "Cancel", Actions: []cliui.Action{
			{Label: "Name/location", Value: name, Run: func() (bool, error) {
				_, _, err := m.Text("Config name or directory (:back returns): ", locate)
				return false, err
			}},
			{Label: "Harness", Value: selectedHarness, Run: func() (bool, error) {
				var err error
				options.Harness, err = chooseConfigHarness(m, s.Home, options.Harness)
				return false, err
			}},
			{Label: "Optional files", Value: files, Run: func() (bool, error) {
				// The optional-files editor commits its selection with Continue;
				// Back must not mutate the creation draft through a shared slice.
				draft := options
				draft.Artifacts = slices.Clone(options.Artifacts)
				updated, accepted, err := optionalFilesMenu(m, s.Home, draft)
				if err == nil && accepted {
					options = updated
				}
				return false, err
			}},
			{Label: "Create config", BreakBefore: true, Blocked: blocked, Run: func() (bool, error) {
				proceed = true
				return true, nil
			}},
		}}, nil
	})
	if err != nil || !proceed {
		return owner, result, false, err
	}
	if err := m.Pause(); err != nil {
		return owner, result, false, err
	}
	result, err = s.CreateConfig(m.Context, owner, options)
	return owner, result, err == nil, err
}
