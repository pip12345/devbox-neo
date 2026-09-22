package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/environment"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func setCommand(factory engineFactory, name *string) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{Use: "set <folder|session>", Short: "Select or clear a folder's default session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if clear && *name != "" {
			return fmt.Errorf("use --name or --clear, not both")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if clear {
			workspace, err := e.ClearDefault(cmd.Context(), args[0])
			if err == nil {
				cmd.Printf("Cleared default session for %s.\n", displayCell(workspace))
			}
			return err
		}
		var selected store.Record
		if *name != "" || environment.IsSessionTarget(args[0]) {
			selected, err = e.Locate(cmd.Context(), args[0], *name)
		} else {
			if !interactive(cmd) {
				return fmt.Errorf("set requires --name or --clear without a terminal, or an exact full session name")
			}
			m := menu{ctx: cmd.Context(), in: promptReader(cmd), out: cmd.OutOrStdout(), cmd: cmd}
			var record *store.Record
			var noDefault bool
			record, noDefault, err = chooseSession(m, e, args[0], true)
			if errors.Is(err, io.EOF) || (err == nil && record == nil && !noDefault) {
				return nil
			}
			if err != nil {
				return err
			}
			if noDefault {
				workspace, err := e.ClearDefault(cmd.Context(), args[0])
				if err == nil {
					cmd.Printf("Cleared default session for %s.\n", displayCell(workspace))
				}
				return err
			}
			selected = *record
		}
		if err != nil {
			return err
		}
		if err := e.SetDefault(cmd.Context(), selected); err != nil {
			return err
		}
		cmd.Printf("Default session for %s: %s\n", displayCell(selected.Identity.Workspace), selected.Identity.LocalName)
		return nil
	}}
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear the folder's default without selecting a replacement")
	return sessionNameFlag(cmd, name)
}

func chooseSession(m menu, e *app.Engine, folder string, forDefault bool) (*store.Record, bool, error) {
	workspace, entries, err := e.FolderSessions(m.ctx, folder)
	if err != nil {
		return nil, false, err
	}
	if len(entries) == 0 && !forDefault {
		fmt.Fprintln(m.out, "No sessions for this folder.")
		return nil, false, m.commandHint(e.Store.Home, "Create a session", "create", folder)
	}
	selected, defaultErr := e.Store.ReadDefault(m.ctx, workspace)
	if defaultErr != nil {
		writeMenuHint(m.out, "Default selection unavailable: "+displayCell(defaultErr.Error()))
	}
	states := map[string]string{}
	if report, err := e.List(m.ctx, workspace); err == nil {
		for _, view := range report.Sessions {
			states[view.Name] = containerState(view)
		}
	} else {
		writeMenuHint(m.out, "Container status unavailable: "+displayCell(err.Error()))
	}
	title := "Select a session to manage its config sources"
	if forDefault {
		title = "Select the default session"
	}
	if err := writeMenuTitle(m.out, title); err != nil {
		return nil, false, err
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
	if forDefault {
		writeStyledConfigLine(m.out, "Current selection: ", displayCell(summary), "  ", configDisplayWidth(m.out), paint.strong)
	}
	fmt.Fprintln(m.out)
	for i, entry := range entries {
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
		if i == current && !forDefault {
			state = "default · " + state
		}
		text := fmt.Sprintf("%-16s %s", displayCell(label), displayCell(state))
		var style func(string) string
		if states[entry.Name] != "running" {
			style = paint.dim
		}
		if i == current && forDefault {
			text += " (selected)"
			style = func(line string) string {
				line = paint.dim(line)
				line = strings.ReplaceAll(line, displayCell(label), paint.strong(displayCell(label)))
				return strings.ReplaceAll(line, "(selected)", paint.green("(selected)"))
			}
		}
		prefix := menuPrefix(i + 1)
		if err := writeStyledConfigLine(m.out, prefix, text, strings.Repeat(" ", len(prefix)), configDisplayWidth(m.out), style); err != nil {
			return nil, false, err
		}
	}
	count := len(entries)
	if forDefault {
		count++
		text := "No default"
		var style func(string) string
		if selected == nil && defaultErr == nil {
			text += " (selected)"
			style = func(line string) string {
				return strings.ReplaceAll(paint.strong(line), "(selected)", paint.green("(selected)"))
			}
		}
		writeStyledConfigLine(m.out, menuPrefix(count), text, "        ", configDisplayWidth(m.out), style)
	}
	choice, err := m.readChoice(count, "Cancel")
	if err != nil || choice < 0 {
		return nil, false, err
	}
	if choice == len(entries) {
		return nil, true, nil
	}
	if entries[choice].Err != nil {
		return nil, false, entries[choice].Err
	}
	return &entries[choice].Record, false, nil
}
