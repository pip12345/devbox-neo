package cli

import (
	"fmt"
	"strings"

	"devbox/internal/app"
	"devbox/internal/store"
)

type folderEditAction uint8

const (
	folderEditNone folderEditAction = iota
	folderEditSetDefault
	folderEditClearDefault
)

func chooseSessionToEdit(m menu, e *app.Engine, folder string) (*store.Record, folderEditAction, error) {
	workspace, entries, err := e.FolderSessions(m.ctx, folder)
	if err != nil {
		return nil, folderEditNone, err
	}
	selected, defaultErr := e.Store.ReadDefault(m.ctx, workspace)
	if len(entries) == 0 && selected == nil && defaultErr == nil {
		fmt.Fprintln(m.out, "No sessions for this folder.")
		return nil, folderEditNone, m.commandHint(e.Store.Home, "Create a session", "create", folder)
	}
	if defaultErr != nil {
		writeMenuHint(m.out, "Default selection unavailable: "+displayCell(defaultErr.Error()))
	}
	states := folderSessionStates(m, e, workspace)
	if err := writeMenuTitle(m.out, "Select a session to edit"); err != nil {
		return nil, folderEditNone, err
	}
	writeMenuHint(m.out, "Folder: "+displayCell(workspace))
	current := -1
	summary := "No default"
	if selected != nil {
		summary = "Unavailable: " + selected.Name
		for i, entry := range entries {
			if entry.Name == selected.Name && entry.Record.ID == selected.ID {
				current, summary = i, entry.Record.Identity.LocalName
			}
		}
	}
	if defaultErr != nil {
		summary = "Unavailable"
	}
	paint := terminalColors(m.out)
	writeStyledConfigLine(m.out, "Default: ", displayCell(summary), "  ", configDisplayWidth(m.out), paint.strong)
	fmt.Fprintln(m.out)
	canSet := false
	for i, entry := range entries {
		if entry.Err == nil {
			canSet = true
		}
		label := sessionPickerLabel(entry, states)
		inactive := states[entry.Name] != "running"
		var style func(string) string
		if inactive {
			style = paint.dim
		}
		prefix := menuPrefix(i + 1)
		if i == current {
			label = "* " + label
			style = defaultRowStyle(paint, prefix, inactive)
		} else {
			label = "  " + label
		}
		if err := writeStyledConfigLine(m.out, prefix, label, strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out), style); err != nil {
			return nil, folderEditNone, err
		}
	}
	count, setIndex, clearIndex := len(entries), -1, -1
	if canSet || selected != nil {
		fmt.Fprintln(m.out)
	}
	if canSet {
		setIndex = count
		count++
		prefix := menuPrefix(count)
		if err := writeConfigLine(m.out, prefix, "Set folder default", strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out)); err != nil {
			return nil, folderEditNone, err
		}
	}
	if selected != nil {
		clearIndex = count
		count++
		prefix := menuPrefix(count)
		if err := writeConfigLine(m.out, prefix, "Clear folder default", strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out)); err != nil {
			return nil, folderEditNone, err
		}
	}
	choice, err := m.readChoice(count, "Exit")
	if err != nil || choice < 0 {
		return nil, folderEditNone, err
	}
	if choice == setIndex {
		return nil, folderEditSetDefault, nil
	}
	if choice == clearIndex {
		return nil, folderEditClearDefault, nil
	}
	if entries[choice].Err != nil {
		return nil, folderEditNone, entries[choice].Err
	}
	return &entries[choice].Record, folderEditNone, nil
}

func chooseFolderDefault(m menu, e *app.Engine, folder string) (*store.Record, error) {
	workspace, entries, err := e.FolderSessions(m.ctx, folder)
	if err != nil {
		return nil, err
	}
	selected, err := e.Store.ReadDefault(m.ctx, workspace)
	if err != nil {
		return nil, err
	}
	states := folderSessionStates(m, e, workspace)
	choices := make([]string, len(entries))
	current, summary := -1, "No default"
	if selected != nil {
		summary = "Unavailable: " + selected.Name
	}
	for i, entry := range entries {
		choices[i] = sessionPickerLabel(entry, states)
		if selected != nil && entry.Name == selected.Name && entry.Record.ID == selected.ID {
			current, summary = i, entry.Record.Identity.LocalName
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no sessions available to select as the folder default")
	}
	choice, err := m.selectedChoice("Set folder default · "+displayCell(workspace), choices, current, summary, "Back")
	if err != nil || choice < 0 {
		return nil, err
	}
	if entries[choice].Err != nil {
		return nil, entries[choice].Err
	}
	return &entries[choice].Record, nil
}

func folderSessionStates(m menu, e *app.Engine, workspace string) map[string]string {
	states := map[string]string{}
	if report, err := e.List(m.ctx, workspace); err == nil {
		for _, view := range report.Sessions {
			states[view.Name] = containerState(view)
		}
	} else {
		writeMenuHint(m.out, "Container status unavailable: "+displayCell(err.Error()))
	}
	return states
}

func defaultRowStyle(paint terminalPaint, prefix string, inactive bool) func(string) string {
	ordinary := func(text string) string {
		if inactive {
			return paint.dim(text)
		}
		return text
	}
	return func(line string) string {
		if strings.HasPrefix(line, prefix+"*") {
			return ordinary(prefix) + paint.green("*") + ordinary(line[len(prefix)+1:])
		}
		return ordinary(line)
	}
}

func sessionPickerLabel(entry store.Entry, states map[string]string) string {
	label := entry.Record.Identity.LocalName
	if label == "" {
		label = entry.Name
	}
	state := states[entry.Name]
	if state == "" {
		state = "unknown"
	}
	if entry.Err != nil {
		state = "Error: " + entry.Err.Error()
	}
	return fmt.Sprintf("%-16s %s", displayCell(label), displayCell(state))
}
