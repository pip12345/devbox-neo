package cli

import (
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/store"
)

func folderEditMenu(m menu, e *app.Engine, folder string, open func(store.Record) error, changed func(string), back string) error {
	workspace, entries, err := e.FolderSessions(m.Context, folder)
	if err != nil {
		return err
	}
	selected, defaultErr := e.Store.ReadDefault(m.Context, workspace)
	if len(entries) == 0 && selected == nil && defaultErr == nil {
		fmt.Fprintln(m.Out, "No sessions for this folder.")
		return m.commandHint(e.Store.Home, "Create a session", "create", folder)
	}
	return m.Run(func() (cliui.Screen, error) {
		workspace, entries, err := e.FolderSessions(m.Context, folder)
		if err != nil {
			return cliui.Screen{}, err
		}
		selected, defaultErr := e.Store.ReadDefault(m.Context, workspace)
		states := folderSessionStates(m, e, workspace)
		page := folderSessionScreen(workspace, entries, selected, defaultErr, states)
		page.Back = back
		canSet := false
		for i, entry := range entries {
			if entry.Err == nil {
				canSet = true
			}
			page.Actions[i].Run = func() (bool, error) {
				if entry.Err != nil {
					return false, m.report(entry.Err)
				}
				return false, open(entry.Record)
			}
		}
		page.Actions = append(page.Actions,
			cliui.Action{Label: "Set folder default", Hidden: !canSet, BreakBefore: true, Run: func() (bool, error) {
				chosen, err := chooseFolderDefault(m, e, folder)
				if err != nil || chosen == nil {
					return false, err
				}
				if err := e.SetDefault(m.Context, *chosen); err != nil {
					return false, err
				}
				message := fmt.Sprintf("Default session for %s: %s", displayCell(chosen.Identity.Workspace), chosen.Identity.LocalName)
				changed(message)
				return false, nil
			}},
			cliui.Action{Label: "Clear folder default", Hidden: selected == nil, BreakBefore: !canSet, Run: func() (bool, error) {
				workspace, err := e.ClearDefault(m.Context, folder)
				if err != nil {
					return false, err
				}
				message := fmt.Sprintf("Cleared default session for %s.", displayCell(workspace))
				changed(message)
				if len(entries) == 0 {
					fmt.Fprintln(m.Out, "No sessions for this folder.")
					return true, m.commandHint(e.Store.Home, "Create a session", "create", folder)
				}
				return false, nil
			}},
		)
		return page, nil
	})
}

// Both folder interactions keep the same header and session rows. Selecting a
// default changes the action attached to a row, not its position or styling.
// Successful changes appear in this header and the exit receipt, rather than
// transient notices above the screen that would move the session rows.
func folderSessionScreen(workspace string, entries []store.Entry, selected *store.DefaultSession, defaultErr error, states map[string]string) cliui.Screen {
	current, summary := -1, "No default"
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
	actions := make([]cliui.Action, len(entries))
	for i, entry := range entries {
		actions[i].Label = entry.Record.Identity.LocalName
		if actions[i].Label == "" {
			actions[i].Label = entry.Name
		}
		actions[i].Status = states[entry.Name]
		actions[i].Selected = i == current
		actions[i].Detail = "Folder: " + displayCell(workspace) + "\nFull name: " + displayCell(entry.Name)
		if entry.Err != nil {
			actions[i].Detail += "\nError: " + displayCell(entry.Err.Error())
		}
	}
	return cliui.Screen{Title: "Select a session to edit", Back: "Exit", Actions: actions, Body: func(out io.Writer) error {
		if defaultErr != nil {
			writeMenuHint(out, "Default selection unavailable: "+displayCell(defaultErr.Error()))
		}
		writeMenuHint(out, "Folder: "+displayCell(workspace))
		return writeStyledConfigLine(out, "Default: ", displayCell(summary), "  ", configDisplayWidth(out), terminalColors(out).strong)
	}, Rows: func(out io.Writer, actions []cliui.Action) error {
		fmt.Fprintln(out)
		paint := terminalColors(out)
		for i, action := range actions {
			if action.BreakBefore {
				fmt.Fprintln(out)
			}
			label := action.Label
			var style func(string) string
			prefix := menuPrefix(i + 1)
			if i < len(entries) {
				label = sessionPickerLabel(entries[i], states)
				inactive := states[entries[i].Name] != "running"
				if inactive {
					style = paint.dim
				}
				if i == current {
					label = "* " + label
					style = defaultRowStyle(paint, prefix, inactive)
				} else {
					label = "  " + label
				}
			}
			if err := writeStyledConfigLine(out, prefix, label, strings.Repeat(" ", len(prefix)), configDisplayWidth(out), style); err != nil {
				return err
			}
		}
		return nil
	}}
}

func chooseFolderDefault(m menu, e *app.Engine, folder string) (*store.Record, error) {
	workspace, entries, err := e.FolderSessions(m.Context, folder)
	if err != nil {
		return nil, err
	}
	selected, err := e.Store.ReadDefault(m.Context, workspace)
	if err != nil {
		return nil, err
	}
	states := folderSessionStates(m, e, workspace)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no sessions available to select as the folder default")
	}
	page := folderSessionScreen(workspace, entries, selected, nil, states)
	page.Title, page.Back = "Select the folder default", "Back"
	choice, err := m.Choose(page)
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
	if report, err := e.List(m.Context, workspace); err == nil {
		for _, view := range report.Sessions {
			states[view.Name] = containerState(view)
		}
	} else {
		m.Notice("Container status unavailable: " + displayCell(err.Error()))
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
