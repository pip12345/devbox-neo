package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/harness"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func runConfigMenu(cmd *cobra.Command, s *resource.Service, owner resource.Owner) (err error) {
	m := newMenu(cmd)
	changed := false
	defer func() {
		err = errors.Join(err, m.Finish())
		if changed {
			_, receiptErr := fmt.Fprint(cmd.OutOrStdout(), configSaveReceipt(cmd, s.Home, owner.Name))
			err = errors.Join(err, receiptErr)
		}
	}()
	err = configMenu(m, s, owner, &changed, "Exit")
	if errors.Is(err, io.EOF) {
		fmt.Fprintln(m.Out, "\nMenu closed. Completed changes remain saved.")
		return nil
	}
	return err
}

func configSaveReceipt(cmd *cobra.Command, home, name string) string {
	steps := scopedSteps(cmd, []commanderror.Step{commanderror.Next("Review pending changes", "status")}, home)
	return fmt.Sprintf("Config changes saved: %s\n%s", displayCell(name), stepsText(steps))
}

func configMenu(m menu, s *resource.Service, owner resource.Owner, changed *bool, back string) error {
	fields := resource.ConfigFields()
	return m.Run(func() (cliui.Screen, error) {
		source, readErr := s.ConfigSource(owner)
		available := fields
		if readErr != nil {
			available = nil
		}
		view, resolveErr := s.ShowOwner(owner)
		rows := make([]configDisplayRow, len(available))
		actions := make([]cliui.Action, 0, len(available)+2)
		for i, field := range available {
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
				value, _ = json.Marshal(view.Values[field.Key])
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
			valueText := configFieldText(rows[i].value)
			actions = append(actions, cliui.Action{Label: configLabel(field.Key), Value: strings.ReplaceAll(valueText, "\n", ", "), Fields: configActionFields(rows[i]), Run: func() (bool, error) {
				return false, editConfigField(m, s, owner, field, source, changed)
			}})
		}
		actions = append(actions, cliui.Action{Label: "Add optional files", Hidden: readErr != nil, BreakBefore: true, Run: func() (bool, error) {
			var configured *string
			if raw := source["harness"]; raw != nil {
				_ = json.Unmarshal(raw, &configured)
			}
			options, proceed, err := optionalFilesMenu(m, s.Home, resource.SetupOptions{Harness: configured})
			if err != nil || !proceed || len(options.Artifacts) == 0 {
				return false, err
			}
			options.Harness = nil
			result, err := s.EditConfig(m.Context, owner, options)
			if len(result.Created) > 0 || len(result.Updated) > 0 {
				*changed = true
			}
			for _, path := range result.Created {
				m.Notice("Created " + displayCell(path))
			}
			for _, path := range result.Skipped {
				m.Notice("Kept existing " + displayCell(path))
			}
			return false, m.report(err)
		}})
		if isNamedConfig(owner.Name) {
			actions = append(actions, cliui.Action{Label: "Delete config", Description: "Remove this named directory and all files; refuses saved-session users", Danger: true, Run: func() (bool, error) {
				result, cancelled, err := deleteConfigWorkflow(m, s, owner.Name, false)
				if err != nil {
					return false, m.report(err)
				}
				if cancelled {
					return false, nil
				}
				*changed = false
				m.Receipt("Deleted config " + displayCell(owner.Name) + " (" + displayCell(result.Path) + ").")
				return true, nil
			}})
		}
		context := []cliui.Field{{Label: "Directory", Value: owner.Root}}
		users, usageErr := s.ConfigUsers(m.Context, owner)
		if len(users) > 0 {
			names := make([]string, len(users))
			for i, user := range users {
				names[i] = user.Session
			}
			context = append(context, cliui.Field{Label: "Used by saved sessions", Values: names})
		}
		if usageErr != nil {
			context = append(context, cliui.Field{Label: "Shared-use report is incomplete", Value: usageErr.Error(), Warning: true})
		}
		if readErr != nil {
			context = append(context, cliui.Field{Label: "Settings unavailable", Value: readErr.Error(), Warning: true})
		} else if resolveErr != nil {
			context = append(context, cliui.Field{Label: "Effective configuration unavailable", Value: resolveErr.Error() + "\nShowing values configured here; you can still edit them.", Warning: true})
		}
		return cliui.Screen{Title: configMenuTitle(owner), Back: back, Actions: actions, Fields: context,
			Rows: func(out io.Writer, actions []cliui.Action) error { return printConfigActions(out, rows, actions) }}, nil
	})
}

