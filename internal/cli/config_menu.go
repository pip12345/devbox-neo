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
			steps := scopedSteps(cmd, []commanderror.Step{commanderror.Next("", "status")}, s.Home)
			_, receiptErr := fmt.Fprintf(cmd.OutOrStdout(), "Config changes saved: %s\nReview pending changes:\n%s", displayCell(owner.Name), stepsText(steps))
			err = errors.Join(err, receiptErr)
		}
	}()
	err = configMenu(m, s, owner, &changed)
	if errors.Is(err, io.EOF) {
		fmt.Fprintln(m.Out, "\nMenu closed. Completed changes remain saved.")
		return nil
	}
	return err
}

func configMenu(m menu, s *resource.Service, owner resource.Owner, changed *bool) error {
	fields := resource.ConfigFields()
	return m.Run(func() (cliui.Screen, error) {
		source, err := s.ConfigSource(owner)
		if err != nil {
			return cliui.Screen{}, err
		}
		view, resolveErr := s.ShowOwner(owner)
		rows := make([]configDisplayRow, len(fields))
		actions := make([]cliui.Action, 0, len(fields)+1)
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
			actions = append(actions, cliui.Action{Label: configLabel(field.Key), Run: func() (bool, error) {
				return false, editConfigField(m, s, owner, field, source, changed)
			}})
		}
		actions = append(actions, cliui.Action{Label: "Add optional files", BreakBefore: true, Run: func() (bool, error) {
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
		return cliui.Screen{Title: configMenuTitle(owner), Back: "Exit", Actions: actions, Body: func(out io.Writer) error {
			if resolveErr != nil {
				_, err := fmt.Fprintf(out, "Effective configuration unavailable: %s\nShowing values configured here; you can still edit them.\n", displayCell(resolveErr.Error()))
				return err
			}
			return nil
		}, Rows: func(out io.Writer, actions []cliui.Action) error { return printConfigActions(out, rows, actions) }}, nil
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
	remove := selected == 1
	var value json.RawMessage
	if !remove {
		value, err = readConfigValue(m, s, field, source[field.Key])
		if err != nil || value == nil {
			return err
		}
	}
	return applyConfigChange(m, s, owner, field, source[field.Key], value, remove, changed)
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
			text, accepted, err := m.Text("New value (:back cancels): ", nil)
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

// The service compares the displayed field snapshot under its owner lock.
// On conflict the next screen reloads; failed writes never advance UI state.
func applyConfigChange(m menu, s *resource.Service, owner resource.Owner, field resource.ConfigField, expected, value json.RawMessage, remove bool, changed *bool) error {
	if err := s.SetConfigField(m.Context, owner, field.Key, expected, value, remove); err != nil {
		if m.Context.Err() != nil {
			return m.Context.Err()
		}
		m.Notice("Not saved: " + displayCell(err.Error()))
		return nil
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
		save := func(updated []string, remove bool) (bool, error) {
			var value json.RawMessage
			if !remove {
				var input any = updated
				if field.Kind == "extensions" {
					input = config.VSCode{Extensions: updated}
				}
				value, err = json.Marshal(input)
				if err != nil {
					return false, err
				}
			}
			return false, applyConfigChange(m, s, owner, field, source[field.Key], value, remove, changed)
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
				text, accepted, err := m.Text("New "+item+" (:back cancels): ", func(value string) error {
					if strings.ContainsRune(value, '\x00') {
						return fmt.Errorf("Entries cannot contain NUL.")
					}
					return nil
				})
				if err != nil || !accepted {
					return false, err
				}
				updated := slices.Clone(entries)
				if existing {
					updated[index] = text
				} else {
					updated = append(updated, text)
				}
				return save(updated, false)
			}
		}
		return cliui.Screen{Title: fmt.Sprintf("%s (%s)", configLabel(field.Key), field.Key), Back: "Back", Prompt: "What would you like to do?", Body: func(out io.Writer) error {
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
