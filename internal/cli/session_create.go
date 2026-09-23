package cli

import (
	"errors"
	"fmt"
	"io"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

func createCommand(factory engineFactory, name *string) *cobra.Command {
	var references []string
	cmd := &cobra.Command{Use: "create <folder>", Short: "Name a new session and select its existing config sources", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		missing := *name == "" || len(references) == 0
		if missing && !interactive(cmd) {
			return commanderror.New("creation_inputs_required", "Session creation requires --name and at least one --config without a terminal.", args[0], nil,
				commanderror.Next("Supply the session name and config sources", "create", args[0], "--name", "<name>", "--config", "<reference>"))
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		workspace, err := environment.CanonicalWorkspace(args[0])
		if err != nil {
			return err
		}
		m := newMenu(cmd)
		defer func() { runErr = errors.Join(runErr, m.finish()) }()
		picker, err := newSourcePicker(m, e.Store.Home, workspace)
		if err != nil {
			return err
		}
		var sources []config.Reference
		for _, input := range references {
			reference, err := picker.capture(input)
			if err != nil {
				return err
			}
			sources = append(sources, reference)
		}
		draft := sessionCreationDraft{name: *name, sources: sources}
		if draft.name != "" {
			if err := environment.ValidateLocalName(draft.name); err != nil {
				return err
			}
		}
		if missing {
			var proceed bool
			draft, proceed, err = createSessionMenu(picker, e, draft)
			if errors.Is(err, io.EOF) || (err == nil && !proceed) {
				fmt.Fprintln(m.out, "Cancelled. No session was created.")
				return nil
			}
			if err != nil {
				return err
			}
		}
		// Creation may emit diagnostics to stderr. Restore the shell before
		// running it so those messages cannot vanish with the menu screen.
		if err := m.finish(); err != nil {
			return err
		}
		result, err := e.Create(cmd.Context(), app.Request{Workspace: workspace, LocalName: draft.name, Sources: draft.sources})
		if err != nil {
			return err
		}
		cmd.Printf("\nCreated session %s\nFull name: %s\nFolder: %s\nContainer: stopped\n", draft.name, result.Name, displayCell(workspace))
		if err := showSourceChain(m, e.Store.Home, workspace, draft.sources); err != nil {
			return err
		}
		selectDefault := commanderror.Next("Select it as this folder's default", "edit", args[0], "--name", draft.name, "--default")
		if interactive(cmd) {
			selectDefault = commanderror.Next("Choose this folder's default session", "edit", args[0])
		}
		steps := scopedSteps(cmd, []commanderror.Step{
			selectDefault,
			commanderror.Next("Then open it", "open", args[0]),
		}, e.Store.Home)
		cmd.Printf("\n%s", stepsText(steps))
		return nil
	}}
	cmd.Example = "  devbox-neo create .\n  devbox-neo create . --name work --config base"
	cmd.Flags().StringArrayVar(&references, "config", nil, "Existing config name or directory path, in source order (repeatable)")
	return sessionNameFlag(cmd, name)
}

type sessionCreationDraft struct {
	name    string
	sources []config.Reference
}

func createSessionMenu(p sourcePicker, e *app.Engine, draft sessionCreationDraft) (sessionCreationDraft, bool, error) {
	for {
		if err := writeMenuTitle(p.out, "Create session"); err != nil {
			return draft, false, err
		}
		writeMenuHint(p.out, "Folder: "+displayCell(p.workspace))
		name := "Not set"
		if draft.name != "" {
			name = draft.name
		}
		if err := writeStyledConfigLine(p.out, "Session name: ", name, "  ", configDisplayWidth(p.out), terminalColors(p.out).strong); err != nil {
			return draft, false, err
		}
		if err := showSourceChain(p.menu, p.home, p.workspace, draft.sources); err != nil {
			return draft, false, err
		}
		nameAction := "Set session name"
		if draft.name != "" {
			nameAction = "Change session name"
		}
		actions := []string{nameAction, "Add source"}
		if len(draft.sources) > 0 {
			actions = append(actions, "Replace source", "Remove source", "Reorder sources")
		}
		gapBefore := -1
		if draft.name != "" && len(draft.sources) > 0 {
			actions = append([]string{"Create session"}, actions...)
			gapBefore = 1
		}
		choice, err := p.menu.choose("What would you like to do?", actions, "Cancel", gapBefore)
		if err != nil || choice < 0 {
			return draft, false, err
		}
		switch actions[choice] {
		case "Create session":
			if err := p.menu.pause(); err != nil {
				return draft, false, err
			}
			spec, err := e.Resolve(app.Request{Workspace: p.workspace, LocalName: draft.name, Sources: draft.sources})
			if err != nil {
				if len(spec.Warnings) > 0 {
					p.menu.showNextPlain()
				}
				fmt.Fprintf(p.out, "Error: %s\n", displayCell(err.Error()))
				continue
			}
			return draft, true, nil
		case "Set session name", "Change session name":
			name, changed, err := editSessionCreationName(p, draft.name)
			if err != nil {
				return draft, false, err
			}
			if changed {
				draft.name = name
			}
		default:
			updated, changed, err := editSourceChain(p, draft.sources, actions[choice])
			if errors.Is(err, io.EOF) {
				return draft, false, err
			}
			if err != nil {
				fmt.Fprintf(p.out, "Error: %s\n", displayCell(err.Error()))
				continue
			}
			if changed {
				draft.sources = updated
			}
		}
	}
}

func editSessionCreationName(p sourcePicker, current string) (string, bool, error) {
	for {
		if err := writeMenuTitle(p.out, "Session name"); err != nil {
			return current, false, err
		}
		writeMenuHint(p.out, "Folder: "+displayCell(p.workspace))
		if current != "" {
			writeMenuHint(p.out, "Current name: "+current)
		}
		value, err := p.line("Session name (:back cancels): ")
		if err != nil || value == ":back" {
			return current, false, err
		}
		if err := environment.ValidateLocalName(value); err != nil {
			fmt.Fprintf(p.out, "Error: %s\n", err)
			continue
		}
		return value, true, nil
	}
}
