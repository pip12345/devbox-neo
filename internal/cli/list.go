package cli

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"devbox/internal/app"
	"devbox/internal/store"
)

func sortViews(views []app.View, by string) {
	sort.SliceStable(views, func(i, j int) bool {
		if by == "last-active" && !views[i].LastActivity.Equal(views[j].LastActivity) {
			return views[i].LastActivity.After(views[j].LastActivity)
		}
		return views[i].Name < views[j].Name
	})
}

// Paths and other external text can contain tabs, newlines, or terminal escapes.
// Quote those values so one container cannot forge rows or control the terminal.
func displayCell(value string) string {
	if value == "" {
		return "-"
	}
	if strings.IndexFunc(value, func(r rune) bool { return r < 32 || r >= 127 && r < 160 }) >= 0 {
		return strconv.QuoteToASCII(value)
	}
	return value
}

func activityAge(at, now time.Time) string {
	if at.IsZero() {
		return "-"
	}
	age := now.Sub(at)
	if age < time.Minute {
		return "just now"
	}
	for _, unit := range []struct {
		duration time.Duration
		name     string
	}{{365 * 24 * time.Hour, "year"}, {24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}} {
		if age >= unit.duration {
			n := int(age / unit.duration)
			suffix := "s"
			if n == 1 {
				suffix = ""
			}
			return fmt.Sprintf("%d %s%s ago", n, unit.name, suffix)
		}
	}
	return "just now"
}

func exactTime(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return at.UTC().Format(time.RFC3339)
}

func printSessionList(out io.Writer, views []app.View, wide bool, now time.Time) error {
	// Align plain cells before styling whole rows: tabwriter counts ANSI escapes
	// as visible text, which would otherwise shift columns on inactive rows.
	var table bytes.Buffer
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	header := "NAME\tHARNESS\tPROFILE\tLAST ACTIVE\tCONTAINER\tFOLDER"
	if wide {
		header += "\tLAST ACTION\tCREATED"
	}
	fmt.Fprintln(w, header)
	for _, view := range views {
		state := containerState(view)
		profile := viewProfile(view)
		activity := activityAge(view.LastActivity, now)
		if wide {
			activity = exactTime(view.LastActivity)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s", displayCell(view.Name), displayCell(view.Harness), displayCell(profile), activity, state, displayCell(view.Workspace))
		if wide {
			fmt.Fprintf(w, "\t%s\t%s", displayCell(view.LastAction), exactTime(view.CreatedAt))
		}
		fmt.Fprintln(w)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return printListRows(out, views, table.String())
}

func printUnmatchedContainers(out io.Writer, views []app.View) error {
	if len(views) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nWarning: managed containers with no session record:"); err != nil {
		return err
	}
	for _, view := range views {
		state := "stopped"
		if view.Running {
			state = "running"
		}
		if _, err := fmt.Fprintf(out, "  %s (%s)\n", displayCell(view.Name), state); err != nil {
			return err
		}
	}
	return nil
}

func containerState(view app.View) string {
	state := "missing"
	if view.Exists {
		state = "stopped"
		if view.Running {
			state = "running"
		}
	}
	if view.Error != "" {
		state += "!"
	}
	if view.Pending != nil {
		state += "*"
	}
	return state
}

func viewProfile(view app.View) string {
	if view.Profile == "" && view.SessionID != "" {
		return ".project"
	}
	if view.Project {
		return view.Profile + ".project"
	}
	return view.Profile
}

func printListRows(out io.Writer, views []app.View, table string) error {
	paint := terminalColors(out)
	lines := strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	for i, line := range lines {
		if i > 0 && !(views[i-1].Exists && views[i-1].Running) {
			line = paint.dim(line)
		}
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	for _, view := range views {
		if view.Error != "" {
			if _, err := fmt.Fprintf(out, "! %s: %s\n", displayCell(view.Name), displayCell(view.Error)); err != nil {
				return err
			}
		}
		if view.Pending != nil {
			p := view.Pending
			if _, err := fmt.Fprintf(out, "* %s: pending %s (%s): %s -> %s; retry the same transfer command.\n", displayCell(view.Name), displayCell(store.TransferCommand(p.Mode)), displayCell(p.Phase), displayCell(p.Source), displayCell(p.Destination)); err != nil {
				return err
			}
		}
	}
	return nil
}
