// devbox-migrate is deliberately separate from the ordinary runtime binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"devbox/internal/cliui"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	_, ttyErr := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	runtime := docker.Runtime{Runner: docker.ExecRunner{}}
	cmd := newCommand(migration.DockerSource{Runtime: runtime}, migration.Merger{Docker: runtime, Host: config.Snapshot(), UID: os.Getuid(), GID: os.Getgid()}, ttyErr == nil)
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", safe(err.Error()))
		os.Exit(1)
	}
}
func safe(s string) string {
	for _, r := range s {
		if r < 32 || r == 127 {
			return strconv.QuoteToASCII(s)
		}
	}
	return s
}
func newCommand(runtime migration.SourceRuntime, merger migration.Merger, interactive bool) *cobra.Command {
	var source, destination string
	var dry, stage, merge, resume, confirm, caches bool
	var skip, external []string
	var confirmMerge, capture, pending bool
	var choices migration.MergeChoices
	var renames []string
	cmd := &cobra.Command{Use: "devbox-migrate", Short: "Stage and explicitly merge an old Devbox home into Neo", SilenceUsage: true, SilenceErrors: true}
	f := cmd.Flags()
	f.StringVar(&source, "source", "", "Old home (default ~/.devbox; DEVBOX_HOME is ignored)")
	f.StringVar(&destination, "destination", "", "Neo home (default ~/.devbox-neo; staging uses its .migration sibling)")
	f.BoolVar(&dry, "dry-run", false, "Read-only metadata inventory; no payload scan, report file, or locks")
	f.Bool("verbose", false, "Show full inventory details; saved reports are always detailed")
	f.BoolVar(&stage, "stage", false, "Prepare converted host-backed data and report; does not merge")
	f.BoolVar(&merge, "merge", false, "Review and explicitly merge a prepared import")
	f.BoolVar(&resume, "resume", false, "Resume an already approved staging or merge operation")
	f.BoolVar(&confirm, "confirm-stage", false, "Explicitly approve the printed staging scope in non-interactive use")
	f.BoolVar(&caches, "caches", false, "Include mapped optional caches (otherwise excluded)")
	f.StringArrayVar(&skip, "skip", nil, "Skip an exact inventory key and its dependents (repeatable)")
	f.StringArrayVar(&external, "approve-external-auth", nil, "Approve an external auth item, e.g. auth:pi (repeatable)")
	f.BoolVar(&confirmMerge, "confirm-merge", false, "Approve the exact printed merge plan for non-interactive use")
	f.BoolVar(&capture, "capture-config", false, "Capture selected stopped-container OpenCode config into private work state")
	f.BoolVar(&pending, "review-pending", false, "With --merge, review changed inputs of unfinished environments without resetting completed imports")
	f.StringVar(&choices.Global, "global", "", "Existing global configuration: keep (default) or import")
	f.StringArrayVar(&renames, "rename-profile", nil, "Rename an imported profile: old=new (repeatable)")
	f.StringArrayVar(&choices.Reuse, "reuse-profile", nil, "Explicitly reuse an existing profile by inventory key")
	f.StringArrayVar(&choices.Projects, "approve-project", nil, "Approve an exact project inventory key for config conversion")
	f.StringArrayVar(&choices.ReplaceAuth, "replace-auth", nil, "Approve replacing a staged auth inventory key, with backup")
	f.StringArrayVar(&choices.Accept, "accept-change", nil, "Accept an exact change key printed by merge review (repeatable)")
	f.StringArrayVar(&choices.OmitConfig, "omit-container-config", nil, "Explicitly omit container-only config for an exact session key")
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, args []string) (runErr error) {
		ui := cliui.New(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		cmd.SetContext(ui.Context)
		defer func() { runErr = errors.Join(runErr, ui.Finish()) }()
		count := 0
		for _, on := range []bool{dry, stage, merge, resume} {
			if on {
				count++
			}
		}
		if count == 0 && !interactive {
			return cmd.Help()
		}
		if count > 1 {
			return fmt.Errorf("select exactly one of --dry-run, --stage, --merge, or --resume")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		expand := func(s string) string {
			if s == "~" {
				return home
			}
			if strings.HasPrefix(s, "~/") {
				return filepath.Join(home, s[2:])
			}
			return s
		}
		if source == "" {
			source = filepath.Join(home, ".devbox")
		}
		if destination == "" {
			destination = filepath.Join(home, ".devbox-neo")
		}
		p, err := migration.NewPaths(expand(source), expand(destination))
		if err != nil {
			return err
		}
		if p.Source == home || p.Destination == home {
			return fmt.Errorf("the user home itself cannot be a migration endpoint")
		}
		oldDefault := filepath.Join(home, ".devbox")
		if p.Destination == oldDefault || strings.HasPrefix(p.Destination, oldDefault+string(filepath.Separator)) {
			return fmt.Errorf("Neo's destination cannot be the conventional ~/.devbox or its descendants")
		}
		if count == 0 {
			action, err := migrationMenu(ui, p)
			if pauseErr := ui.Pause(); pauseErr != nil {
				return errors.Join(err, pauseErr)
			}
			if err != nil || action == "" {
				return err
			}
			dry, stage, merge, resume = action == "preview", action == "stage", action == "merge", action == "resume"
		}
		mergeFlags := confirmMerge || capture || pending || choices.Global != "" || len(renames) > 0 || len(choices.Reuse) > 0 || len(choices.Projects) > 0 || len(choices.ReplaceAuth) > 0 || len(choices.Accept) > 0 || len(choices.OmitConfig) > 0
		if mergeFlags && !merge {
			return fmt.Errorf("merge decision flags require --merge")
		}
		if merge && (confirm || caches || len(external) > 0) {
			return fmt.Errorf("staging flags cannot change merge scope")
		}
		if pending && (capture || choices.Global != "" || len(renames) > 0 || len(choices.Reuse) > 0 || len(choices.Projects) > 0 || len(choices.ReplaceAuth) > 0 || len(choices.OmitConfig) > 0 || len(skip) > 0) {
			return fmt.Errorf("pending review cannot change selected owners or repeat publication")
		}
		if (dry || resume) && (confirm || caches || len(skip) > 0 || len(external) > 0) {
			return fmt.Errorf("selection and approval flags belong to --stage; --resume retains the recorded scope")
		}
		merger.Progress = cmd.OutOrStdout()
		if resume {
			j, err := migration.Load(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Continuing previously approved work from %s.\n", safe(p.Work))
			if j.Merge != nil {
				j, err = merger.Resume(cmd.Context(), p)
			} else {
				j, err = migration.ResumeStage(cmd.Context(), p, runtime, cmd.OutOrStdout())
			}
			return printMigrationResult(cmd, j, err)
		}
		if merge {
			j, err := migration.Load(p)
			if err != nil {
				return err
			}
			choices.Skip = skip
			choices.Rename = map[string]string{}
			for _, entry := range renames {
				old, name, ok := strings.Cut(entry, "=")
				if !ok || old == "" || name == "" {
					return fmt.Errorf("profile renames require old=new")
				}
				if _, exists := choices.Rename[old]; exists {
					return fmt.Errorf("duplicate profile rename")
				}
				choices.Rename[old] = name
			}
			if capture {
				j, err = merger.CaptureConfigs(cmd.Context(), p, choices)
				if err != nil {
					return printMigrationResult(cmd, j, err)
				}
			}
			if interactive {
				return mergeMenu(ui, cmd, merger, j, choices, pending)
			}
			var plan migration.MergePlan
			if pending {
				plan, err = merger.PendingPlan(cmd.Context(), j, choices.Accept)
			} else {
				plan, err = merger.Plan(cmd.Context(), j, choices)
			}
			if err != nil {
				return err
			}
			if err = migration.ReviewReport(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
			if err = plan.Ready(); err != nil {
				return err
			}
			if !confirmMerge {
				return fmt.Errorf("non-interactive merging requires --confirm-merge after reviewing and accepting the printed changes")
			}
			if pending {
				j, err = merger.Reapprove(cmd.Context(), p, plan)
				if err == nil {
					j, err = merger.Resume(cmd.Context(), p)
				}
			} else {
				j, err = merger.Apply(cmd.Context(), p, plan)
			}
			return printMigrationResult(cmd, j, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Discovering metadata in %s (no payload scan or data changes)...\n", safe(p.Source))
		v, err := migration.InventorySource(cmd.Context(), p)
		if err != nil {
			return err
		}
		if dry {
			return printInventory(cmd, v, nil)
		}
		if _, err := os.Lstat(p.Work); err == nil {
			return fmt.Errorf("staging already exists; inspect its report and use --resume for an interrupted run")
		} else if !os.IsNotExist(err) {
			return err
		}
		selection := migration.Selection{Skip: skip, Caches: caches, ExternalAuth: external}
		if !interactive {
			excluded, err := v.Select(selection)
			if reportErr := printInventory(cmd, v, &migration.Journal{Phase: "Staging preview", Excluded: excluded}); reportErr != nil {
				return reportErr
			}
			if err != nil {
				return err
			}
			if !confirm {
				return fmt.Errorf("non-interactive staging requires --confirm-stage and explicit --skip/--approve-external-auth decisions where needed")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Checking stopped source writers, then copying and verifying host-backed data...")
			j, err := migration.Stage(cmd.Context(), v, selection, runtime, cmd.OutOrStdout())
			if j != nil {
				err = errors.Join(err, printInventory(cmd, &j.Inventory, j))
			}
			return err
		}
		return stageMenu(ui, cmd, runtime, v, selection)
	}
	return cmd
}

func printInventory(cmd *cobra.Command, v *migration.Inventory, j *migration.Journal) error {
	detailed, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return err
	}
	if detailed {
		return migration.Report(cmd.OutOrStdout(), v, j)
	}
	return migration.CompactReport(cmd.OutOrStdout(), v, j)
}

func printMigrationResult(cmd *cobra.Command, j *migration.Journal, err error) error {
	if j != nil {
		return errors.Join(err, printInventory(cmd, &j.Inventory, j))
	}
	return err
}
