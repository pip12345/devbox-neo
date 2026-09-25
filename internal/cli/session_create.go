package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func createCommand(factory engineFactory, name *string) *cobra.Command {
	var references []string
	cmd := &cobra.Command{Use: "create <folder>", Short: "Name a new session and select its configs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		missing := *name == "" || len(references) == 0
		if missing && !interactive(cmd) {
			return commanderror.New("creation_inputs_required", "Session creation requires --name and at least one --config without a terminal.", args[0], nil,
				commanderror.Next("Supply the session name and configs", "create", args[0], "--name", "<name>", "--config", "<name|path>"))
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
		defer func() { runErr = errors.Join(runErr, m.Finish()) }()
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
				fmt.Fprintln(m.Out, "Cancelled. No session was created.")
				return nil
			}
			if err != nil {
				return err
			}
		}
		// Creation streams diagnostics; restore the shell before materialization.
		if err := m.Finish(); err != nil {
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
		steps := scopedSteps(cmd, []commanderror.Step{selectDefault, commanderror.Next("Then open it", "open", args[0])}, e.Store.Home)
		cmd.Printf("\n%s", stepsText(steps))
		return nil
	}}
	cmd.Example = "  devbox-neo create .\n  devbox-neo create . --name work --config base"
	cmd.Flags().StringArrayVar(&references, "config", nil, "Existing config name or directory path, in order (repeatable)")
	return sessionNameFlag(cmd, name)
}

type sessionCreationDraft struct {
	workspace string
	name      string
	sources   []config.Reference
}

func (d sessionCreationDraft) missing() string {
	if d.name == "" && len(d.sources) == 0 {
		return "Set a session name and add at least one config first."
	}
	if d.name == "" {
		return "Set a session name first."
	}
	if len(d.sources) == 0 {
		return "Add at least one config first."
	}
	return ""
}

func createSessionMenu(p sourcePicker, e *app.Engine, draft sessionCreationDraft) (sessionCreationDraft, bool, error) {
	return sessionCreationMenu(p, e, draft, nil)
}

// The browser supplies materialization so a failed build returns to this same
// draft. Direct create retains its existing streamed, command-ending operation.
func sessionCreationMenu(p sourcePicker, e *app.Engine, draft sessionCreationDraft, create func(sessionCreationDraft) (bool, error)) (sessionCreationDraft, bool, error) {
	proceed := false
	err := p.Run(func() (cliui.Screen, error) {
		if draft.workspace != "" {
			p.workspace = draft.workspace
		}
		nameAction := "Set session name"
		if draft.name != "" {
			nameAction = "Change session name"
		}
		createAction := cliui.Action{Label: "Create session", BreakBefore: true, Blocked: draft.missing(), Run: func() (bool, error) {
			if err := p.Pause(); err != nil {
				return false, err
			}
			spec, err := e.Resolve(app.Request{Workspace: p.workspace, LocalName: draft.name, Sources: draft.sources})
			if err != nil {
				if len(spec.Warnings) > 0 {
					if reviewErr := p.ReviewOutput(); reviewErr != nil {
						return false, reviewErr
					}
				}
				return false, p.report(err)
			}
			if create != nil {
				var err error
				proceed, err = create(draft)
				return proceed, err
			}
			proceed = true
			return true, nil
		}}
		actions := []cliui.Action{
			{Label: nameAction, BreakBefore: true, Run: func() (bool, error) {
				name, changed, err := editSessionCreationName(p, draft.name)
				if changed {
					draft.name = name
				}
				return false, err
			}},
		}
		actions = append(actions, p.chainActions(draft.sources, false, func(updated []config.Reference) error { draft.sources = updated; return nil })...)
		if draft.workspace != "" {
			actions = append(actions, cliui.Action{Label: "Change folder", Run: func() (bool, error) {
				folder, ok, err := p.Text(cliui.TextRequest{Prompt: "Workspace folder: ", Initial: draft.workspace, Validate: func(value string) error { _, err := environment.CanonicalWorkspace(value); return err }})
				if err == nil && ok {
					draft.workspace, err = environment.CanonicalWorkspace(folder)
				}
				return false, err
			}})
		}
		actions = append(actions, createAction)
		page := cliui.Screen{Title: "Create session", Actions: actions, Back: "Cancel", Body: func(out io.Writer) error {
			if err := writeMenuHint(out, "Folder: "+displayCell(p.workspace)); err != nil {
				return err
			}
			name := draft.name
			if name == "" {
				name = "Not set"
			}
			if err := writeStyledConfigLine(out, "Session name: ", name, "  ", configDisplayWidth(out), terminalColors(out).strong); err != nil {
				return err
			}
			return showSourceChain(p.menu, p.home, p.workspace, draft.sources)
		}}
		if p.Redraws() {
			page.Body = nil
			name := draft.name
			if name == "" {
				name = "Not set"
			}
			var refs []string
			for i, ref := range draft.sources {
				refs = append(refs, fmt.Sprintf("%d. %s", i+1, ref.Label))
			}
			selected := strings.Join(refs, "\n")
			if selected == "" {
				selected = "None"
			}
			page.Fields = []cliui.Field{{Label: "Folder", Value: p.workspace}, {Label: "Name", Value: name}, {Label: "Configs", Value: selected}}
			if len(draft.sources) > 0 {
				resolved, err := e.CombinedConfiguration(store.Record{Identity: environment.Identity{Workspace: p.workspace}, Sources: draft.sources})
				if err != nil {
					page.Fields = append(page.Fields, cliui.Field{Label: "Error", Value: err.Error(), Warning: true})
				} else {
					page.Fields = append(page.Fields, cliui.Field{Label: "Harness", Value: resolved.Settings.Harness})
				}
			}
		}
		return page, nil
	})
	return draft, proceed, err
}

func editSessionCreationName(p sourcePicker, current string) (string, bool, error) {
	if err := writeMenuTitle(p.Out, "Session name"); err != nil {
		return current, false, err
	}
	writeMenuHint(p.Out, "Folder: "+displayCell(p.workspace))
	if current != "" {
		writeMenuHint(p.Out, "Current name: "+current)
	}
	name, changed, err := p.Text(cliui.TextRequest{Prompt: "Session name (:back cancels): ", Initial: current, Validate: environment.ValidateLocalName})
	if !changed {
		return current, false, err
	}
	return name, true, nil
}
