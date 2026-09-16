// devbox-migrate is deliberately separate from the ordinary runtime binary.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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
	f.BoolVar(&dry, "dry-run", false, "Read-only filesystem inventory; creates no report or locks")
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
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		count := 0
		for _, on := range []bool{dry, stage, merge, resume} {
			if on {
				count++
			}
		}
		if count == 0 {
			return cmd.Help()
		}
		if count != 1 {
			return fmt.Errorf("select exactly one of --dry-run, --stage, --merge, or --resume")
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
		merger.Progress = cmd.OutOrStdout()
		if resume {
			j, err := migration.Load(p)
			if err != nil {
				return err
			}
			if j.Merge != nil {
				j, err = merger.Resume(cmd.Context(), p)
			} else {
				j, err = migration.ResumeStage(cmd.Context(), p, runtime)
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
				return mergeMenu(cmd, merger, j, choices, pending)
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
		fmt.Fprintf(cmd.ErrOrStderr(), "Scanning %s (filesystem only; no data changes)...\n", safe(p.Source))
		v, err := migration.InventorySource(cmd.Context(), p)
		if err != nil {
			return err
		}
		if dry {
			return migration.Report(cmd.OutOrStdout(), v, nil)
		}
		if _, err := os.Lstat(p.Work); err == nil {
			return fmt.Errorf("staging already exists; inspect its report and use --resume for an interrupted run")
		} else if !os.IsNotExist(err) {
			return err
		}
		selection := migration.Selection{Skip: skip, Caches: caches, ExternalAuth: external}
		if !interactive {
			excluded, err := v.Select(selection)
			if reportErr := migration.Report(cmd.OutOrStdout(), v, &migration.Journal{Phase: "Staging preview", Excluded: excluded}); reportErr != nil {
				return reportErr
			}
			if err != nil {
				return err
			}
			if !confirm {
				return fmt.Errorf("non-interactive staging requires --confirm-stage and explicit --skip/--approve-external-auth decisions where needed")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Checking stopped source writers, then copying and verifying host-backed data...")
			j, err := migration.Stage(cmd.Context(), v, selection, runtime)
			if j != nil {
				err = errors.Join(err, migration.Report(cmd.OutOrStdout(), &j.Inventory, j))
			}
			return err
		}
		return stageMenu(cmd, runtime, v, selection)
	}
	return cmd
}

func printMigrationResult(cmd *cobra.Command, j *migration.Journal, err error) error {
	if j != nil {
		return errors.Join(err, migration.Report(cmd.OutOrStdout(), &j.Inventory, j))
	}
	return err
}

type menu struct {
	in  *bufio.Reader
	out io.Writer
}

func (m menu) line(prompt string) (string, error) {
	if _, err := fmt.Fprint(m.out, prompt); err != nil {
		return "", err
	}
	line, err := m.in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
func stageMenu(cmd *cobra.Command, runtime migration.SourceRuntime, v *migration.Inventory, s migration.Selection) error {
	m := menu{bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout()}
	fmt.Fprintf(m.out, "Migrate Devbox -> Neo\n\nSource       %s\nStaging      %s\nDestination  %s\n\n", safe(v.Paths.Source), safe(v.Paths.Work), safe(v.Paths.Destination))
	if _, err := os.Lstat(v.Paths.Destination); err == nil {
		fmt.Fprintln(m.out, "Destination already exists; its contents will not be changed.")
	}
	fmt.Fprintln(m.out, "This step stages host-backed files only. Merge requires a separate explicit approval.")
	fmt.Fprintln(m.out, "Existing Neo data and project files stay untouched. Keep the old CLI and source containers idle during copying.")
	for {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		fmt.Fprintln(m.out, "\n1. Review inventory and issues\n2. Choose what to stage\n3. Prepare supported items (review exclusions first)\n4. Rescan after manual fixes\n5. Cancel")
		choice, err := m.line("> ")
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			if err = migration.Report(m.out, v, nil); err != nil {
				return err
			}
		case "2":
			if err = choose(m, v, &s); err != nil {
				return err
			}
		case "3":
			proposed := s
			proposed.Skip = append([]string(nil), s.Skip...)
			for _, item := range v.Items {
				if len(item.Issues) > 0 && !has(proposed.Skip, item.Key) {
					proposed.Skip = append(proposed.Skip, item.Key)
				}
			}
			// Asking about external auth authorizes only copying to private staging,
			// not replacement of existing destination credentials.
			for _, item := range v.Items {
				if item.Kind != "auth" || has(proposed.Skip, item.Key) || has(proposed.ExternalAuth, item.Key) {
					continue
				}
				if item.Path == v.Paths.Source || strings.HasPrefix(item.Path, v.Paths.Source+string(filepath.Separator)) {
					continue
				}
				answer, err := m.line("Copy external " + safe(item.Key) + " from " + safe(item.Path) + "? [y/N] ")
				if err != nil {
					return err
				}
				if strings.EqualFold(answer, "y") {
					proposed.ExternalAuth = append(proposed.ExternalAuth, item.Key)
				} else {
					proposed.Skip = append(proposed.Skip, item.Key)
				}
			}
			excluded, err := v.Select(proposed)
			fmt.Fprintln(m.out, "\nStaging scope:")
			var total int64
			selected := 0
			for _, item := range v.Items {
				if reason := excluded[item.Key]; reason != "" {
					fmt.Fprintf(m.out, "  Skip %s (%s)\n", safe(item.Key), safe(reason))
				} else {
					fmt.Fprintf(m.out, "  Copy %s\n", safe(item.Key))
					total += item.Bytes
					selected++
				}
			}
			if err != nil {
				fmt.Fprintln(m.out, "Review required:", safe(err.Error()))
				continue
			}
			fmt.Fprintf(m.out, "\nSelected items: %d; portable bytes: %d\nCaches selected: %t\n", selected, total, proposed.Caches)
			fmt.Fprintln(m.out, "Project conversions are staged proposals, not live edits. Review container-only config capture and destination conflicts with --merge.")
			answer, err := m.line("Prepare this scope, including the listed exclusions? [y/N] ")
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") {
				continue
			}
			for {
				fmt.Fprintln(m.out, "Checking stopped source writers, then copying and verifying host-backed data...")
				j, stageErr := migration.Stage(cmd.Context(), v, proposed, runtime)
				if j != nil {
					return errors.Join(stageErr, migration.Report(m.out, &j.Inventory, j))
				}
				if stageErr == nil {
					return nil
				}
				fmt.Fprintln(m.out, "Cannot stage:", safe(stageErr.Error()))
				answer, err = m.line("1. Recheck\n2. Back\n3. Cancel\n> ")
				if err != nil {
					return err
				}
				if answer == "1" {
					continue
				}
				if answer == "3" {
					fmt.Fprintln(m.out, "Cancelled.")
					return nil
				}
				break
			}
		case "4":
			next, err := migration.InventorySource(cmd.Context(), v.Paths)
			if err != nil {
				fmt.Fprintln(m.out, "Cannot rescan:", safe(err.Error()))
				continue
			}
			v = next
			kept := []string{}
			for _, key := range s.Skip {
				for _, item := range v.Items {
					if item.Key == key {
						kept = append(kept, key)
					}
				}
			}
			s.Skip = kept
			fmt.Fprintln(m.out, "Inventory refreshed; no destination or project changes.")
		case "5":
			fmt.Fprintln(m.out, "Cancelled.")
			return nil
		default:
			fmt.Fprintln(m.out, "Choose 1-5.")
		}
	}
}
func has(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func choose(m menu, v *migration.Inventory, s *migration.Selection) error {
	for {
		fmt.Fprintln(m.out, "\nToggle an item by number. Skipping an owner also skips dependent sessions.")
		for n, item := range v.Items {
			state := "include"
			if has(s.Skip, item.Key) {
				state = "skip"
			}
			fmt.Fprintf(m.out, "%d. [%s] %s (%s)\n", n+1, state, safe(item.Key), safe(item.Harness))
		}
		fmt.Fprintf(m.out, "c. Toggle caches (currently %t)\n0. Back\n", s.Caches)
		answer, err := m.line("> ")
		if err != nil {
			return err
		}
		if answer == "0" {
			return nil
		}
		if answer == "c" {
			s.Caches = !s.Caches
			continue
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(v.Items) {
			fmt.Fprintln(m.out, "Choose a listed number.")
			continue
		}
		key := v.Items[n-1].Key
		if !has(s.Skip, key) {
			s.Skip = append(s.Skip, key)
		} else {
			next := []string{}
			for _, old := range s.Skip {
				if old != key {
					next = append(next, old)
				}
			}
			s.Skip = next
		}
	}
}
