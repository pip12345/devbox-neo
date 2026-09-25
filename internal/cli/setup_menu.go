package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"devbox/internal/cliui"
	"devbox/internal/harness"
	"devbox/internal/resource"
)

func availableHarnesses(m menu, home string) ([]string, error) {
	registry, err := harness.Enumerate(home)
	if err != nil {
		return nil, err
	}
	for _, issue := range registry.Invalid {
		m.Notice(fmt.Sprintf("Unavailable harness %s: %s", displayCell(issue.Name), displayCell(issue.Err.Error())))
	}
	var names []string
	for _, h := range registry.Valid {
		names = append(names, h.Definition.Name)
	}
	return names, nil
}

func configCreationMenu(m menu, home string) (resource.SetupOptions, bool, error) {
	var options resource.SetupOptions
	names, err := availableHarnesses(m, home)
	if err != nil {
		return options, false, err
	}
	for {
		current, summary := len(names), "Unset"
		if options.Harness != nil {
			current, summary = slices.Index(names, *options.Harness), *options.Harness
		}
		choice, err := m.SelectCurrent("Select a harness", append(slices.Clone(names), "Leave unset"), current, summary, "Cancel")
		if err != nil || choice < 0 {
			return options, false, err
		}
		options.Harness = nil
		if choice < len(names) {
			value := names[choice]
			options.Harness = &value
		}
		var proceed bool
		options, proceed, err = optionalFilesMenu(m, home, options)
		if err != nil || proceed {
			return options, proceed, err
		}
	}
}

func optionalFilesMenu(m menu, home string, options resource.SetupOptions) (resource.SetupOptions, bool, error) {
	labels := []string{"Harness config files", "setup.sh", "before-open.sh", "Dockerfile"}
	proceed := false
	err := m.Run(func() (cliui.Screen, error) {
		var summary []string
		actions := make([]cliui.Action, 0, len(labels)+1)
		for i, artifact := range resource.SetupArtifacts {
			label := labels[i]
			selected := slices.Contains(options.Artifacts, artifact)
			if selected && artifact == "harness-config" {
				label += " (" + options.ArtifactHarness + ")"
			}
			if selected {
				summary = append(summary, label)
			}
			actions = append(actions, cliui.Action{Label: label, Checked: &selected, Run: func() (bool, error) {
				if index := slices.Index(options.Artifacts, artifact); index >= 0 {
					options.Artifacts = slices.Delete(options.Artifacts, index, index+1)
					return false, nil
				}
				if artifact == "harness-config" {
					names, err := availableHarnesses(m, home)
					if err != nil {
						return false, err
					}
					if len(names) == 0 {
						m.Notice("No harness definitions are available for file generation.")
						return false, nil
					}
					target := options.ArtifactHarness
					if target == "" && options.Harness != nil {
						target = *options.Harness
					}
					index, err := m.SelectCurrent("Choose which harness's config files to add", names, slices.Index(names, target), "", "Back")
					if err != nil || index < 0 {
						return false, err
					}
					options.ArtifactHarness = names[index]
				}
				options.Artifacts = append(options.Artifacts, artifact)
				return false, nil
			}})
		}
		actions = append(actions, cliui.Action{Label: "Continue", BreakBefore: true, Run: func() (bool, error) {
			if !slices.Contains(options.Artifacts, "harness-config") {
				options.ArtifactHarness = ""
			}
			proceed = true
			return true, nil
		}})
		return cliui.Screen{Title: "Choose optional files", Back: "Back", Actions: actions, Body: func(out io.Writer) error {
			text := "None"
			if len(summary) > 0 {
				text = strings.Join(summary, ", ")
			}
			if err := writeStyledConfigLine(out, "Current selection: ", displayCell(text), "  ", configDisplayWidth(out), terminalColors(out).strong); err != nil {
				return err
			}
			_, err := fmt.Fprintln(out)
			return err
		}}, nil
	})
	return options, proceed, err
}
