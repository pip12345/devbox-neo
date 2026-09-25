package main

import (
	"fmt"
	"io"

	"devbox/internal/cliui"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func mergeMenu(ui *cliui.Runner, cmd *cobra.Command, m migration.Merger, j *migration.Journal, c migration.MergeChoices, pending bool) error {
	reviewed := ""
	applied := false
	planNow := func() (migration.MergePlan, error) {
		if pending {
			return m.PendingPlan(cmd.Context(), j, c.Accept)
		}
		return m.Plan(cmd.Context(), j, c)
	}
	err := ui.Run(func() (cliui.Screen, error) {
		plan, err := planNow()
		if err != nil {
			return cliui.Screen{}, err
		}
		ownersBlocked, captureBlocked := "", ""
		if pending {
			ownersBlocked = "Pending review cannot change owner selection or repeat shared-file publications."
			captureBlocked = "Capture scope is fixed for an approved merge."
		}
		return cliui.Screen{Title: fmt.Sprintf("Merge review: %d publications, %d session imports. Existing sessions will not be overwritten.", len(plan.Publications), len(plan.Sessions)), Back: "Cancel", Prompt: "What would you like to do?", Actions: []cliui.Action{
			{Label: "Review full plan", Run: func() (bool, error) {
				return false, ui.View("Merge plan", func(out io.Writer) error { return migration.ReviewReport(out, plan) })
			}},
			{Label: "Resolve owner conflicts / choose exclusions", Blocked: ownersBlocked, Run: func() (bool, error) {
				if err := mergeDecisions(ui, j, &c); err != nil {
					return false, err
				}
				reviewed = ""
				c.Accept = nil
				return false, nil
			}},
			{Label: "Capture stopped-container OpenCode config", Blocked: captureBlocked, Run: func() (bool, error) {
				if err := ui.Pause(); err != nil {
					return false, err
				}
				var err error
				j, err = m.CaptureConfigs(cmd.Context(), j.Inventory.Paths, c)
				if err != nil {
					if j == nil {
						return false, err
					}
					ui.Notice("Capture failed: " + safe(err.Error()))
				}
				reviewed = ""
				c.Accept = nil
				return false, nil
			}},
			{Label: "Accept displayed behavior changes", Run: func() (bool, error) {
				if err := migration.ReviewReport(ui.Out, plan); err != nil {
					return false, err
				}
				for _, review := range plan.Reviews {
					if review.Blocking {
						ui.Notice("Resolve the blockers before accepting this plan.")
						return false, nil
					}
				}
				confirmed, err := ui.Confirm("Accept these exact displayed changes, including proxy removal and setup effects where listed? [y/N] ")
				if err != nil || !confirmed {
					return false, err
				}
				c.Accept = nil
				for _, review := range plan.Reviews {
					c.Accept = append(c.Accept, review.Key)
				}
				accepted, err := planNow()
				if err != nil {
					return false, err
				}
				reviewed = accepted.Fingerprint()
				return false, nil
			}},
			{Label: "Apply reviewed merge", Run: func() (bool, error) {
				if err := plan.Ready(); err != nil {
					ui.Notice("Not ready: " + safe(err.Error()))
					return false, nil
				}
				if len(plan.Reviews) > 0 && reviewed != plan.Fingerprint() {
					ui.Notice("Accept the current plan first; it may have changed since review.")
					return false, nil
				}
				if err := migration.ReviewReport(ui.Out, plan); err != nil {
					return false, err
				}
				fmt.Fprintln(ui.Out, "Keep both CLIs and direct writers idle. New containers will be built; setup code may change shared workspace or external resources.")
				confirmed, err := ui.Confirm("Apply this exact merge plan? [y/N] ")
				if err != nil || !confirmed {
					return false, err
				}
				if pending {
					j, err = m.Reapprove(cmd.Context(), j.Inventory.Paths, plan)
					if err == nil {
						j, err = m.Resume(cmd.Context(), j.Inventory.Paths)
					}
				} else {
					j, err = m.Apply(cmd.Context(), j.Inventory.Paths, plan)
				}
				applied = true
				return true, printMigrationResult(cmd, j, err)
			}},
		}}, nil
	})
	if err == nil && !applied {
		fmt.Fprintln(ui.Out, "Cancelled; no merge was applied.")
	}
	return err
}

func mergeDecisions(ui *cliui.Runner, j *migration.Journal, c *migration.MergeChoices) error {
	return ui.Run(func() (cliui.Screen, error) {
		page := cliui.Screen{Title: "Choose an owner. Skipping it also skips dependent imports.", Back: "Back"}
		for _, item := range j.Inventory.Items {
			if j.Excluded[item.Key] != "" {
				continue
			}
			page.Actions = append(page.Actions, cliui.Action{Label: safe(item.Key), Run: func() (bool, error) { return false, editOwner(ui, j, c, item) }})
		}
		return page, nil
	})
}
func editOwner(ui *cliui.Runner, j *migration.Journal, c *migration.MergeChoices, item migration.Item) error {
	if has(c.Skip, item.Key) {
		choice, err := ui.Select("What would you like to do?", []string{"Include again"}, "Back")
		if err == nil && choice == 0 {
			c.Skip = without(c.Skip, item.Key)
		}
		return err
	}
	comparison := func(out io.Writer) error {
		lines, err := migration.ConfigurationComparison(j, item, *c)
		if err != nil {
			_, writeErr := fmt.Fprintln(out, safe(err.Error()))
			return writeErr
		}
		for _, line := range lines {
			if _, err := fmt.Fprintln(out, line); err != nil {
				return err
			}
		}
		return nil
	}
	// An owner decision is one operation; the caller then reloads the owner list
	// and invalidates any previous merge approval, as before.
	var actions []cliui.Action
	change := func(label string, apply func()) cliui.Action {
		return cliui.Action{Label: label, Run: func() (bool, error) { apply(); return true, nil }}
	}
	skip := change("Skip owner and dependents", func() { c.Skip = append(c.Skip, item.Key) })
	var body func(io.Writer) error
	switch item.Kind {
	case "profile":
		actions = []cliui.Action{
			{Label: "Choose a different destination config name", Run: func() (bool, error) {
				initial := item.Name
				if name, ok := c.Rename[item.Name]; ok {
					initial = name
				}
				name, accepted, err := ui.Text(cliui.TextRequest{Prompt: "Destination config name (:back cancels): ", Initial: initial})
				if err != nil || !accepted {
					return true, err
				}
				if c.Rename == nil {
					c.Rename = map[string]string{}
				}
				c.Rename[item.Name] = name
				c.Reuse = without(c.Reuse, item.Key)
				return true, nil
			}},
			change("Reuse existing config after review", func() { c.Reuse = append(c.Reuse, item.Key) }), skip,
			{Label: "Compare configuration fields", Run: func() (bool, error) { return true, ui.View("Configuration comparison", comparison) }},
		}
	case "project":
		body = comparison
		actions = []cliui.Action{change("Approve exact project config edit (back up original)", func() { c.Projects = append(c.Projects, item.Key) }), skip}
	case "auth":
		actions = []cliui.Action{
			change("Keep existing auth", func() { c.ReplaceAuth = without(c.ReplaceAuth, item.Key) }),
			change("Replace with staged auth (affects existing sessions; back up original)", func() { c.ReplaceAuth = append(c.ReplaceAuth, item.Key) }), skip,
		}
	case "global":
		body = comparison
		actions = []cliui.Action{change("Keep the existing imported-global config", func() { c.Global = "keep" }), change("Import staged settings into imported-global", func() { c.Global = "import" })}
	case "session":
		actions = []cliui.Action{
			change("Skip this session", func() { c.Skip = append(c.Skip, item.Key) }),
			change("Explicitly omit its container-only OpenCode config", func() { c.OmitConfig = append(c.OmitConfig, item.Key) }),
		}
	default:
		confirmed, err := ui.Confirm("Skip this optional item? [y/N] ")
		if err == nil && confirmed {
			c.Skip = append(c.Skip, item.Key)
		}
		return err
	}
	return ui.Run(func() (cliui.Screen, error) {
		return cliui.Screen{Body: body, Prompt: "What would you like to do?", Actions: actions, Back: "Back"}, nil
	})
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
