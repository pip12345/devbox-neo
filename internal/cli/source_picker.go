package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/cliui"
	"devbox/internal/config"
	"devbox/internal/resource"
)

type sourcePicker struct {
	menu
	home, workspace, cwd, userHome string
}

func newSourcePicker(m menu, home, workspace string) (sourcePicker, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return sourcePicker{}, err
	}
	userHome, err := os.UserHomeDir()
	return sourcePicker{menu: m, home: home, workspace: workspace, cwd: cwd, userHome: userHome}, err
}
func (p sourcePicker) capture(input string) (config.Reference, error) {
	return config.CaptureReference(p.home, p.workspace, p.cwd, p.userHome, input)
}
func (p sourcePicker) existing(input string) (config.Reference, error) {
	reference, err := p.capture(input)
	if err != nil {
		return reference, err
	}
	sources, err := config.ResolveReferences(p.workspace, []config.Reference{reference})
	if err != nil {
		return reference, err
	}
	info, err := os.Lstat(filepath.Join(sources[0].Path, "config.json"))
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("config.json must be a regular file")
	}
	return reference, err
}
func (p sourcePicker) create() (config.Reference, bool, error) {
	owner, result, created, err := createConfig(p.menu, &resource.Service{Home: p.home}, "", p.cwd, p.userHome)
	for _, warning := range result.Warnings {
		p.Notice("Warning: " + displayCell(warning))
	}
	if err != nil {
		for _, path := range result.Created {
			p.Notice("Created " + displayCell(path))
		}
		// Publication may be partial. Keep the service's actionable error intact;
		// do not delete files or retry a create that has already claimed config.json.
		return config.Reference{}, false, err
	}
	if !created {
		return config.Reference{}, false, nil
	}
	p.Notice("Created config " + displayCell(owner.Name) + ". This config is saved independently of the session.")
	reference, err := p.capture(owner.Name)
	return reference, err == nil, err
}

func (p sourcePicker) choose(current *config.Reference, back string) (config.Reference, bool, error) {
	var result config.Reference
	chosen := false
	accept := func(input string) (bool, error) {
		reference, err := p.existing(input)
		if err != nil {
			return false, p.report(err)
		}
		result, chosen = reference, true
		return true, nil
	}
	err := p.Run(func() (cliui.Screen, error) {
		entries, err := os.ReadDir(filepath.Join(p.home, "configs"))
		if err != nil && !os.IsNotExist(err) {
			return cliui.Screen{}, err
		}
		var names []string
		for _, entry := range entries {
			if info, err := os.Lstat(filepath.Join(p.home, "configs", entry.Name(), "config.json")); err == nil && info.Mode().IsRegular() {
				names = append(names, entry.Name())
			}
		}
		nameWidth := len("NAME")
		for _, name := range names {
			nameWidth = max(nameWidth, len(displayCell(name)))
		}
		var actions []cliui.Action
		for _, name := range names {
			selected := false
			if current != nil {
				candidate, err := p.capture(name)
				if err == nil {
					a, aErr := candidate.Expand(p.workspace)
					b, bErr := current.Expand(p.workspace)
					if aErr == nil && bErr == nil {
						ca, ea := config.CanonicalPath(a.Path)
						cb, eb := config.CanonicalPath(b.Path)
						selected = ea == nil && eb == nil && ca == cb
					}
				}
			}
			actions = append(actions, cliui.Action{Label: fmt.Sprintf("%-*s  %-5s  %s", nameWidth, displayCell(name), "fixed", displayCell(filepath.Join(p.home, "configs", name))), Selected: selected, Run: func() (bool, error) { return accept(name) }})
		}
		actions = append(actions, cliui.Action{Label: "Enter a directory path", BreakBefore: len(names) > 0, Run: func() (bool, error) {
			input, accepted, err := p.Text("Config directory (:back cancels): ", nil)
			if err != nil || !accepted {
				return false, err
			}
			return accept(input)
		}})
		actions = append(actions, cliui.Action{Label: "Create and add config", Hidden: len(names) > 0, Run: func() (bool, error) {
			reference, created, err := p.create()
			if errors.Is(err, io.EOF) {
				return false, err
			}
			if err != nil {
				return false, p.report(err)
			}
			if !created {
				return false, nil
			}
			result, chosen = reference, true
			return true, nil
		}})
		return cliui.Screen{Title: "Select an existing config", Back: back, Actions: actions, Body: func(out io.Writer) error {
			if current != nil {
				if err := writeStyledConfigLine(out, "Current selection: ", displayCell(current.Label)+" ("+current.Kind+")", "  ", configDisplayWidth(out), terminalColors(out).strong); err != nil {
					return err
				}
			}
			if len(names) == 0 {
				return writeMenuHint(out, "No named configs found. Create one here, or enter an existing directory path.")
			}
			fmt.Fprintln(out)
			prefix := strings.Repeat(" ", len(menuPrefix(1)))
			return writeConfigLine(out, prefix, fmt.Sprintf("%-*s  %-5s  PATH", nameWidth, "NAME", "TYPE"), prefix, configDisplayWidth(out))
		}}, nil
	})
	return result, chosen, err
}

