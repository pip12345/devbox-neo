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
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func createCommand(factory engineFactory, name *string) *cobra.Command {
	var references []string
	var makeDefault bool
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
		draft := sessionCreationDraft{workspace: workspace, name: *name, sources: sources, makeDefault: makeDefault}
		if draft.name != "" {
			if err := environment.ValidateLocalName(draft.name); err != nil {
				return err
			}
		}
		if missing {
			f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
			return f.createSessionFromDraft(draft)
		}
		// Creation streams diagnostics; restore the shell before materialization.
		if err := m.Finish(); err != nil {
			return err
		}
		result, err := e.Create(cmd.Context(), app.Request{Workspace: workspace, LocalName: draft.name, Sources: draft.sources, MakeDefault: draft.makeDefault})
		if err != nil {
			return err
		}
		cmd.Printf("\nCreated session %s\nSession: %s\nFolder: %s\nContainer: stopped\n", draft.name, result.Session, displayCell(workspace))
		if err := showSourceChain(m, e.Store.Home, workspace, draft.sources); err != nil {
			return err
		}
		selectDefault := commanderror.Next("Select it as this folder's default", "edit", args[0], "--name", draft.name, "--default")
		if interactive(cmd) {
			selectDefault = commanderror.Next("Choose this folder's default session", "edit", args[0])
		}
		steps := []commanderror.Step{selectDefault, commanderror.Next("Then open it", "open", args[0])}
		if draft.makeDefault {
			cmd.Printf("Folder default: %s\n", draft.name)
			steps = []commanderror.Step{commanderror.Next("Open it", "open", result.Session)}
		}
		cmd.Printf("\n%s", stepsText(scopedSteps(cmd, steps, e.Store.Home)))
		return nil
	}}
	cmd.Example = "  dbx create .\n  dbx create . --name work --config base"
	cmd.Flags().StringArrayVar(&references, "config", nil, "Existing config name or directory path, in order (repeatable)")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "Make the created session the folder default, replacing any existing selection")
	return sessionNameFlag(cmd, name)
}

type sessionCreationDraft struct {
	workspace   string
	name        string
	sources     []config.Reference
	makeDefault bool
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

// Submission runs inside the form so a failed build retains the draft for retry.
func sessionCreationMenu(p sourcePicker, e *app.Engine, draft sessionCreationDraft, create func(sessionCreationDraft) (bool, error)) (sessionCreationDraft, bool, error) {
	proceed := false
	err := p.Run(func() (cliui.Screen, error) {
		p.workspace = draft.workspace
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
			proceed, err = create(draft)
			return proceed, err
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
		actions = append(actions, cliui.Action{Label: "Change folder", Run: func() (bool, error) {
			folder, ok, err := p.Text(cliui.TextRequest{Prompt: "Workspace folder: ", Initial: draft.workspace, Validate: func(value string) error { _, err := environment.CanonicalWorkspace(value); return err }})
			if err == nil && ok {
				draft.workspace, err = environment.CanonicalWorkspace(folder)
			}
			return false, err
		}})
		defaultDescription := ""
		selected, defaultErr := e.Store.ReadDefault(p.Context, draft.workspace)
		if defaultErr != nil {
			defaultDescription = "Cannot read current default: " + displayCell(defaultErr.Error())
		} else if selected != nil {
			label := selected.ID
			if current, err := e.Locate(p.Context, selected.ID, ""); err == nil && current.Settings.Workspace == draft.workspace {
				label = current.Settings.LocalName
			}
			defaultDescription = "Replaces " + displayCell(label)
		}
		actions = append(actions, cliui.Action{Label: "Make folder default", Value: defaultDescription, Description: "Select this session after creation; unchecked keeps the current selection", Checked: &draft.makeDefault, Run: func() (bool, error) {
			draft.makeDefault = !draft.makeDefault
			return false, nil
		}}, createAction)
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
				resolved, err := e.CombinedConfiguration(store.Record{Settings: store.Settings{Binding: environment.Binding{Workspace: p.workspace}, Sources: draft.sources}})
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
