package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

// frontend owns navigation only. Operations keep using app/resource/store, and
// existing synchronous editors share its single runner and terminal ownership.
type frontend struct {
	m         menu
	cmd       *cobra.Command
	engine    engineFactory
	e         *app.Engine
	s         *resource.Service
	focusItem string
	// Keep an explicitly opened folder visible even before it has sessions.
	knownFolder string
	sortBy      string
}

func runFrontend(cmd *cobra.Command, engine engineFactory, resources resourceFactory, configs bool) (err error) {
	out, ok := cmd.OutOrStdout().(*os.File)
	if !interactive(cmd) || !ok || !terminal(out) || os.Getenv("TERM") == "dumb" {
		return cmd.Help()
	}
	s, err := resources(cmd)
	if err != nil {
		return err
	}
	m := newMenu(cmd)
	defer func() { err = errors.Join(err, m.Finish()) }()
	f := frontend{m: m, cmd: cmd, engine: engine, s: s}
	return f.browse(configs)
}
func (f *frontend) loadEngine() error {
	if f.e != nil {
		return nil
	}
	var err error
	f.e, err = f.engine(f.cmd)
	return err
}
func (f *frontend) action(label, description string, run func() error) cliui.Action {
	return cliui.Action{Label: label, Description: description, Run: func() (bool, error) {
		err := run()
		if err != nil {
			return false, f.m.report(err)
		}
		return false, nil
	}}
}
func (f *frontend) browse(configs bool) error {
	if f.sortBy == "" {
		f.sortBy = "name"
	}
	return f.m.Run(func() (cliui.Screen, error) {
		page := cliui.Screen{Title: "Sessions", Back: "Exit", OnTab: func() (bool, error) { configs = !configs; return false, nil }, FocusItem: f.focusItem}
		f.focusItem = ""
		if configs {
			page.Title = "Configs"
			page.Collection = &cliui.Collection{Title: "Configs", Empty: "No configs yet."}
			entries, err := f.s.ListConfigs()
			if err != nil {
				f.m.Notice("Config inventory unavailable: " + displayCell(err.Error()))
			}
			for _, entry := range entries {
				entry := entry
				item := cliui.Item{Key: entry.Path, Label: entry.Name, Description: entry.Harness,
					Fields: []cliui.Field{{Label: "Directory", Value: entry.Path}, {Label: "Harness", Value: entry.Harness}},
					Open:   func() error { return f.m.report(f.editConfig(entry.Name)) }}
				if entry.Error != "" {
					item.Fields = append(item.Fields, cliui.Field{Label: "Error", Value: entry.Error, Warning: true})
				}
				page.Collection.Items = append(page.Collection.Items, item)
			}
			create := f.action("Create config", "", f.createConfig)
			create.Shortcut = "n"
			create.BreakBefore = true
			page.Actions = append(page.Actions, create, f.action("Edit a directory by path", "", func() error {
				path, ok, err := f.m.Text(cliui.TextRequest{Prompt: "Config name or directory: "})
				if err != nil || !ok {
					return err
				}
				return f.editConfig(path)
			}))
		} else {
			var report app.InventoryReport
			err := f.loadEngine()
			if err == nil {
				report, err = f.e.List(f.m.Context, "")
			}
			if err != nil {
				f.m.Notice("Inventory unavailable: " + displayCell(err.Error()))
			}
			page.Collection = f.sessionCollection(report, f.sortBy)
			if err != nil {
				page.Collection.Empty = "Session inventory unavailable."
			}
			for workspace, issue := range report.DefaultErrors {
				f.m.Notice(displayCell(workspace) + ": " + displayCell(issue))
			}
			create := f.action("Create session", "", f.createSession)
			create.Shortcut = "n"
			create.BreakBefore = true
			all := f.action("All-session operations", "Status, recreate and filtered deletion", f.allSessions)
			all.Shortcut = "a"
			page.Actions = append(page.Actions, create, all, f.action("Open folder by path", "", func() error {
				folder, ok, err := f.m.Text(cliui.TextRequest{Prompt: "Workspace folder: "})
				if err != nil || !ok {
					return err
				}
				return f.folder(folder)
			}), f.action("Sort sessions", f.sortBy, func() error {
				values := []string{"name", "last-active"}
				i, err := f.m.Select("Sort sessions within folders", []string{"Name", "Last active"}, "Back")
				if err == nil && i >= 0 {
					f.sortBy = values[i]
				}
				return err
			}))
		}
		refresh := f.action("Refresh", "Reload the current inventory", func() error { return nil })
		refresh.Shortcut = "r"
		page.Actions = append(page.Actions, refresh, f.action("Help and shell integration", "Commands, version and completion scripts", f.help))
		return page, nil
	})
}
func sessionFields(v app.View) []cliui.Field {
	fields := []cliui.Field{{Label: "Folder", Value: v.Workspace}, {Label: "Harness", Value: v.Harness},
		{Label: "Container", Value: strings.TrimRight(containerState(v), "!*"), Status: true}, {Label: "Lifetime", Value: lifetimeState(v)}}
	if v.Pending != nil {
		fields = append(fields, cliui.Field{Label: "Transfer", Value: "Pending — use Copy or move", Warning: true})
	}
	var sources []string
	for i, ref := range v.Sources {
		sources = append(sources, fmt.Sprintf("%d. %s", i+1, ref.Label))
	}
	fields = append(fields, cliui.Field{Label: "Configs", Value: strings.Join(sources, "\n")}, cliui.Field{Label: "Last active", Value: activityAge(v.LastActivity, time.Now())}, cliui.Field{Label: "Full name", Value: v.Name})
	if v.Error != "" {
		fields = append(fields, cliui.Field{Label: "Error", Value: v.Error, Warning: true})
	}
	return fields
}

