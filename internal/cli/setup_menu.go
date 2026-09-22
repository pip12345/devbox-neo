package cli

import (
	"fmt"
	"slices"
	"strings"

	"devbox/internal/harness"
	"devbox/internal/resource"
)

func (m menu) selectedChoice(title string, choices []string, selected int, summary, back string) (int, error) {
	if err := writeMenuTitle(m.out, title); err != nil {
		return -1, err
	}
	paint := terminalColors(m.out)
	if summary == "" {
		summary = "None"
		if selected >= 0 && selected < len(choices) {
			summary = choices[selected]
		}
	}
	if err := writeStyledConfigLine(m.out, "Current selection: ", displayCell(summary), "  ", configDisplayWidth(m.out), paint.strong); err != nil {
		return -1, err
	}
	fmt.Fprintln(m.out)
	for i, choice := range choices {
		text := displayCell(choice)
		var style func(string) string
		if i == selected {
			text += " (selected)"
			style = func(line string) string {
				return strings.ReplaceAll(paint.strong(line), "(selected)", paint.green("(selected)"))
			}
		}
		prefix := menuPrefix(i + 1)
		if err := writeStyledConfigLine(m.out, prefix, text, strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out), style); err != nil {
			return -1, err
		}
	}
	return m.readChoice(len(choices), back)
}

func availableHarnesses(m menu, home string) ([]string, error) {
	registry, err := harness.Enumerate(home)
	if err != nil {
		return nil, err
	}
	for _, issue := range registry.Invalid {
		fmt.Fprintf(m.out, "Unavailable harness %s: %s\n", displayCell(issue.Name), displayCell(issue.Err.Error()))
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
		choice, err := m.selectedChoice("Select a harness", append(slices.Clone(names), "Leave unset"), current, summary, "Cancel")
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
	for {
		if err := writeMenuTitle(m.out, "Choose optional files"); err != nil {
			return options, false, err
		}
		summary := []string{}
		for i, artifact := range resource.SetupArtifacts {
			if slices.Contains(options.Artifacts, artifact) {
				label := labels[i]
				if artifact == "harness-config" {
					label += " (" + options.ArtifactHarness + ")"
				}
				summary = append(summary, label)
			}
		}
		text := "None"
		if len(summary) > 0 {
			text = strings.Join(summary, ", ")
		}
		paint := terminalColors(m.out)
		if err := writeStyledConfigLine(m.out, "Current selection: ", displayCell(text), "  ", configDisplayWidth(m.out), paint.strong); err != nil {
			return options, false, err
		}
		fmt.Fprintln(m.out)
		for i, artifact := range resource.SetupArtifacts {
			label := "  " + labels[i]
			var style func(string) string
			if slices.Contains(options.Artifacts, artifact) {
				label = "✓ " + labels[i]
				if artifact == "harness-config" {
					label += " (" + options.ArtifactHarness + ")"
				}
				style = func(line string) string { return strings.ReplaceAll(paint.strong(line), "✓", paint.green("✓")) }
			}
			prefix := menuPrefix(i + 1)
			if err := writeStyledConfigLine(m.out, prefix, displayCell(label), strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out), style); err != nil {
				return options, false, err
			}
		}
		fmt.Fprintf(m.out, "\n%sContinue\n", menuPrefix(len(labels)+1))
		choice, err := m.readChoice(len(labels)+1, "Back")
		if err != nil || choice < 0 {
			return options, false, err
		}
		if choice == len(labels) {
			if !slices.Contains(options.Artifacts, "harness-config") {
				options.ArtifactHarness = ""
			}
			return options, true, nil
		}
		artifact := resource.SetupArtifacts[choice]
		if index := slices.Index(options.Artifacts, artifact); index >= 0 {
			options.Artifacts = slices.Delete(options.Artifacts, index, index+1)
			continue
		}
		if artifact == "harness-config" {
			names, err := availableHarnesses(m, home)
			if err != nil {
				return options, false, err
			}
			if len(names) == 0 {
				fmt.Fprintln(m.out, "No harness definitions are available for file generation.")
				continue
			}
			target := options.ArtifactHarness
			if target == "" && options.Harness != nil {
				target = *options.Harness
			}
			index, err := m.selectedChoice("Choose which harness's config files to add", names, slices.Index(names, target), "", "Back")
			if err != nil {
				return options, false, err
			}
			if index < 0 {
				continue
			}
			options.ArtifactHarness = names[index]
		}
		options.Artifacts = append(options.Artifacts, artifact)
	}
}