func editConfigField(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField, source map[string]json.RawMessage, changed *bool) error {
	if field.Kind == "list" || field.Kind == "extensions" {
		return editList(m, s, owner, field, changed)
	}
	actions := []cliui.Action{{Label: "Edit value"}, {Label: "Remove this setting", Hidden: source[field.Key] == nil}}
	selected, err := m.Choose(cliui.Screen{Title: fmt.Sprintf("%s (%s)", configLabel(field.Key), field.Key), Back: "Back", Actions: actions, Body: func(out io.Writer) error {
		if err := writeMenuHint(out, field.Help); err != nil {
			return err
		}
		if raw, exists := source[field.Key]; exists {
			return printConfigRows(out, []configDisplayRow{{label: "Configured here", value: menuConfigValue(raw, field)}}, "  ", configDisplayWidth(out))
		}
		return nil
	}})
	if err != nil || selected < 0 {
		return err
	}
	if selected == 1 {
		if err := applyConfigChange(m, s, owner, field, source[field.Key], nil, true, changed); err != nil {
			_, err = configEditFailure(m, err)
			return err
		}
		return nil
	}
	pending := source[field.Key]
	for {
		value, err := readConfigValue(m, s, field, pending)
		if err != nil || value == nil {
			return err
		}
		if err := applyConfigChange(m, s, owner, field, source[field.Key], value, false, changed); err != nil {
			retry, err := configEditFailure(m, err)
			if err != nil || !retry {
				return err
			}
			pending = value
			continue
		}
		return nil
	}
}

func configActionFields(row configDisplayRow) []cliui.Field {
	entries, list := row.value.([]string)
	if !list || len(entries) == 0 {
		return []cliui.Field{{Label: "Value", Value: configFieldText(row.value)}, {Label: "From", Value: row.origin}}
	}
	var fields []cliui.Field
	for i, entry := range entries {
		origin := row.origin
		if i < len(row.entryOrigins) {
			origin = row.entryOrigins[i]
		}
		fields = append(fields, cliui.Field{Label: fmt.Sprintf("%d", i+1), Value: entry}, cliui.Field{Label: "From", Value: origin})
	}
	return fields
}

func configFieldText(value any) string {
	if value == nil {
		return "Unset"
	}
	if entries, ok := value.([]string); ok {
		if len(entries) == 0 {
			return "None"
		}
		return strings.Join(entries, "\n")
	}
	return fmt.Sprint(value)
}

