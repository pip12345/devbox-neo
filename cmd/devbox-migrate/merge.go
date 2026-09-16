package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func mergeMenu(cmd *cobra.Command, m migration.Merger, j *migration.Journal, c migration.MergeChoices, pending bool) error {
	ui := menu{in: bufio.NewReader(cmd.InOrStdin()), out: cmd.OutOrStdout()}
	reviewed := ""
	for {
		var plan migration.MergePlan
		var err error
		if pending {
			plan, err = m.PendingPlan(cmd.Context(), j, c.Accept)
		} else {
			plan, err = m.Plan(cmd.Context(), j, c)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(ui.out, "\nMerge review: %d publications, %d session imports. Existing sessions will not be overwritten.\n", len(plan.Publications), len(plan.Sessions))
		fmt.Fprintln(ui.out, "1. Review full plan\n2. Resolve owner conflicts / choose exclusions\n3. Capture stopped-container OpenCode config\n4. Accept displayed behavior changes\n5. Apply reviewed merge\n6. Cancel")
		choice, err := ui.line("> ")
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			if err = migration.ReviewReport(ui.out, plan); err != nil {
				return err
			}
		case "2":
			if pending {
				fmt.Fprintln(ui.out, "Pending review cannot change owner selection or repeat shared-file publications.")
				continue
			}
			if err = mergeDecisions(ui, j, &c); err != nil {
				return err
			}
			reviewed = ""
			c.Accept = nil
		case "3":
			if pending {
				fmt.Fprintln(ui.out, "Capture scope is fixed for an approved merge.")
				continue
			}
			j, err = m.CaptureConfigs(cmd.Context(), j.Inventory.Paths, c)
			if err != nil {
				fmt.Fprintln(ui.out, "Capture failed:", safe(err.Error()))
				if j == nil {
					return err
				}
			}
			reviewed = ""
			c.Accept = nil
		case "4":
			if err = migration.ReviewReport(ui.out, plan); err != nil {
				return err
			}
			blocked := false
			for _, r := range plan.Reviews {
				if r.Blocking {
					blocked = true
				}
			}
			if blocked {
				fmt.Fprintln(ui.out, "Resolve the blockers before accepting this plan.")
				continue
			}
			answer, err := ui.line("Accept these exact displayed changes, including proxy removal and setup effects where listed? [y/N] ")
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") {
				continue
			}
			c.Accept = nil
			for _, r := range plan.Reviews {
				c.Accept = append(c.Accept, r.Key)
			}
			if pending {
				plan, err = m.PendingPlan(cmd.Context(), j, c.Accept)
			} else {
				plan, err = m.Plan(cmd.Context(), j, c)
			}
			if err != nil {
				return err
			}
			reviewed = plan.Fingerprint()
		case "5":
			if err = plan.Ready(); err != nil {
				fmt.Fprintln(ui.out, "Not ready:", safe(err.Error()))
				continue
			}
			if len(plan.Reviews) > 0 && reviewed != plan.Fingerprint() {
				fmt.Fprintln(ui.out, "Accept the current plan first; it may have changed since review.")
				continue
			}
			if err = migration.ReviewReport(ui.out, plan); err != nil {
				return err
			}
			fmt.Fprintln(ui.out, "Keep both CLIs and direct writers idle. New containers will be built; setup code may change shared workspace or external resources.")
			answer, err := ui.line("Apply this exact merge plan? [y/N] ")
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") {
				continue
			}
			if pending {
				j, err = m.Reapprove(cmd.Context(), j.Inventory.Paths, plan)
				if err == nil {
					j, err = m.Resume(cmd.Context(), j.Inventory.Paths)
				}
			} else {
				j, err = m.Apply(cmd.Context(), j.Inventory.Paths, plan)
			}
			return printMigrationResult(cmd, j, err)
		case "6":
			fmt.Fprintln(ui.out, "Cancelled; no merge was applied.")
			return nil
		default:
			fmt.Fprintln(ui.out, "Choose 1-6.")
		}
	}
}
func mergeDecisions(ui menu, j *migration.Journal, c *migration.MergeChoices) error {
	items := []migration.Item{}
	for _, item := range j.Inventory.Items {
		if j.Excluded[item.Key] == "" {
			items = append(items, item)
		}
	}
	for {
		fmt.Fprintln(ui.out, "\nChoose an owner. Skipping it also skips dependent imports.")
		for n, item := range items {
			fmt.Fprintf(ui.out, "%d. %s\n", n+1, safe(item.Key))
		}
		fmt.Fprintln(ui.out, "0. Back")
		answer, err := ui.line("> ")
		if err != nil {
			return err
		}
		if answer == "0" {
			return nil
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(items) {
			continue
		}
		item := items[n-1]
		if has(c.Skip, item.Key) {
			answer, err = ui.line("1. Include again\n2. Back\n> ")
			if err != nil {
				return err
			}
			if answer == "1" {
				c.Skip = without(c.Skip, item.Key)
			}
			continue
		}
		switch item.Kind {
		case "profile":
			fmt.Fprintln(ui.out, "1. Rename imported profile\n2. Reuse existing profile after review\n3. Skip owner and dependents\n4. Compare configuration fields\n5. Back")
			answer, err = ui.line("> ")
			if err != nil {
				return err
			}
			switch answer {
			case "1":
				name, e := ui.line("New profile name: ")
				if e != nil {
					return e
				}
				if c.Rename == nil {
					c.Rename = map[string]string{}
				}
				c.Rename[item.Name] = name
				c.Reuse = without(c.Reuse, item.Key)
			case "2":
				c.Reuse = append(c.Reuse, item.Key)
			case "3":
				c.Skip = append(c.Skip, item.Key)
			case "4":
				lines, e := migration.ConfigurationComparison(j, item, *c)
				if e != nil {
					fmt.Fprintln(ui.out, safe(e.Error()))
				} else {
					for _, line := range lines {
						fmt.Fprintln(ui.out, line)
					}
				}
			}
		case "project":
			lines, e := migration.ConfigurationComparison(j, item, *c)
			if e != nil {
				fmt.Fprintln(ui.out, safe(e.Error()))
			} else {
				for _, line := range lines {
					fmt.Fprintln(ui.out, line)
				}
			}
			answer, err = ui.line("1. Approve exact project config edit (back up original)\n2. Skip owner and dependents\n3. Back\n> ")
			if err != nil {
				return err
			}
			if answer == "1" {
				c.Projects = append(c.Projects, item.Key)
			} else if answer == "2" {
				c.Skip = append(c.Skip, item.Key)
			}
		case "auth":
			answer, err = ui.line("1. Keep existing auth\n2. Replace with staged auth (affects existing sessions; back up original)\n3. Skip owner and dependents\n4. Back\n> ")
			if err != nil {
				return err
			}
			if answer == "1" {
				c.ReplaceAuth = without(c.ReplaceAuth, item.Key)
			} else if answer == "2" {
				c.ReplaceAuth = append(c.ReplaceAuth, item.Key)
			} else if answer == "3" {
				c.Skip = append(c.Skip, item.Key)
			}
		case "global":
			lines, e := migration.ConfigurationComparison(j, item, *c)
			if e != nil {
				fmt.Fprintln(ui.out, safe(e.Error()))
			} else {
				for _, line := range lines {
					fmt.Fprintln(ui.out, line)
				}
			}
			answer, err = ui.line("1. Keep existing Neo global settings\n2. Import staged settings (affects existing environments)\n3. Back\n> ")
			if err != nil {
				return err
			}
			if answer == "1" {
				c.Global = "keep"
			} else if answer == "2" {
				c.Global = "import"
			}
		case "session":
			answer, err = ui.line("1. Skip this session\n2. Explicitly omit its container-only OpenCode config\n3. Back\n> ")
			if err != nil {
				return err
			}
			if answer == "1" {
				c.Skip = append(c.Skip, item.Key)
			} else if answer == "2" {
				c.OmitConfig = append(c.OmitConfig, item.Key)
			}
		default:
			answer, err = ui.line("Skip this optional item? [y/N] ")
			if err != nil {
				return err
			}
			if strings.EqualFold(answer, "y") {
				c.Skip = append(c.Skip, item.Key)
			}
		}
	}
}
func without(values []string, key string) []string {
	result := []string{}
	for _, value := range values {
		if value != key {
			result = append(result, value)
		}
	}
	return result
}
