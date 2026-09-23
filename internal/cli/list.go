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
	"devbox/internal/config"
	"devbox/internal/store"
)

func sortViews(views []app.View, by string) {
	sort.SliceStable(views, func(i, j int) bool {
		if by == "folder" && views[i].Workspace != views[j].Workspace {
			return views[i].Workspace < views[j].Workspace
		}
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

// List headings start at the first output line; menu headings deliberately
// insert a blank line because they follow another screen or prompt.
func writeListTitle(out io.Writer, title string) error {
	return writeStyledConfigLine(out, "", title, "", configDisplayWidth(out), terminalColors(out).strong)
}

func printSessionList(out io.Writer, views []app.View, wide bool, now time.Time) error {
	return printSessionTable(out, views, wide, now, false)
}

func sourceSummary(sources []config.Reference) string {
	if len(sources) == 0 {
		return "None"
	}
	labels := make([]string, len(sources))
	for i, source := range sources {
		label := source.Label
		if source.Kind == config.ReferenceRelative {
			label = source.Path
			if label != "." && label != ".." && !strings.HasPrefix(label, "../") {
				label = "./" + label
			}
		}
		labels[i] = displayCell(label)
	}
	return strings.Join(labels, " → ")
}

func printSessionTable(out io.Writer, views []app.View, wide bool, now time.Time, local bool) error {
	// Align plain cells before styling whole rows: tabwriter counts ANSI escapes
	// as visible text, which would otherwise shift columns on inactive rows.
	var table bytes.Buffer
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	header := "NAME\tDEFAULT\tHARNESS\tLAST ACTIVE\tCONTAINER\tCONFIGS"
	if !local {
		header = "FOLDER\t" + header
	}
	if wide {
		header += "\tLAST ACTION\tCREATED"
	}
	fmt.Fprintln(w, header)
	for _, view := range views {
		state := containerState(view)
		name := view.Name
		if local && view.LocalName != "" {
			name = view.LocalName
		}
		marker := ""
		if view.Default {
			marker = "*"
		}
		activity := activityAge(view.LastActivity, now)
		if wide {
			activity = exactTime(view.LastActivity)
		}
		if !local {
			fmt.Fprintf(w, "%s\t", displayCell(view.Workspace))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s", displayCell(name), marker, displayCell(view.Harness), activity, state, sourceSummary(view.Sources))
		if wide {
			fmt.Fprintf(w, "\t%s\t%s", displayCell(view.LastAction), exactTime(view.CreatedAt))
		}
		fmt.Fprintln(w)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return printListRows(out, views, table.String(), true)
}

func printUnmatchedContainers(out io.Writer, views []app.View) error {
	if len(views) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Warning: managed containers with no session record:"); err != nil {
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

func printListRows(out io.Writer, views []app.View, table string, defaults bool) error {
	paint := terminalColors(out)
	lines := strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	for i, line := range lines {
		if i > 0 {
			view := views[i-1]
			style := func(text string) string { return text }
			if !(view.Exists && view.Running) {
				style = paint.dim
			}
			if index := strings.IndexByte(line, '*'); defaults && view.Default && index >= 0 {
				line = style(line[:index]) + paint.green("*") + style(line[index+1:])
			} else {
				line = style(line)
			}
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