func configMenuTitle(owner resource.Owner) string { return "Config · " + displayCell(owner.Name) }
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
		n, err := m.Select("New value", []string{"Yes", "No"}, "Cancel")
		if err != nil || n < 0 {
			return nil, err
		}
		value = n == 0
	case "string":
		var choices []string
		if field.Key == "harness" {
			registry, err := harness.Enumerate(s.Home)
			if err != nil {
				return nil, err
			}
			for _, issue := range registry.Invalid {
				m.Notice(fmt.Sprintf("Unavailable harness %s: %s", displayCell(issue.Name), displayCell(issue.Err.Error())))
			}
			for _, h := range registry.Valid {
				choices = append(choices, h.Definition.Name)
			}
		}
		if len(choices) > 0 {
			var selected string
			_ = json.Unmarshal(current, &selected)
			n, err := m.SelectCurrent("New value", append(slices.Clone(choices), "Enter a value or host expression"), slices.Index(choices, selected), selected, "Cancel")
			if err != nil || n < 0 {
				return nil, err
			}
			if n < len(choices) {
				value = choices[n]
			}
		}
		if value == nil {
			var initial string
			_ = json.Unmarshal(current, &initial)
			text, accepted, err := m.Text(cliui.TextRequest{Prompt: "New value (:back cancels): ", Initial: initial, Sensitive: field.Sensitive})
			if err != nil || !accepted {
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

// A conflict invalidates the snapshot behind the pending edit; return to the
// refreshed field rather than replaying it over another writer's value. Other
// failures retain the pending input, but only another submission may retry it.
func configEditFailure(m menu, err error) (retry bool, fatal error) {
	if m.Context.Err() != nil {
		return false, m.Context.Err()
	}
	m.Notice("Not saved: " + displayCell(err.Error()))
	return !errors.Is(err, resource.ErrConfigChanged), nil
}

// Persistence returns before the next prompt, so no owner lock spans input.
func applyConfigChange(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField, expected, value json.RawMessage, remove bool, changed *bool) error {
	if err := s.SetConfigField(m.Context, owner, field.Key, expected, value, remove); err != nil {
		return err
	}
	if !bytes.Equal(expected, value) {
		*changed = true
	}
	m.Notice("Saved " + configLabel(field.Key) + ".")
	return nil
}

func editList(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField, changed *bool) error {
	item := listItemName(field.Key)
	return m.Run(func() (cliui.Screen, error) {
		source, err := s.ConfigSource(owner)
		if err != nil {
			return cliui.Screen{}, err
		}
		entries, err := configEntries(source[field.Key], field)
		if err != nil {
			return cliui.Screen{}, err
		}
		labels := make([]string, len(entries))
		for i, entry := range entries {
			labels[i] = configEntryLabel(entry, field)
		}
		persist := func(updated []string, remove bool) error {
			var value json.RawMessage
			if !remove {
				var input any = updated
				if field.Kind == "extensions" {
					input = config.VSCode{Extensions: updated}
				}
				value, err = json.Marshal(input)
				if err != nil {
					return err
				}
			}
			return applyConfigChange(m, s, owner, field, source[field.Key], value, remove, changed)
		}
		save := func(updated []string, remove bool) (bool, error) {
			if err := persist(updated, remove); err != nil {
				_, err = configEditFailure(m, err)
				return false, err
			}
			return false, nil
		}
		edit := func(existing bool) func() (bool, error) {
			return func() (bool, error) {
				index := len(entries)
				if existing {
					var err error
					index, err = m.Select("Select "+item, labels, "Back")
					if err != nil || index < 0 {
						return false, err
					}
				}
				pending := ""
				if existing {
					pending = entries[index]
				}
				for {
					text, accepted, err := m.Text(cliui.TextRequest{Prompt: "New " + item + " (:back cancels): ", Initial: pending, Sensitive: field.Sensitive, Validate: func(value string) error {
						if strings.ContainsRune(value, '\x00') {
							return fmt.Errorf("Entries cannot contain NUL.")
						}
						return nil
					}})
					if err != nil || !accepted {
						return false, err
					}
					updated := slices.Clone(entries)
					if existing {
						updated[index] = text
					} else {
						updated = append(updated, text)
					}
					if err := persist(updated, false); err != nil {
						retry, err := configEditFailure(m, err)
						if err != nil || !retry {
							return false, err
						}
						pending = text
						continue
					}
					return false, nil
				}
			}
		}
		return cliui.Screen{Title: fmt.Sprintf("%s (%s)", configLabel(field.Key), field.Key), Back: "Back", Body: func(out io.Writer) error {
			if field.Help != "" {
				if err := writeMenuHint(out, field.Help); err != nil {
					return err
				}
			}
			if len(entries) == 0 {
				fmt.Fprintln(out)
				return writeMenuHint(out, fmt.Sprintf("No %s configured here.", strings.ToLower(configLabel(field.Key))))
			}
			if err := writeMenuTitle(out, configLabel(field.Key)+" configured here:"); err != nil {
				return err
			}
			for i, label := range labels {
				prefix := menuPrefix(i + 1)
				if err := writeConfigLine(out, prefix, label, strings.Repeat(" ", len(prefix)), configDisplayWidth(out)); err != nil {
					return err
				}
			}
			return nil
		}, Actions: []cliui.Action{
			{Label: "Add " + item, Run: edit(false)},
			{Label: "Edit " + item, Hidden: len(entries) == 0, Run: edit(true)},
			{Label: "Remove " + item, Hidden: len(entries) == 0, Run: func() (bool, error) {
				index, err := m.Select("Remove "+item, labels, "Back")
				if err != nil || index < 0 {
					return false, err
				}
				return save(slices.Delete(slices.Clone(entries), index, index+1), false)
			}},
			{Label: "Remove this setting", Hidden: source[field.Key] == nil, Run: func() (bool, error) { return save(nil, true) }},
		}}, nil
	})
}
