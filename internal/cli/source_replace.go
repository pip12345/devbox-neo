package cli

import (
	"encoding/json"
	"fmt"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"github.com/spf13/cobra"
)

type selectionResult struct {
	Name      string              `json:"name"`
	Workspace string              `json:"workspace"`
	Sources   []config.Reference  `json:"sources"`
	Next      []commanderror.Step `json:"next_steps"`
}

func replaceSessionConfigs(cmd *cobra.Command, e *app.Engine, target, localName string, inputs []string, asJSON bool) error {
	r, err := e.Locate(cmd.Context(), target, localName)
	if err != nil {
		return err
	}
	picker, err := newSourcePicker(menu{}, e.Store.Home, r.Settings.Workspace)
	if err != nil {
		return err
	}
	var sources []config.Reference
	for _, input := range inputs {
		ref, err := picker.capture(input)
		if err != nil {
			return err
		}
		sources = append(sources, ref)
	}
	updated, err := e.UpdateSources(cmd.Context(), r, sources)
	if err != nil {
		return err
	}
	result := selectionResult{Name: updated.ID, Workspace: updated.Settings.Workspace, Sources: updated.Settings.Sources,
		Next: scopedSteps(cmd, []commanderror.Step{commanderror.Next("Review pending changes", "status", updated.Directory)}, e.Store.Home)}
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Replaced selected configs for %s:\n", displayCell(result.Name)); err != nil {
		return err
	}
	for i, ref := range result.Sources {
		source, err := ref.Expand(result.Workspace)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s  %s\n", i+1, displayCell(ref.Label), displayCell(source.Path)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Selected configs saved; container changes may still be pending.\n%s", stepsText(result.Next))
	return err
}