func (f *frontend) sessionCollection(report app.InventoryReport, sortBy string) *cliui.Collection {
	collection := &cliui.Collection{Title: "Sessions", Empty: "No sessions yet."}
	groups := map[string][]app.View{}
	if f.knownFolder != "" {
		groups[f.knownFolder] = nil
	}
	var ungrouped []app.View
	for _, v := range report.Sessions {
		if v.Workspace == "" {
			ungrouped = append(ungrouped, v)
			continue
		}
		groups[v.Workspace] = append(groups[v.Workspace], v)
	}
	folders := make([]string, 0, len(groups))
	for folder := range groups {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	item := func(v app.View, depth int) cliui.Item {
		label := v.LocalName
		if label == "" {
			label = v.Name
		}
		state := strings.TrimRight(containerState(v), "!*")
		if v.Pending != nil {
			state = "transfer pending"
		}
		if v.Error != "" {
			state = "error"
		}
		return cliui.Item{Key: v.Name, Label: label, Description: v.Workspace + " " + v.Harness, Depth: depth,
			Status: state, Activity: activityAge(v.LastActivity, time.Now()), Selected: v.Default, Fields: sessionFields(v),
			Open: func() error { return f.m.report(f.session(v)) }}
	}
	for _, v := range ungrouped {
		collection.Items = append(collection.Items, item(v, 0))
	}
	for _, folder := range folders {
		rows := groups[folder]
		sortViews(rows, sortBy)
		selected := "None"
		for _, v := range rows {
			if v.Default {
				selected = v.LocalName
			}
		}
		fields := []cliui.Field{{Label: "Default", Value: selected}, {Label: "Sessions", Value: fmt.Sprint(len(rows))}}
		if issue := report.DefaultErrors[folder]; issue != "" {
			fields = append(fields, cliui.Field{Label: "Error", Value: issue, Warning: true})
		}
		collection.Items = append(collection.Items, cliui.Item{Key: folder, Label: folder, Folder: true, Fields: fields, Open: func() error { return f.m.report(f.folder(folder)) }})
		for _, v := range rows {
			collection.Items = append(collection.Items, item(v, 1))
		}
	}
	for _, v := range report.UnmatchedContainers {
		collection.Items = append(collection.Items, item(v, 0))
	}
	return collection
}
func (f *frontend) session(v app.View) error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	return f.m.Run(func() (cliui.Screen, error) {
		// Refresh visible facts without making desired-config health a prerequisite
		// for stop/delete or other repair operations.
		var navigation *cliui.Navigation
		if report, err := f.e.List(f.m.Context, ""); err == nil {
			navigation = &cliui.Navigation{Collection: *f.sessionCollection(report, f.sortBy), Key: v.Name}
			found := false
			for _, row := range append(report.Sessions, report.UnmatchedContainers...) {
				if row.Name == v.Name {
					v = row
					found = true
					break
				}
			}
			if !found {
				return cliui.Screen{}, fmt.Errorf("session %s no longer exists; refresh the browser", v.Name)
			}
		}
		run := func(title string, op func(context.Context) error) func() error {
			return func() error { return f.foreground(title, op) }
		}
		var actions []cliui.Action
		addGroup := func(label string, entries ...cliui.Action) {
			for _, entry := range entries {
				entry.Group = label
				actions = append(actions, entry)
			}
		}
		addGroup("Harness",
			f.action("Open", "Launch the recorded harness", run("Open", func(ctx context.Context) error { _, err := f.e.Open(ctx, app.Request{Workspace: v.Name}); return err })),
			f.action("Continue", "Resume the harness conversation", run("Continue", func(ctx context.Context) error {
				_, err := f.e.Open(ctx, app.Request{Workspace: v.Name, Continue: true})
				return err
			})),
			f.action("Open with options", "Continuation and one-off harness arguments", func() error { return f.openWithOptions(v.Name) }),
		)
		addGroup("Commands & access",
			f.action("Shell", "Attach to the configured shell", run("Shell", func(ctx context.Context) error { return f.e.Exec(ctx, v.Name, "", nil, true) })),
			f.action("Exec", "Run a command with exact arguments", func() error { return f.exec(v.Name) }),
			f.action("SSH", "Share an authenticated SSH connection", func() error { return f.ssh(v.Name) }),
		)
		addGroup("Inspect",
			f.action("Status", "Live state, active commands and pending changes", func() error { return f.status(v.Name) }),
			f.action("Logs", "", func() error { return f.logs(v.Name) }),
			f.action("Networks", "", func() error { return f.networks(v.Name) }),
		)
		addGroup("Container lifecycle",
			f.action("Start", "Keep running until stopped", run("Start", func(ctx context.Context) error { _, err := f.e.Start(ctx, v.Name, ""); return err })),
			f.action("Stop", "", func() error { return f.stop(v.Name) }),
			f.action("Recreate", "Replace the container using current settings", func() error { return f.recreate(v.Name) }),
		)
		addGroup("Manage session",
			f.action("Edit selected configs", sourceSummary(v.Sources), func() error { return f.editSession(v.Name) }),
			f.defaultAction(v),
			cliui.Action{Label: "Copy or move", Description: "Destination, name, preview and explicit transfer", Run: func() (bool, error) {
				moved, err := f.transfer(v.Name)
				if err != nil {
					return false, f.m.report(err)
				}
				return moved, nil
			}},
			cliui.Action{Label: "Delete", Description: "Container first; saved data/history separately", Danger: true, Run: func() (bool, error) {
				if err := f.delete([]string{v.Name}); err != nil {
					return false, f.m.report(err)
				}
				_, err := f.e.Store.Read(f.m.Context, v.Name)
				return os.IsNotExist(err), nil
			}},
		)
		label := v.LocalName
		if label == "" {
			label = v.Name
		}
		return cliui.Screen{Title: "Session · " + displayCell(label), Back: "Back", Actions: actions, Fields: []cliui.Field{{Label: "Folder", Value: v.Workspace}, {Label: "Harness", Value: v.Harness}, {Label: "Container", Value: strings.TrimRight(containerState(v), "!*"), Status: true}}, Navigation: navigation}, nil
	})
}
func (f *frontend) foreground(title string, operation func(context.Context) error) error {
	if err := f.m.Pause(); err != nil {
		return err
	}
	if err := f.m.Context.Err(); err != nil {
		return err
	}
	ctx, done := operationContext(f.m.Context)
	fmt.Fprintln(f.cmd.OutOrStdout(), "\n"+title)
	err := func() error {
		defer done()
		if input, ok := f.cmd.InOrStdin().(*os.File); ok && terminal(input) {
			return preserveTerminal(input, func() error { return operation(ctx) })
		}
		return operation(ctx)
	}()
	if err != nil {
		RenderError(f.cmd, err)
	}
	if f.m.Context.Err() != nil {
		return f.m.Context.Err()
	}
	reviewErr := f.m.ReviewOutput()
	if f.e != nil && f.m.Context.Err() == nil {
		report, refreshErr := f.e.List(f.m.Context, "")
		if refreshErr != nil {
			f.m.Notice("Could not refresh session state; displayed inventory may be stale: " + displayCell(refreshErr.Error()))
		} else {
			f.m.RefreshNavigation(*f.sessionCollection(report, f.sortBy))
		}
	}
	return errors.Join(err, reviewErr)
}
func (f *frontend) jsonView(title string, value any) error {
	return f.m.View(title, func(out io.Writer) error {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	})
}
func (f *frontend) status(target string) error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	if target == "" {
		report, err := f.e.StatusAll(f.m.Context, "")
		if err != nil {
			return err
		}
		return f.m.View("Status · all sessions", func(out io.Writer) error {
			if err := printStatusList(out, report.Sessions); err != nil {
				return err
			}
			if err := printDefaultErrors(out, report.DefaultErrors); err != nil {
				return err
			}
			return printUnmatchedContainers(out, report.UnmatchedContainers)
		})
	}
	details, err := f.e.Status(f.m.Context, target, "")
	if err != nil {
		return err
	}
	return f.m.View("Status", func(out io.Writer) error {
		return printStatusDetails(out, details, scopedSteps(f.cmd, []commanderror.Step{commanderror.Next("To apply changes", "recreate", details.Name)}, f.s.Home))
	})
}
func (f *frontend) editSession(name string) error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	record, err := f.e.Locate(f.m.Context, name, "")
	if err != nil {
		return err
	}
	saved, err := sourceChainMenu(f.m, f.e, record, "Back")
	if saved {
		f.m.Notice("Selected configs saved. Check status before applying container changes.")
		f.m.Receipt("Selected configs saved; container changes may still be pending.\n" + stepsText(scopedSteps(f.cmd, []commanderror.Step{commanderror.Next("Review pending changes", "status", name)}, f.s.Home)))
	}
	return err
}
func (f *frontend) defaultAction(v app.View) cliui.Action {
	label := "Make folder default"
	if v.Default {
		label = "Clear folder default"
	}
	a := f.action(label, "", func() error {
		if v.Default {
			return f.clearDefault(v.Workspace)
		}
		record, err := f.e.Store.Read(f.m.Context, v.Name)
		if err != nil {
			return err
		}
		if record.ID != v.SessionID {
			return fmt.Errorf("session identity changed; select it again")
		}
		err = f.e.SetDefault(f.m.Context, record)
		if err == nil {
			f.m.Receipt("Default session for " + displayCell(v.Workspace) + ": " + displayCell(v.LocalName))
		}
		return err
	})
	a.Hidden = v.SessionID == ""
	return a
}