func showSourceChain(m menu, home, workspace string, sources []config.Reference) error {
	fmt.Fprintln(m.Out, "\nConfigs, in order:")
	if len(sources) == 0 {
		fmt.Fprintln(m.Out, "   None")
	}
	for i, reference := range sources {
		source, err := reference.Expand(workspace)
		if err != nil {
			return err
		}
		prefix := fmt.Sprintf("   %d. ", i+1)
		text := fmt.Sprintf("%-12s %s", displayCell(reference.Label), displayCell(source.Path))
		if err := writeConfigLine(m.Out, prefix, text, strings.Repeat(" ", len(prefix)), configDisplayWidth(m.Out)); err != nil {
			return err
		}
		if _, err := config.ReadLayer(filepath.Join(source.Path, "config.json"), config.Snapshot()); err != nil {
			writeMenuHint(m.Out, "      Error: "+displayCell(err.Error()))
			action, reason := "edit", "Edit this config"
			if os.IsNotExist(err) {
				action, reason = "create", "Create this config separately"
			}
			if hintErr := m.commandHint(home, reason, "config", action, source.Path); hintErr != nil {
				return hintErr
			}
		}
	}
	return nil
}

// Chain controls do not own persistence. Creation replaces its draft; editing
// saves through UpdateSources with its existing identity/conflict checks.
func (p sourcePicker) chainActions(sources []config.Reference, apply func([]config.Reference) error) []cliui.Action {
	save := func(updated []config.Reference) (bool, error) {
		if err := config.ValidateReferenceChain(p.workspace, updated); err != nil {
			return false, p.report(err)
		}
		return false, apply(updated)
	}
	add := func(selectConfig func() (config.Reference, bool, error)) func() (bool, error) {
		return func() (bool, error) {
			reference, selected, err := selectConfig()
			if errors.Is(err, io.EOF) {
				return false, err
			}
			if err != nil {
				return false, p.report(err)
			}
			if !selected {
				return false, nil
			}
			return save(append(slices.Clone(sources), reference))
		}
	}
	selectIndex := func() (int, error) {
		labels := make([]string, len(sources))
		for i, reference := range sources {
			labels[i] = reference.Label + " (" + reference.Kind + ")"
		}
		return p.Select("Select a config", labels, "Back")
	}
	return []cliui.Action{
		{Label: "Add existing config", Run: add(func() (config.Reference, bool, error) { return p.choose(nil, "Back") })},
		{Label: "Create config", Run: add(p.create)},
		{Label: "Replace config", Hidden: len(sources) == 0, Run: func() (bool, error) {
			index, err := selectIndex()
			if err != nil || index < 0 {
				return false, err
			}
			reference, selected, err := p.choose(&sources[index], "Back")
			if err != nil || !selected {
				return false, err
			}
			updated := slices.Clone(sources)
			updated[index] = reference
			return save(updated)
		}},
		{Label: "Remove config", Hidden: len(sources) == 0, Run: func() (bool, error) {
			index, err := selectIndex()
			if err != nil || index < 0 {
				return false, err
			}
			return save(slices.Delete(slices.Clone(sources), index, index+1))
		}},
		{Label: "Reorder configs", Hidden: len(sources) < 2, Run: func() (bool, error) {
			index, err := selectIndex()
			if err != nil || index < 0 {
				return false, err
			}
			labels := make([]string, len(sources))
			for i := range labels {
				labels[i] = fmt.Sprintf("Position %d", i+1)
			}
			position, err := p.SelectCurrent("Choose the new position", labels, index, "", "Back")
			if err != nil || position < 0 {
				return false, err
			}
			updated := slices.Delete(slices.Clone(sources), index, index+1)
			return save(slices.Insert(updated, position, sources[index]))
		}},
	}
}
