package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"devbox/internal/config"
	"devbox/internal/harness"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func runConfigMenu(cmd *cobra.Command, s *resource.Service, owner resource.Owner) error {
	m := menu{ctx: cmd.Context(), in: promptReader(cmd), out: cmd.OutOrStdout()}
	err := configMenu(m, s, owner)
	if errors.Is(err, io.EOF) {
		fmt.Fprintln(m.out, "\nMenu closed. Completed changes remain saved.")
		return nil
	}
	return err
}

func configMenu(m menu, s *resource.Service, owner resource.Owner) error {
	fmt.Fprintln(m.out, "Changes save immediately. Creation changes require recreate; other changes apply on the next eligible open.")
	fields := resource.ConfigFields(owner.Kind)
	for {
		source, err := s.ConfigSource(owner)
		if err != nil {
			return err
		}
		view, resolveErr := s.ShowOwner(owner)
		fmt.Fprintf(m.out, "\n%s configuration: %s\n", owner.Kind, displayCell(filepath.Join(owner.Root, "config.json")))
		fmt.Fprintf(m.out, "Values include inherited settings. Edits affect only this %s.\n", configScopeName(owner.Kind))
		if resolveErr != nil {
			fmt.Fprintf(m.out, "Effective configuration unavailable: %s\nShowing values configured here; you can still edit them.\n", displayCell(resolveErr.Error()))
		} else if owner.Kind == "project" && slices.Contains(view.Trace.Excluded, "project") {
			fmt.Fprintln(m.out, "Project overrides are excluded by global settings; edits here will not currently affect opens.")
		}
		rows := make([]configDisplayRow, len(fields))
		for i, field := range fields {
			value := source[field.Key]
			if resolveErr == nil {
				effective := view.Values[field.Key]
				if field.Key == "inherit_profile" {
					effective = true
					if raw, exists := source[field.Key]; exists {
						_ = json.Unmarshal(raw, &effective)
					}
				}
				value, _ = json.Marshal(effective)
			}
			origin := "set here"
			if _, exists := source[field.Key]; !exists {
				origin = "inherited"
			}
			rows[i] = configDisplayRow{label: configLabel(field.Key), value: menuConfigValue(value, field), origin: "(" + origin + ")"}
			if resolveErr != nil && source[field.Key] == nil {
				rows[i].value = "Unavailable"
				rows[i].origin = "(not set here)"
			}
		}
		n, err := m.chooseConfig(rows)
		if err != nil || n < 0 {
			return err
		}
		field := fields[n]
		fmt.Fprintf(m.out, "\n%s (%s)\n%s\n", configLabel(field.Key), field.Key, field.Help)
		if field.Kind == "list" || field.Kind == "extensions" {
			if err = editList(m, s, owner, field); err != nil {
				return err
			}
			continue
		}
		if raw, exists := source[field.Key]; exists {
			if err := printConfigRows(m.out, []configDisplayRow{{label: "Configured here", value: menuConfigValue(raw, field)}}, "  ", configDisplayWidth(m.out)); err != nil {
				return err
			}
		}
		actions := []string{"Edit value"}
		if _, exists := source[field.Key]; exists {
			actions = append(actions, "Reset to inherited (remove this setting)")
		}
		action, err := m.choose(configLabel(field.Key), actions, "Back")
		if err != nil {
			return err
		}
		if action < 0 {
			continue
		}
		remove := action == 1
		var value json.RawMessage
		if !remove {
			value, err = readConfigValue(m, s, field)
			if err != nil {
				return err
			}
			if value == nil {
				continue
			}
		}
		if err = applyConfigChange(m, s, owner, field, source[field.Key], value, remove); err != nil {
			return err
		}
	}
}

func configScopeName(scope string) string {
	if scope == "global" {
		return "global configuration"
	}
	return scope
}