func (f *frontend) clearDefault(folder string) error {
	workspace, err := f.e.ClearDefault(f.m.Context, folder)
	if err == nil {
		f.m.Receipt("Cleared default session for " + displayCell(workspace) + ".")
	}
	return err
}

func (f *frontend) folder(folder string) error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	workspace, err := environment.CanonicalWorkspace(folder)
	if err != nil {
		return err
	}
	f.knownFolder = workspace
	return f.m.Run(func() (cliui.Screen, error) {
		selected, defaultErr := f.e.Store.ReadDefault(f.m.Context, workspace)
		name := "None"
		if selected != nil {
			name = selected.Name
		}
		var navigation *cliui.Navigation
		if report, err := f.e.List(f.m.Context, ""); err == nil {
			navigation = &cliui.Navigation{Collection: *f.sessionCollection(report, f.sortBy), Key: workspace}
			for _, v := range report.Sessions {
				if v.Default && v.Workspace == workspace {
					name = v.LocalName
				}
			}
		}
		fields := []cliui.Field{{Label: "Default", Value: name}}
		if defaultErr != nil {
			fields = append(fields, cliui.Field{Label: "Error", Value: defaultErr.Error(), Warning: true})
		}
		clear := f.action("Clear folder default", "", func() error { return f.clearDefault(workspace) })
		clear.Hidden = selected == nil && defaultErr == nil
		return cliui.Screen{Title: workspace, Back: "Back", Fields: fields, Navigation: navigation, Actions: []cliui.Action{
			f.action("Create session here", "", func() error { return f.createSessionIn(workspace) }), clear,
		}}, nil
	})
}
func (f *frontend) createConfig() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, result, created, err := createConfig(f.m, f.s, "", cwd, home)
	if err != nil {
		for _, path := range result.Created {
			f.m.Notice("Created " + displayCell(path))
		}
		return err
	}
	if created {
		f.m.Notice("Created " + displayCell(filepath.Dir(result.Path)))
	}
	for _, warning := range result.Warnings {
		f.m.Notice("Warning: " + displayCell(warning))
	}
	return nil
}
func (f *frontend) editConfig(ref string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	owner, err := f.s.ConfigDirectory(ref, cwd, home)
	if err != nil {
		return err
	}
	changed := false
	err = configMenu(f.m, f.s, owner, &changed, "Back")
	if changed {
		f.m.Receipt(configSaveReceipt(f.cmd, f.s.Home, owner.Name))
	}
	return err
}
func (f *frontend) createSession() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return f.createSessionIn(cwd)
}

