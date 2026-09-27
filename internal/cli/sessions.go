package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"devbox/internal/app"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func sessionCommands(factory engineFactory, name *string) []*cobra.Command {
	var listJSON, wide bool
	var sortBy string
	list := &cobra.Command{Use: "list [folder]", Short: "List saved sessions, defaults, selected configs, and container status", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if sortBy != "folder" && sortBy != "name" && sortBy != "last-active" {
			return fmt.Errorf("unknown session sort %q: use folder, name, or last-active", sortBy)
		}
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		folder := ""
		if len(args) > 0 {
			folder = args[0]
		}
		report, err := e.List(cmd.Context(), folder)
		if err != nil {
			return err
		}
		sortViews(report.Sessions, sortBy)
		if listJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		if len(report.Sessions) == 0 {
			cmd.Println("No saved sessions. Use create <folder> to create one.")
		} else if folder != "" {
			if err := writeListTitle(cmd.OutOrStdout(), displayCell(report.Sessions[0].Workspace)); err != nil {
				return err
			}
			if err := printSessionTable(cmd.OutOrStdout(), report.Sessions, wide, time.Now(), true); err != nil {
				return err
			}
		} else if err := printSessionList(cmd.OutOrStdout(), report.Sessions, wide, time.Now()); err != nil {
			return err
		}
		if err := printDefaultErrors(cmd.OutOrStdout(), report.DefaultErrors); err != nil {
			return err
		}
		return printUnmatchedContainers(cmd.OutOrStdout(), report.UnmatchedContainers)
	}}
	list.Flags().BoolVar(&listJSON, "json", false, "Print saved sessions and inventory diagnostics as JSON")
	list.Flags().BoolVar(&wide, "wide", false, "Also show session IDs, container names, exact timestamps, and the last action")
	list.Flags().StringVar(&sortBy, "sort", "folder", "Sort sessions by folder, name, or last-active (newest first)")
	return []*cobra.Command{list, statusCommand(factory, name), deleteCommand(factory, name), transferCommand(factory, name), renameCommand(factory, name)}
}

func printDefaultErrors(out io.Writer, issues map[string]string) error {
	folders := make([]string, 0, len(issues))
	for folder := range issues {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	for _, folder := range folders {
		if err := writeConfigLine(out, "Warning: ", displayCell(folder)+" default: "+displayCell(issues[folder]), "  ", configDisplayWidth(out)); err != nil {
			return err
		}
	}
	return nil
}

func transferCommand(factory engineFactory, name *string) *cobra.Command {
	var options app.TransferOptions
	var asJSON, move bool
	description := "Copy session state to another folder or local name"
	cmd := &cobra.Command{Use: "copy <folder|session-id> [destination-folder]", Short: description, Long: description + ".\nBy default, keep the source and leave the destination stopped.\nWith --move, remove the source after the destination is ready and preserve its running intent.\nUse --as NAME to choose another destination name, including within the same folder.\nRetry the same command to resume an interrupted transfer.", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		e, err := factory(cmd)
		if err != nil {
			return err
		}
		options.Mode = "clone"
		if move {
			options.Mode = "relocate"
		}
		options.Source, options.LocalName = args[0], *name
		options.Destination = ""
		if len(args) == 2 {
			options.Destination = args[1]
		}
		result, err := e.Transfer(cmd.Context(), options)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		action := "Completed"
		if result.DryRun {
			action = "Would perform"
		}
		cmd.Printf("%s %s: %s -> %s\nSession: %s\nFolder: %s\n", action, store.TransferCommand(result.Mode), result.Source, result.Destination, result.LocalName, displayCell(result.Workspace))
		for i, source := range result.ResolvedSources {
			cmd.Printf("  %d. %-12s %s\n", i+1, displayCell(source.Label), displayCell(source.Path))
		}
		return nil
	}}
	cmd.Flags().BoolVar(&move, "move", false, "Remove the source after the destination is ready")
	cmd.Flags().StringVar(&options.As, "as", "", "Destination local name (default: preserve the source name)")
	cmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Preview without copying session data")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the transfer result as JSON")
	return sessionNameFlag(cmd, name)
}
