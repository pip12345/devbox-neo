package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/config"
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

func (p sourcePicker) choose(current *config.Reference) (config.Reference, bool, error) {
	entries, err := os.ReadDir(filepath.Join(p.home, "configs"))
	if err != nil && !os.IsNotExist(err) {
		return config.Reference{}, false, err
	}
	var names []string
	for _, entry := range entries {
		path := filepath.Join(p.home, "configs", entry.Name(), "config.json")
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	for {
		if len(names) == 0 {
			writeMenuHint(p.out, "No reusable configs found. Create one separately, or enter an existing directory path.")
			if err := p.commandHint(p.home, "Create a reusable config", "config", "create", "base"); err != nil {
				return config.Reference{}, false, err
			}
		}
		choices := append(slices.Clone(names), "Enter a directory path")
		selected := -1
		if current != nil {
			for i, name := range names {
				candidate, _ := p.capture(name)
				a, _ := candidate.Expand(p.workspace)
				b, _ := current.Expand(p.workspace)
				if ca, err := config.CanonicalPath(a.Path); err == nil {
					if cb, err := config.CanonicalPath(b.Path); err == nil && ca == cb {
						selected = i
					}
				}
			}
		}
		var choice int
		if current == nil {
			choice, err = p.menu.choose("Select a config source", choices, "Back")
		} else {
			choice, err = p.selectedChoice("Replace config source", choices, selected, current.Label+" ("+current.Kind+")", "Back")
		}
		if err != nil || choice < 0 {
			return config.Reference{}, false, err
		}
		input := ""
		if choice < len(names) {
			input = names[choice]
		} else {
			input, err = p.line("Config directory (:back cancels): ")
			if err != nil || input == ":back" {
				return config.Reference{}, false, err
			}
		}
		reference, err := p.capture(input)
		if err == nil {
			var sources []config.Source
			sources, err = config.ResolveReferences(p.workspace, []config.Reference{reference})
			if err == nil {
				info, statErr := os.Lstat(filepath.Join(sources[0].Path, "config.json"))
				err = statErr
				if err == nil && !info.Mode().IsRegular() {
					err = fmt.Errorf("config.json must be a regular file")
				}
			}
		}
		if err != nil {
			fmt.Fprintf(p.out, "Error: %s\n", displayCell(err.Error()))
			if os.IsNotExist(err) {
				if hintErr := p.commandHint(p.home, "Create this config separately", "config", "create", input); hintErr != nil {
					return config.Reference{}, false, hintErr
				}
			}
			continue
		}
		return reference, true, nil
	}
}

func showSourceChain(m menu, home, workspace string, sources []config.Reference) error {
	fmt.Fprintln(m.out, "\nConfig sources, in order:")
	if len(sources) == 0 {
		fmt.Fprintln(m.out, "   None")
	}
	for i, reference := range sources {
		source, err := reference.Expand(workspace)
		if err != nil {
			return err
		}
		prefix := fmt.Sprintf("   %d. ", i+1)
		text := fmt.Sprintf("%-12s %-9s %s", displayCell(reference.Label), reference.Kind, displayCell(source.Path))
		if err := writeConfigLine(m.out, prefix, text, strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out)); err != nil {
			return err
		}
		if _, err := config.ReadLayer(filepath.Join(source.Path, "config.json"), config.Snapshot()); err != nil {
			writeMenuHint(m.out, "      Error: "+displayCell(err.Error()))
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

func editSourceChain(p sourcePicker, sources []config.Reference, action string) ([]config.Reference, bool, error) {
	updated := slices.Clone(sources)
	index := -1
	if action != "Add source" {
		choices := make([]string, len(sources))
		for i, reference := range sources {
			choices[i] = reference.Label + " (" + reference.Kind + ")"
		}
		var err error
		index, err = p.menu.choose("Select a source", choices, "Back")
		if err != nil || index < 0 {
			return sources, false, err
		}
	}
	switch action {
	case "Add source", "Replace source":
		var current *config.Reference
		if index >= 0 {
			current = &sources[index]
		}
		reference, chosen, err := p.choose(current)
		if err != nil || !chosen {
			return sources, false, err
		}
		if index < 0 {
			updated = append(updated, reference)
		} else {
			updated[index] = reference
		}
	case "Remove source":
		updated = slices.Delete(updated, index, index+1)
	case "Reorder sources":
		choices := make([]string, len(sources))
		for i := range choices {
			choices[i] = fmt.Sprintf("Position %d", i+1)
		}
		position, err := p.selectedChoice("Choose the new position", choices, index, "", "Back")
		if err != nil || position < 0 {
			return sources, false, err
		}
		reference := updated[index]
		updated = slices.Delete(updated, index, index+1)
		updated = slices.Insert(updated, position, reference)
	}
	if err := config.ValidateReferenceChain(p.workspace, updated); err != nil {
		return sources, false, err
	}
	return updated, true, nil
}
