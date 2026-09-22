package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	fields := resource.ConfigFields()
	for {
		source, err := s.ConfigSource(owner)
		if err != nil {
			return err
		}
		view, resolveErr := s.ShowOwner(owner)
		if err := writeMenuTitle(m.out, configMenuTitle(owner)); err != nil {
			return err
		}
		if resolveErr != nil {
			fmt.Fprintf(m.out, "Effective configuration unavailable: %s\nShowing values configured here; you can still edit them.\n", displayCell(resolveErr.Error()))

		}
		rows := make([]configDisplayRow, len(fields))
		for i, field := range fields {
			value := source[field.Key]
			origin := owner.Name
			var entrySources []string
			if resolveErr == nil {
				key := field.Key
				if field.Kind == "extensions" {
					key = "vscode.extensions"
				}
				origin = configSourceLabel(view.Trace.Sources[key])
				entrySources = view.Trace.EntrySources[key]
				effective := view.Values[field.Key]
				value, _ = json.Marshal(effective)
			}
			rows[i] = configDisplayRow{label: configLabel(field.Key), value: menuConfigValue(value, field), origin: origin, command: field.Key == "shell"}
			if resolveErr != nil {
				_, items := configDisplayParts(rows[i].value)
				for range items {
					entrySources = append(entrySources, owner.Name)
				}
			}
			rows[i].entryOrigins = configEntryOrigins(entrySources)
			if resolveErr != nil && source[field.Key] == nil {
				rows[i].value = "Unavailable"
				rows[i].origin = "unknown"
			}
		}
		n, err := m.chooseConfig(rows, "Add optional files")
		if err != nil || n < 0 {
			return err
		}
		if n == len(fields) {
			var configured *string
			if raw := source["harness"]; raw != nil {
				_ = json.Unmarshal(raw, &configured)
			}
			options, proceed, err := optionalFilesMenu(m, s.Home, resource.SetupOptions{Harness: configured})
			if err != nil {
				return err
			}
			if !proceed || len(options.Artifacts) == 0 {
				continue
			}
			options.Harness = nil
			result, err := s.EditConfig(m.ctx, owner, options)
			for _, path := range result.Created {
				fmt.Fprintf(m.out, "Created %s\n", displayCell(path))
			}
			for _, path := range result.Skipped {
				fmt.Fprintf(m.out, "Kept existing %s\n", displayCell(path))
			}
			if err != nil {
				fmt.Fprintf(m.out, "Error: %s\n", displayCell(err.Error()))
			}
			continue
		}
		field := fields[n]
		if err := writeMenuTitle(m.out, fmt.Sprintf("%s (%s)", configLabel(field.Key), field.Key)); err != nil {
			return err
		}
		if err := writeMenuHint(m.out, field.Help); err != nil {
			return err
		}
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
			actions = append(actions, "Remove this setting")
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
			value, err = readConfigValue(m, s, field, source[field.Key])
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

func configMenuTitle(owner resource.Owner) string {
	return "Config · " + displayCell(owner.Name)
}

func configLabel(key string) string {
	switch key {
	case "shell":
		return "Shell command"
	case "harness_args":
		return "Harness arguments"
	case "docker_args":
		return "Docker options"
	case "env":
		return "Environment variables"
	case "ports":
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

func readConfigValue(m menu, s *resource.Service, field resource.ConfigField, current json.RawMessage) (json.RawMessage, error) {
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
		case "harness":
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
		}
		if len(choices) > 0 {
			var selected string
			_ = json.Unmarshal(current, &selected)
			n, err := m.selectedChoice("New value", append(append([]string(nil), choices...), "Enter a value or host expression"), slices.Index(choices, selected), selected, "Cancel")
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
	case "mounts":
		return "mount"
	case "ports":
		return "port forward"
	case "env":
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
			fmt.Fprintln(m.out)
			if err := writeMenuHint(m.out, fmt.Sprintf("No %s configured here.", strings.ToLower(configLabel(field.Key)))); err != nil {
				return err
			}
		} else if err := writeMenuTitle(m.out, configLabel(field.Key)+" configured here:"); err != nil {
			return err
		}
		labels := make([]string, len(entries))
		for i, entry := range entries {
			labels[i] = configEntryLabel(entry, field)
			prefix := menuPrefix(i + 1)
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
			choices = append(choices, "Remove this setting")
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