func (f *frontend) createSessionIn(folder string) error {
	return f.createSessionFromDraft(sessionCreationDraft{workspace: folder})
}

func (f *frontend) createSessionFromDraft(draft sessionCreationDraft) error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	workspace, err := environment.CanonicalWorkspace(draft.workspace)
	if err != nil {
		return err
	}
	picker, err := newSourcePicker(f.m, f.s.Home, workspace)
	if err != nil {
		return err
	}
	var result app.Result
	draft.workspace = workspace
	draft, proceed, err := sessionCreationMenu(picker, f.e, draft, func(draft sessionCreationDraft) (bool, error) {
		err := f.foreground("Create session", func(ctx context.Context) error {
			var err error
			result, err = f.e.Create(ctx, app.Request{Workspace: draft.workspace, LocalName: draft.name, Sources: draft.sources})
			return err
		})
		if err == nil {
			return true, nil
		}
		if f.m.Context.Err() != nil {
			return false, f.m.Context.Err()
		}
		var failure *commanderror.Error
		if errors.As(err, &failure) && failure.Code == "create_stop_failed" {
			f.m.Notice("Session created, but stopping it failed. Use Stop in the session menu.")
			return true, nil
		}
		return false, f.m.report(err)
	})
	if err != nil || !proceed {
		return err
	}
	// Creation never selects a default or launches the harness.
	f.focusItem = result.Name
	return f.session(app.View{Name: result.Name, LocalName: draft.name, Workspace: draft.workspace})
}
func (f *frontend) help() error {
	actions := []cliui.Action{f.action("Command reference", Version, func() error {
		return f.m.View("Commands", func(out io.Writer) error { _, err := io.WriteString(out, f.cmd.Root().UsageString()); return err })
	})}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		shell := shell
		actions = append(actions, f.action("Completion · "+shell, "Print the script in the normal terminal", func() error {
			return f.foreground("Completion script", func(context.Context) error {
				root := f.cmd.Root()
				return writeCompletionScript(root, f.cmd.OutOrStdout(), shell, !root.CompletionOptions.DisableDescriptions)
			})
		}))
	}
	return f.m.Run(func() (cliui.Screen, error) {
		return cliui.Screen{Title: "Help and shell integration", Back: "Back", Actions: actions}, nil
	})
}
