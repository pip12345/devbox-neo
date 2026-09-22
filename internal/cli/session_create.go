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
	cmd := &cobra.Command{Use: "create <folder>", Short: "Name a new session and select its existing config sources", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		m := menu{ctx: cmd.Context(), in: promptReader(cmd), out: cmd.OutOrStdout(), cmd: cmd}
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
		localName := *name
		for localName == "" {
			value, err := m.line("Session name (:back cancels): ")
			if errors.Is(err, io.EOF) || value == ":back" {
				cmd.Println("Cancelled. No session was created.")
				return nil
			}
			if err != nil {
				return err
			}
			if err := environment.ValidateLocalName(value); err != nil {
				cmd.Printf("Error: %s\n", err)
				continue
			}
			localName = value
		}
		if err := environment.ValidateLocalName(localName); err != nil {
			return err
		}
		if missing {
			var proceed bool
			sources, proceed, err = createSessionMenu(picker, e, localName, sources)
			if errors.Is(err, io.EOF) || (err == nil && !proceed) {
				cmd.Println("Cancelled. No session was created.")
				return nil
			}
			if err != nil {
				return err
			}
		}
		result, err := e.Create(cmd.Context(), app.Request{Workspace: workspace, LocalName: localName, Sources: sources})
		if err != nil {
			return err
		}
		cmd.Printf("\nCreated session %s\nFull name: %s\nFolder: %s\nContainer: stopped\n", localName, result.Name, displayCell(workspace))
		if err := showSourceChain(m, e.Store.Home, workspace, sources); err != nil {
			return err
		}
		selectDefault := commanderror.Next("Select it as this folder's default", "set", args[0], "--name", localName)
		if interactive(cmd) {
			selectDefault = commanderror.Next("Choose this folder's default session", "set", args[0])
		}
		steps := scopedSteps(cmd, []commanderror.Step{
			selectDefault,
			commanderror.Next("Then open it", "open", args[0]),
		}, e.Store.Home)
		cmd.Printf("\n%s", stepsText(steps))
		return nil
	}}
	cmd.Flags().StringArrayVar(&references, "config", nil, "Existing config name or directory path, in source order (repeatable)")
	return sessionNameFlag(cmd, name)
}

func createSessionMenu(p sourcePicker, e *app.Engine, name string, sources []config.Reference) ([]config.Reference, bool, error) {
	for {
		if err := writeMenuTitle(p.out, "Create session · "+name); err != nil {
			return sources, false, err
		}
		writeMenuHint(p.out, "Folder: "+displayCell(p.workspace))
		if err := showSourceChain(p.menu, p.home, p.workspace, sources); err != nil {
			return sources, false, err
		}
		actions := []string{"Add source"}
		if len(sources) > 0 {
			actions = []string{"Create session", "Add source", "Replace source", "Remove source", "Reorder sources"}
		}
		choice, err := p.menu.choose("What would you like to do?", actions, "Cancel")
		if err != nil || choice < 0 {
			return sources, false, err
		}
		if actions[choice] == "Create session" {
			if _, err := e.Resolve(app.Request{Workspace: p.workspace, LocalName: name, Sources: sources}); err != nil {
				fmt.Fprintf(p.out, "Error: %s\n", displayCell(err.Error()))
				continue
			}
			return sources, true, nil
		}
		updated, changed, err := editSourceChain(p, sources, actions[choice])
		if errors.Is(err, io.EOF) {
			return sources, false, err
		}
		if err != nil {
			fmt.Fprintf(p.out, "Error: %s\n", displayCell(err.Error()))
			continue
		}
		if changed {
			sources = updated
		}
	}
}
