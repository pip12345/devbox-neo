package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func editCommand(factory engineFactory, name *string) *cobra.Command {
	var show, asJSON, setDefault, clearDefault bool
	var references []string
	cmd := &cobra.Command{Use: "edit <folder|session>", Short: "Browse a folder's sessions or edit a session's selected configs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		replace := cmd.Flags().Changed("config")
		if replace && (show || setDefault || clearDefault) {
			return fmt.Errorf("use --config alone, not with --show, --default, or --clear-default")
		}
		if asJSON && !show && !replace {
			return fmt.Errorf("--json requires --show or --config")
		}
		if setDefault && clearDefault {
			return fmt.Errorf("use --default or --clear-default, not both")
		}
		if clearDefault && *name != "" {
			return fmt.Errorf("use --name or --clear-default, not both")
		}
		if show && (setDefault || clearDefault) {
			return fmt.Errorf("use --show or change the folder default, not both")
		}
		direct := *name != "" || environment.IsSessionTarget(args[0])
		if show && !direct {
			return fmt.Errorf("--show requires --name or an exact full session name")
		}
		if setDefault && !direct {
			return fmt.Errorf("--default requires --name or an exact full session name")
		}
		if replace && !direct {
			return fmt.Errorf("--config requires --name or an exact full session name")
		}
		if !show && !setDefault && !clearDefault && !replace && !interactive(cmd) {
			return fmt.Errorf("editing requires a terminal; use --name NAME with --config to replace selected configs, --show to inspect, or --default to select without prompting")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		if replace {
			return replaceSessionConfigs(cmd, e, args[0], *name, references, asJSON)
		}
		if clearDefault {
			workspace, err := e.ClearDefault(cmd.Context(), args[0])
			if err == nil {
				cmd.Printf("Cleared default session for %s.\n", displayCell(workspace))
			}
			return err
		}
		if setDefault {
			r, err := e.Locate(cmd.Context(), args[0], *name)
			if err != nil {
				return err
			}
			if err := e.SetDefault(cmd.Context(), r); err != nil {
				return err
			}
			cmd.Printf("Default session for %s: %s\n", displayCell(r.Identity.Workspace), r.Identity.LocalName)
			return nil
		}
		if show {
			r, err := e.Locate(cmd.Context(), args[0], *name)
			if err != nil {
				return err
			}
			view, err := combinedView(e, r)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			return printConfigView(cmd.OutOrStdout(), view)
		}
		target := ""
		if direct {
			r, err := e.Locate(cmd.Context(), args[0], *name)
			if err != nil {
				return err
			}
			target = r.Identity.Name
		} else {
			target, err = environment.CanonicalWorkspace(args[0])
			if err != nil {
				return err
			}
			if _, err := e.List(cmd.Context(), ""); err != nil {
				return err
			}
		}
		m := newMenu(cmd)
		defer func() { runErr = errors.Join(runErr, m.Finish()) }()
		f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		if direct {
			err = f.editSession(target)
		} else {
			f.knownFolder, f.focusItem = target, target
			err = f.browse(false)
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(m.Out, "Menu closed. Completed changes remain saved.")
			return nil
		}
		return err
	}}
	cmd.Example = "  devbox-neo edit .\n  devbox-neo edit . --name work --default\n  devbox-neo edit . --name work --config base --config ./project-config"
	cmd.Flags().BoolVar(&show, "show", false, "Show combined settings and their sources without editing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print --show or --config results as JSON; never prompt")
	cmd.Flags().StringArrayVar(&references, "config", nil, "Replace the entire selected config list, in order (repeatable; never additive)")
	cmd.Flags().BoolVar(&setDefault, "default", false, "Select this session as its folder's default without prompting")
	cmd.Flags().BoolVar(&clearDefault, "clear-default", false, "Clear the folder's default without selecting another session")
	return sessionNameFlag(cmd, name)
}

func combinedView(e *app.Engine, r store.Record) (resource.ConfigView, error) {
	resolved, err := e.CombinedConfiguration(r)
	if err != nil {
		return resource.ConfigView{}, err
	}
	return (resource.Service{Home: e.Store.Home}).ConfigurationView("session", r.Identity.Name, resolved)
}

func sourceChainMenu(m menu, e *app.Engine, r store.Record, back string) (saved bool, err error) {
	picker, err := newSourcePicker(m, e.Store.Home, r.Identity.Workspace)
	if err != nil {
		return saved, err
	}
	apply := func(sources []config.Reference) error {
		updated, err := e.UpdateSources(m.Context, r, sources)
		if err != nil {
			var actionable *commanderror.Error
			if !errors.As(err, &actionable) || actionable.Code != "sources_changed" {
				return err
			}
			latest, readErr := e.Store.Read(m.Context, r.Identity.Name)
			if readErr != nil {
				return readErr
			}
			if latest.ID != r.ID {
				return fmt.Errorf("session identity changed; select it again")
			}
			r = latest
			return m.report(err)
		}
		r = updated
		saved = true
		m.Notice("Saved selected configs.")
		return nil
	}
	err = m.Run(func() (cliui.Screen, error) {
		resolved, configErr := e.CombinedConfiguration(r)
		if configErr == nil {
			configErr = resolved.Settings.Validate()
		}
		actions := picker.chainActions(r.Sources, true, apply)
		actions = append(actions, cliui.Action{Label: "Show combined configuration", Run: func() (bool, error) {
			view, err := combinedView(e, r)
			if err != nil {
				return false, m.report(err)
			}
			return false, m.View("Combined configuration", func(out io.Writer) error { return printConfigView(out, view) })
		}})
		return cliui.Screen{Title: "Manage configs", Actions: actions, Back: back, Body: func(out io.Writer) error {
			writeMenuHint(out, "Session: "+r.Identity.LocalName)
			writeMenuHint(out, "Folder: "+displayCell(r.Identity.Workspace))
			if err := showSourceChain(m, e.Store.Home, r.Identity.Workspace, r.Sources); err != nil {
				return err
			}
			if configErr != nil {
				return writeMenuHint(out, "Configuration error: "+displayCell(configErr.Error()))
			}
			return nil
		}}, nil
	})
	return saved, err
}