func configLabel(key string) string {
	switch key {
	case "default_shell":
		return "Shell command"
	case "harness_args":
		return "Harness arguments"
	case "docker_args":
		return "Docker options"
	case "extra_env", "global_env":
		return "Environment variables"
	case "extra_ports":
		return "Port forwards"
	case "vscode":
		return "VS Code extensions"
	}
	text := strings.ReplaceAll(key, "_", " ")
	if text == "" {
		return "Entries"
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func configEntries(raw json.RawMessage, field resource.ConfigField) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if field.Kind == "extensions" {
		var v config.VSCode
		err := json.Unmarshal(raw, &v)
		return v.Extensions, err
	}
	var entries []string
	err := json.Unmarshal(raw, &entries)
	return entries, err
}

func menuConfigValue(raw json.RawMessage, field resource.ConfigField) any {
	if len(raw) == 0 {
		return nil
	}
	if field.Kind == "list" || field.Kind == "extensions" {
		entries, err := configEntries(raw, field)
		if err != nil {
			return "Invalid value"
		}
		if field.Sensitive {
			entries = config.RedactEnv(entries)
		}
		return entries
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "Invalid value"
	}
	return value
}

func configEntryLabel(entry string, field resource.ConfigField) string {
	if field.Sensitive {
		entry = config.RedactEnv([]string{entry})[0]
	}
	if entry == "" {
		return "(empty)"
	}
	return displayCell(entry)
}

func readConfigValue(m menu, s *resource.Service, field resource.ConfigField) (json.RawMessage, error) {
	var value any
	switch field.Kind {
	case "bool":
		n, err := m.choose("New value", []string{"Yes", "No"}, "Cancel")
		if err != nil || n < 0 {
			return nil, err
		}
		value = n == 0
	case "string":
		choices := []string{}
		switch field.Key {
		case "on_exit":
			choices = []string{"stop", "running"}
		case "harness", "default_harness":
			registry, err := harness.Enumerate(s.Home)
			if err != nil {
				return nil, err
			}
			for _, issue := range registry.Invalid {
				fmt.Fprintf(m.out, "Unavailable harness %s: %s\n", displayCell(issue.Name), displayCell(issue.Err.Error()))
			}
			for _, h := range registry.Valid {
				choices = append(choices, h.Definition.Name)
			}
		case "default_profile":
			profiles, err := s.Profiles()
			if err != nil {
				return nil, err
			}
			for _, p := range profiles {
				if p.Error == "" {
					choices = append(choices, p.Name)
				}
			}
		}
		if len(choices) > 0 {
			n, err := m.choose("New value", append(append([]string(nil), choices...), "Enter a value or host expression"), "Cancel")
			if err != nil || n < 0 {
				return nil, err
			}
			if n < len(choices) {
				value = choices[n]
			}
		}
		if value == nil {
			text, err := m.line("New value (:back cancels): ")
			if err != nil || text == ":back" {
				return nil, err
			}
			value = text
		}
	default:
		return nil, fmt.Errorf("unsupported config control")
	}
	return json.Marshal(value)
}

func listItemName(key string) string {
	switch key {
	case "extra_mounts":
		return "mount"
	case "extra_ports":
		return "port forward"
	case "extra_env", "global_env":
		return "environment variable"
	case "vscode":
		return "extension"
	case "docker_args":
		return "option"
	default:
		return "argument"
	}
}

// A failed operation stays visible without closing the editor. Both callers
// reload source before the next operation, including after a conflict.
func applyConfigChange(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField, expected, value json.RawMessage, remove bool) error {
	if err := s.SetConfigField(m.ctx, owner, field.Key, expected, value, remove); err != nil {
		if m.ctx.Err() != nil {
			return m.ctx.Err()
		}
		_, writeErr := fmt.Fprintf(m.out, "Not saved: %s\n", displayCell(err.Error()))
		return writeErr
	}
	_, err := fmt.Fprintf(m.out, "Saved %s.\n", configLabel(field.Key))
	return err
}

func editList(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField) error {
	item := listItemName(field.Key)
	for {
		source, err := s.ConfigSource(owner)
		if err != nil {
			return err
		}
		entries, err := configEntries(source[field.Key], field)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Fprintf(m.out, "\nNo %s configured here.\n", strings.ToLower(configLabel(field.Key)))
		} else {
			fmt.Fprintf(m.out, "\n%s configured here:\n", configLabel(field.Key))
		}
		labels := make([]string, len(entries))
		for i, entry := range entries {
			labels[i] = configEntryLabel(entry, field)
			prefix := fmt.Sprintf("  [%d] ", i+1)
			if err := writeConfigLine(m.out, prefix, labels[i], strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out)); err != nil {
				return err
			}
		}
		actions := []string{"add"}
		choices := []string{"Add " + item}
		if len(entries) > 0 {
			actions = append(actions, "edit", "remove")
			choices = append(choices, "Edit "+item, "Remove "+item)
		}
		if _, configured := source[field.Key]; configured {
			actions = append(actions, "reset")
			choices = append(choices, "Reset to inherited (remove this setting)")
		}
		n, err := m.choose("What would you like to do?", choices, "Back")
		if err != nil || n < 0 {
			return err
		}
		switch actions[n] {
		case "add", "edit":
			index := len(entries)
			if actions[n] == "edit" {
				index, err = m.choose("Select "+item, labels, "Back")
				if err != nil {
					return err
				}
				if index < 0 {
					continue
				}
			}
			text, err := m.line("New " + item + " (:back cancels): ")
			if err != nil {
				return err
			}
			if text == ":back" {
				continue
			}
			if strings.ContainsRune(text, '\x00') {
				fmt.Fprintln(m.out, "Entries cannot contain NUL.")
				continue
			}
			if actions[n] == "add" {
				entries = append(entries, text)
			} else {
				entries[index] = text
			}
		case "remove":
			index, err := m.choose("Remove "+item, labels, "Back")
			if err != nil {
				return err
			}
			if index < 0 {
				continue
			}
			entries = append(entries[:index], entries[index+1:]...)
		}
		remove := actions[n] == "reset"
		var value json.RawMessage
		if !remove {
			var input any = entries
			if field.Kind == "extensions" {
				input = config.VSCode{Extensions: entries}
			}
			value, err = json.Marshal(input)
			if err != nil {
				return err
			}
		}
		if err = applyConfigChange(m, s, owner, field, source[field.Key], value, remove); err != nil {
			return err
		}
	}
}
