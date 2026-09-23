package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func editCommand(factory engineFactory, name *string) *cobra.Command {
	var show, asJSON, setDefault, clearDefault bool
	cmd := &cobra.Command{Use: "edit <folder|session>", Short: "Edit a session's sources or its folder's default selection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if asJSON && !show {
			return fmt.Errorf("--json requires --show")
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
		if !show && !setDefault && !clearDefault && !interactive(cmd) {
			return fmt.Errorf("editing requires a terminal; use --name NAME --show to inspect or --name NAME --default to select without prompting")
		}
		e, err := factory(cmd)
		if err != nil {
			return err
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
		m := menu{ctx: cmd.Context(), in: promptReader(cmd), out: cmd.OutOrStdout(), cmd: cmd}
		for {
			var selected *store.Record
			if direct {
				r, err := e.Locate(cmd.Context(), args[0], *name)
				if err != nil {
					return err
				}
				selected = &r
			} else {
				var action folderEditAction
				selected, action, err = chooseSessionToEdit(m, e, args[0])
				if errors.Is(err, io.EOF) || (err == nil && selected == nil && action == folderEditNone) {
					return nil
				}
				if err != nil {
					return err
				}
				switch action {
				case folderEditSetDefault:
					chosen, err := chooseFolderDefault(m, e, args[0])
					if errors.Is(err, io.EOF) {
						return nil
					}
					if err != nil {
						return err
					}
					if chosen != nil {
						if err := e.SetDefault(cmd.Context(), *chosen); err != nil {
							return err
						}
						cmd.Printf("Default session for %s: %s\n", displayCell(chosen.Identity.Workspace), chosen.Identity.LocalName)
					}
					continue
				case folderEditClearDefault:
					workspace, err := e.ClearDefault(cmd.Context(), args[0])
					if err != nil {
						return err
					}
					cmd.Printf("Cleared default session for %s.\n", displayCell(workspace))
					continue
				}
			}
			back := "Back"
			if direct {
				back = "Done"
			}
			err = sourceChainMenu(m, e, *selected, back)
			if errors.Is(err, io.EOF) {
				cmd.Println("Menu closed. Completed changes remain saved.")
				return nil
			}
			if err != nil || direct {
				return err
			}
		}
	}}
	cmd.Flags().BoolVar(&show, "show", false, "Show combined settings and their sources without editing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print --show output as JSON")
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

func sourceChainMenu(m menu, e *app.Engine, r store.Record, back string) error {
	picker, err := newSourcePicker(m, e.Store.Home, r.Identity.Workspace)
	if err != nil {
		return err
	}
	for {
		if err := writeMenuTitle(m.out, "Manage config sources"); err != nil {
			return err
		}
		writeMenuHint(m.out, "Session: "+r.Identity.LocalName)
		writeMenuHint(m.out, "Folder: "+displayCell(r.Identity.Workspace))
		if err := showSourceChain(m, e.Store.Home, r.Identity.Workspace, r.Sources); err != nil {
			return err
		}
		if resolved, err := e.CombinedConfiguration(r); err != nil {
			writeMenuHint(m.out, "Configuration error: "+displayCell(err.Error()))
		} else if err := resolved.Settings.Validate(); err != nil {
			writeMenuHint(m.out, "Configuration error: "+displayCell(err.Error()))
		}
		actions := []string{"Add source"}
		if len(r.Sources) > 0 {
			actions = append(actions, "Replace source", "Remove source", "Reorder sources")
		}
		actions = append(actions, "Show combined configuration")
		choice, err := m.choose("What would you like to do?", actions, back)
		if err != nil || choice < 0 {
			return err
		}
		if actions[choice] == "Show combined configuration" {
			view, err := combinedView(e, r)
			if err != nil {
				fmt.Fprintf(m.out, "Error: %s\n", displayCell(err.Error()))
			} else if err := printConfigView(m.out, view); err != nil {
				return err
			}
			continue
		}
		sources, changed, err := editSourceChain(picker, r.Sources, actions[choice])
		if errors.Is(err, io.EOF) {
			return err
		}
		if err != nil {
			fmt.Fprintf(m.out, "Error: %s\n", displayCell(err.Error()))
			continue
		}
		if !changed {
			continue
		}
		updated, err := e.UpdateSources(m.ctx, r, sources)
		if err != nil {
			var actionable *commanderror.Error
			if !errors.As(err, &actionable) || actionable.Code != "sources_changed" {
				return err
			}
			fmt.Fprintf(m.out, "Error: %s\n", displayCell(err.Error()))
			latest, readErr := e.Store.Read(m.ctx, r.Identity.Name)
			if readErr != nil {
				return readErr
			}
			if latest.ID != r.ID {
				return fmt.Errorf("session identity changed; select it again")
			}
			r = latest
			continue
		}
		r = updated
		fmt.Fprintln(m.out, "Saved config sources.")
	}
}
