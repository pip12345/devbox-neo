package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devbox/internal/config"
)

func ReviewReport(w io.Writer, p MergePlan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\nMerge plan (fresh destination: %t)\n", p.Fresh)
	for _, pub := range p.Publications {
		action := "Add"
		if pub.Before != "missing" {
			action = "Replace"
		}
		fmt.Fprintf(&b, "  %s %s -> %s\n", action, display(pub.Item), display(pub.Target))
		if pub.Backup != "" {
			fmt.Fprintf(&b, "    Backup: %s\n", display(pub.Backup))
		}
	}
	for _, job := range p.Sessions {
		fmt.Fprintf(&b, "  Import %s -> %s\n", display(job.Item), display(job.Identity.Name))
	}
	keys := []string{}
	for key := range p.Excluded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "  Skip %s (%s)\n", display(key), display(p.Excluded[key]))
	}
	for _, notice := range p.Notices {
		fmt.Fprintf(&b, "  Keep: %s\n", display(notice))
	}
	for _, warning := range p.Warnings {
		fmt.Fprintf(&b, "  Warning: %s\n", display(warning))
	}
	for _, r := range p.Reviews {
		state := "Needs acceptance"
		if r.Blocking {
			state = "Blocked"
		} else if contains(p.Choices.Accept, r.Key) {
			state = "Accepted"
		}
		fmt.Fprintf(&b, "\n[%s] %s\n  %s\n", state, display(r.Key), display(r.Message))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
func mergeProgress(w io.Writer, j *Journal) error {
	p := j.Merge.Plan
	if err := ReviewReport(w, p); err != nil {
		return err
	}
	complete, pending := 0, 0
	for _, job := range p.Sessions {
		if a := j.Merge.Attempts[job.Item]; a != nil && a.Phase == "done" {
			complete++
		} else {
			pending++
		}
	}
	if _, err := fmt.Fprintf(w, "\nImported sessions: %d\nPending sessions: %d\nOriginal home retained: %s\nReport: %s\n", complete, pending, display(j.Inventory.Paths.Source), display(filepath.Join(j.Inventory.Paths.Work, "report.txt"))); err != nil {
		return err
	}
	if j.Phase == "completed" {
		for _, job := range p.Sessions {
			if _, err := fmt.Fprintf(w, "Continue:\n  devbox-neo --home %s open %s --continue\n", shell(j.Inventory.Paths.Destination), shell(job.Identity.Name)); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprintf(w, "Resume:\n  devbox-migrate --source %s --destination %s --resume\nReview changed pending configuration:\n  devbox-migrate --source %s --destination %s --merge --review-pending\n", shell(j.Inventory.Paths.Source), shell(j.Inventory.Paths.Destination), shell(j.Inventory.Paths.Source), shell(j.Inventory.Paths.Destination))
	return err
}

// ConfigurationComparison shows schema field changes, not whole documents.
// Environment/auth and argument values are withheld even when the input file
// is malformed or contains fields unknown to the current schema.
func ConfigurationComparison(j *Journal, item Item, c MergeChoices) ([]string, error) {
	source := stagedItemRoot(j, item)
	target := ""
	switch item.Kind {
	case "global":
		target = filepath.Join(j.Inventory.Paths.Destination, "config.json")
	case "profile":
		name := item.Name
		if c.Rename[name] != "" {
			name = c.Rename[name]
		}
		if !config.Name.MatchString(name) {
			return nil, fmt.Errorf("invalid profile name")
		}
		source = filepath.Join(source, "config.json")
		target = filepath.Join(j.Inventory.Paths.Destination, "profiles", name, "config.json")
	case "project":
		source = filepath.Join(source, "config.json")
		target = filepath.Join(item.Path, "config.json")
	default:
		return nil, fmt.Errorf("this owner has no Devbox config comparison")
	}
	decode := func(path string) (map[string]json.RawMessage, error) {
		b, err := readRegular(context.Background(), path)
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		if err != nil {
			return nil, err
		}
		var value map[string]json.RawMessage
		if config.Decode(b, &value) != nil {
			return nil, fmt.Errorf("configuration cannot be compared: invalid JSON (values withheld)")
		}
		return value, nil
	}
	before, err := decode(target)
	if err != nil {
		return nil, err
	}
	after, err := decode(source)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := []string{}
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	public := map[string]bool{"version": true, "default_profile": true, "default_harness": true, "on_exit": true, "default_shell": true, "harness": true, "network": true, "host_network": true, "extra_networks": true, "extra_mounts": true, "extra_ports": true, "vscode": true, "ignore_project_overrides": true, "inherit_profile": true, "proxy": true}
	lines := []string{"Compare " + display(target) + " with staged source:"}
	for _, key := range ordered {
		if strings.TrimSpace(string(before[key])) == strings.TrimSpace(string(after[key])) {
			continue
		}
		render := func(raw json.RawMessage) string {
			if len(raw) == 0 {
				return "<absent>"
			}
			if !public[key] {
				return "<values withheld>"
			}
			return display(string(raw))
		}
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s", display(key), render(before[key]), render(after[key])))
	}
	if len(lines) == 1 {
		lines = append(lines, "  No field differences.")
	}
	return lines, nil
}
