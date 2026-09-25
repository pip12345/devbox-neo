package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/sshshare"
	"devbox/internal/store"
)

func (f *frontend) form(title string, build func() []cliui.Action) error {
	return f.m.Run(func() (cliui.Screen, error) { return cliui.Screen{Title: title, Back: "Back", Actions: build()}, nil })
}
func (f *frontend) text(label string, value *string, validate func(string) error) cliui.Action {
	return f.action(label, displayCell(*value), func() error {
		next, ok, err := f.m.Text(label+": ", validate)
		if err == nil && ok {
			*value = next
		}
		return err
	})
}
func (f *frontend) toggle(label string, value *bool) cliui.Action {
	a := f.action(label, "", func() error { *value = !*value; return nil })
	a.Checked = value
	return a
}
func (f *frontend) arguments(title string, args *[]string) error {
	return f.form(title, func() []cliui.Action {
		var actions []cliui.Action
		for i, arg := range *args {
			i := i
			arg := arg
			actions = append(actions, f.action(fmt.Sprintf("Argument %d", i+1), displayCell(arg), func() error {
				choice, err := f.m.Select("Argument", []string{"Edit value", "Remove"}, "Back")
				if err != nil || choice < 0 {
					return err
				}
				if choice == 1 {
					*args = slices.Delete(*args, i, i+1)
					return nil
				}
				value, ok, err := f.m.Text("Exact argument (empty is allowed): ", nil)
				if err == nil && ok {
					(*args)[i] = value
				}
				return err
			}))
		}
		actions = append(actions, f.action("Add argument", "Each entry is one exact argv value; no implicit shell parsing", func() error {
			value, ok, err := f.m.Text("Exact argument (empty is allowed): ", nil)
			if err == nil && ok {
				*args = append(*args, value)
			}
			return err
		}))
		actions = append(actions, f.action("Set arguments from JSON", "Paste an exact string array; escapes preserve tabs/newlines", func() error {
			var next []string
			_, accepted, err := f.m.Text("JSON array of arguments: ", func(value string) error {
				if err := json.Unmarshal([]byte(value), &next); err != nil {
					return err
				}
				if next == nil {
					return fmt.Errorf("enter a JSON string array; use [] to clear")
				}
				return nil
			})
			if err == nil && accepted {
				*args = next
			}
			return err
		}))
		return actions
	})
}
func (f *frontend) container(target string) error {
	return f.form("Container actions", func() []cliui.Action {
		return []cliui.Action{
			f.action("Start — keep running until stop", "Also sets keep-running intent on an already running container", func() error {
				return f.foreground("Start", func(ctx context.Context) error { _, err := f.e.Start(ctx, target, ""); return err })
			}),
			f.action("Stop", "Clear keep-running intent; force is always explicit", func() error {
				force := false
				return f.form("Stop container", func() []cliui.Action {
					return []cliui.Action{f.toggle("Force: allow interruption of active commands", &force), f.action("Stop container", "", func() error {
						return f.foreground("Stop", func(ctx context.Context) error { return f.e.Stop(ctx, target, "", force) })
					})}
				})
			}),
			f.action("Recreate", "Inspect changes and choose cache behavior", func() error { return f.recreate(target) }),
			f.action("Logs", "Tail count and optional streaming", func() error { return f.logs(target) }),
			f.action("Networks", "Inspect, export variables, connect and disconnect", func() error { return f.networks(target) }),
		}
	})
}
func (f *frontend) recreate(target string) error {
	noCache := false
	return f.form("Recreate", func() []cliui.Action {
		return []cliui.Action{
			f.action("Inspect pending changes", "", func() error { return f.status(target) }),
			f.toggle("Rebuild image without cache", &noCache),
			f.action("Recreate", "Replaces container-local changes; preserves saved session state", func() error {
				yes, err := f.m.Confirm("Recreate with current settings? Container-local changes will be lost. [y/N] ")
				if err != nil || !yes {
					return err
				}
				return f.foreground("Recreate", func(ctx context.Context) error {
					if target == "" {
						_, err := f.e.RecreateAll(ctx, noCache, app.Request{})
						return err
					}
					_, err := f.e.Recreate(ctx, app.Request{Workspace: target}, noCache)
					return err
				})
			}),
		}
	})
}
func validateTail(value string) error {
	if value == "all" {
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("use all or a non-negative count")
	}
	return nil
}
func (f *frontend) logs(target string) error {
	tail := "100"
	follow := false
	return f.form("Docker logs", func() []cliui.Action {
		return []cliui.Action{
			f.text("Tail", &tail, validateTail), f.toggle("Follow until interrupted", &follow),
			f.action("Read logs", "These are Docker logs, not attached harness output", func() error {
				return f.foreground("Docker logs", func(ctx context.Context) error { return f.e.Logs(ctx, target, "", follow, tail) })
			}),
		}
	})
}
func (f *frontend) networks(target string) error {
	return f.form("Networks", func() []cliui.Action {
		return []cliui.Action{
			f.action("Inspect networks", "Addresses, gateways and attachment facts", func() error {
				facts, err := f.e.NetworkFacts(f.m.Context, target, "")
				if err != nil {
					return err
				}
				return f.jsonView("Network facts", facts)
			}),
			f.action("Environment exports", "All values or one selected variable", func() error {
				facts, err := f.e.NetworkFacts(f.m.Context, target, "")
				if err != nil {
					return err
				}
				values := facts.Env()
				var keys []string
				for key := range values {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				actions := []cliui.Action{f.action("All exports", "", func() error {
					return f.m.View("Network environment", func(out io.Writer) error {
						for _, key := range keys {
							fmt.Fprintf(out, "export %s=%s\n", key, shellQuote(values[key]))
						}
						return nil
					})
				})}
				for _, key := range keys {
					key := key
					actions = append(actions, f.action(key, "", func() error {
						return f.m.View(key, func(out io.Writer) error { _, err := fmt.Fprintln(out, values[key]); return err })
					}))
				}
				return f.form("Network environment", func() []cliui.Action { return actions })
			}),
			f.action("Connect network", "Attach an existing secondary network", func() error { return f.networkChange(target, true) }),
			f.action("Disconnect network", "The primary network cannot be removed", func() error { return f.networkChange(target, false) }),
		}
	})
}
func (f *frontend) networkChange(target string, connect bool) error {
	name, ok, err := f.m.Text("Existing Docker network: ", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("enter a network name")
		}
		return nil
	})
	if err != nil || !ok {
		return err
	}
	return f.foreground("Change network attachment", func(ctx context.Context) error { return f.e.ChangeNetwork(ctx, target, "", name, connect) })
}
func (f *frontend) access(target string) error {
	return f.form("Access tools", func() []cliui.Action {
		return []cliui.Action{
			f.action("Open with options", "Continuation and one-off harness arguments", func() error {
				resume := false
				var harnessArgs, args []string
				return f.form("Launch options", func() []cliui.Action {
					return []cliui.Action{f.toggle("Continue previous conversation", &resume), f.action("Harness arguments", fmt.Sprint(harnessArgs), func() error { return f.arguments("Harness arguments", &harnessArgs) }), f.action("Trailing arguments", fmt.Sprint(args), func() error { return f.arguments("Arguments appended last", &args) }), f.action("Open", "Invocation-only options; nothing saved", func() error {
						return f.foreground("Open", func(ctx context.Context) error {
							_, err := f.e.Open(ctx, app.Request{Workspace: target, Continue: resume, HarnessArgs: harnessArgs, Args: args})
							return err
						})
					})}
				})
			}),
			f.action("Exec", "Executable and exact arguments; no implicit shell", func() error {
				executable := ""
				var args []string
				return f.form("Execute a command", func() []cliui.Action {
					run := f.action("Run command", "Terminal ownership returns after the command exits", func() error {
						return f.foreground("Exec", func(ctx context.Context) error {
							return f.e.Exec(ctx, target, "", append([]string{executable}, args...), false)
						})
					})
					if executable == "" {
						run.Blocked = "Enter an executable first."
					}
					return []cliui.Action{f.text("Executable", &executable, func(value string) error {
						if value == "" {
							return fmt.Errorf("enter an executable")
						}
						return nil
					}), f.action("Arguments", fmt.Sprint(args), func() error { return f.arguments("Command arguments", &args) }), run}
				})
			}),
			f.action("SSH connection sharing", "Foreground authenticated connection; Ctrl-C disconnects", func() error {
				destination := ""
				host := false
				return f.form("SSH sharing", func() []cliui.Action {
					connect := f.action("Connect", "Keep this foreground connection open while sharing", func() error {
						return f.foreground("SSH sharing", func(ctx context.Context) error {
							input, err := prepareSSH(f.cmd.InOrStdin(), f.cmd.ErrOrStderr(), destination, host)
							if err != nil {
								return err
							}
							return sshInteraction(ctx, input, f.cmd.ErrOrStderr(), f.e, target, "", destination, host)
						})
					})
					if destination == "" {
						connect.Blocked = "Enter a destination first."
					}
					return []cliui.Action{f.text("Destination", &destination, sshshare.Validate), f.toggle("Use host SSH master", &host), connect}
				})
			}),
		}
	})
}
func (f *frontend) transfer(target string) (moved bool, err error) {
	options := app.TransferOptions{Source: target, Mode: "clone"}
	move := false
	pending, err := f.e.Store.Pending(target)
	if err != nil {
		return false, err
	}
	if pending != nil {
		journal, err := f.e.Store.ReadTransfer(pending.Source)
		if err != nil {
			return false, err
		}
		if journal == nil {
			return false, fmt.Errorf("transfer changed; select it again")
		}
		options.Source, options.Destination, options.As = journal.Source.Name, journal.Destination.Workspace, journal.Destination.LocalName
		move = journal.Mode == "relocate"
	}
	err = f.form("Copy or move", func() []cliui.Action {
		options.Mode = "clone"
		if move {
			options.Mode = "relocate"
		}
		fields := []cliui.Action{
			f.text("Destination folder (empty keeps source folder)", &options.Destination, nil),
			f.text("Destination local name (empty preserves name)", &options.As, nil),
			f.toggle("Move: remove source after destination is ready", &move),
		}
		if pending != nil {
			for i := range fields {
				fields[i].Blocked = "Pending transfer: recorded endpoints and mode are pinned."
			}
		}
		return append(fields,
			f.action("Preview", "Rebased config references and destination identity", func() error {
				preview := options
				preview.DryRun = true
				result, err := f.e.Transfer(f.m.Context, preview)
				if err != nil {
					return err
				}
				return f.jsonView("Transfer preview", result)
			}),
			cliui.Action{Label: "Transfer", Description: "Preview exact endpoints, then confirm", Run: func() (bool, error) {
				q := options
				q.DryRun = true
				preview, err := f.e.Transfer(f.m.Context, q)
				if err != nil {
					return false, f.m.report(err)
				}
				yes, err := f.m.Confirm(fmt.Sprintf("%s: %s -> %s\nWorkspace/config files are not copied. Proceed? [y/N] ", store.TransferCommand(preview.Mode), displayCell(preview.Source), displayCell(preview.Destination)))
				if err != nil || !yes {
					return false, err
				}
				err = f.foreground("Transfer", func(ctx context.Context) error {
					result, err := f.e.Transfer(ctx, options)
					if err == nil {
						moved = result.Mode == "relocate"
						fmt.Fprintf(f.cmd.OutOrStdout(), "%s: %s -> %s\n", store.TransferCommand(result.Mode), result.Source, result.Destination)
					}
					return err
				})
				if err != nil {
					return false, f.m.report(err)
				}
				return true, nil
			}},
		)
	})
	return moved, err
}
func (f *frontend) allSessions() error {
	if err := f.loadEngine(); err != nil {
		return err
	}
	return f.form("All-session operations", func() []cliui.Action {
		return []cliui.Action{
			f.action("Status for all sessions", "", func() error { return f.status("") }),
			f.action("Recreate all", "Preflight and apply current settings", func() error { return f.recreate("") }),
			f.action("Bulk deletion", "Exact targets or intersecting filters", func() error { return f.delete(nil) }),
		}
	})
}
func (f *frontend) chooseDeleteTargets(targets *[]string) error {
	report, err := f.e.List(f.m.Context, "")
	if err != nil {
		return err
	}
	return f.form("Select deletion targets", func() []cliui.Action {
		var actions []cliui.Action
		for _, v := range append(report.Sessions, report.UnmatchedContainers...) {
			v := v
			selected := slices.Contains(*targets, v.Name)
			a := f.action(v.Name, displayCell(v.Workspace), func() error {
				if i := slices.Index(*targets, v.Name); i >= 0 {
					*targets = slices.Delete(*targets, i, i+1)
				} else {
					*targets = append(*targets, v.Name)
				}
				return nil
			})
			a.Checked = &selected
			actions = append(actions, a)
		}
		return actions
	})
}
func (f *frontend) delete(targets []string) error {
	options := app.DeleteOptions{Selection: app.Selection{Targets: slices.Clone(targets)}}
	age := ""
	scope := app.DeleteContainer
	bulk := len(targets) == 0
	return f.form("Delete environments", func() []cliui.Action {
		var actions []cliui.Action
		if bulk {
			actions = append(actions, f.action("Choose exact targets", fmt.Sprintf("%d selected", len(options.Selection.Targets)), func() error {
				if err := f.chooseDeleteTargets(&options.Selection.Targets); err != nil {
					return err
				}
				if len(options.Selection.Targets) > 0 {
					options.Selection.All = false
					options.Selection.Stopped = false
					options.Orphaned = false
					age = ""
					options.OlderThan = 0
				}
				return nil
			}))
			if len(options.Selection.Targets) == 0 {
				actions = append(actions, f.toggle("All saved environments and unmatched managed containers", &options.Selection.All), f.toggle("Stopped containers only", &options.Selection.Stopped), f.toggle("Missing containers only", &options.Orphaned), f.text("Inactive longer than (empty disables)", &age, func(value string) error {
					if value == "" {
						options.OlderThan = 0
						return nil
					}
					d, err := time.ParseDuration(value)
					if err != nil || d <= 0 {
						return fmt.Errorf("enter a positive duration, such as 720h")
					}
					options.OlderThan = d
					return nil
				}))
			}
		}
		actions = append(actions, f.toggle("Force: permit interrupting attached commands", &options.Force), f.action("Preview scope", string(scope), func() error {
			i, err := f.m.Select("Preview deletion scope", []string{"Container only", "Whole saved environment"}, "Back")
			if err == nil && i >= 0 {
				scope = app.DeleteContainer
				if i == 1 {
					scope = app.DeleteSession
				}
			}
			return err
		}))
		blocked := ""
		if len(options.Selection.Targets) == 0 && !options.Selection.All && !options.Selection.Stopped && !options.Orphaned && options.OlderThan == 0 {
			blocked = "Select exact targets or at least one filter."
		}
		preview := f.action("Preview deletion", "Read-only; execution rechecks the selection while locked", func() error {
			q := options
			q.Scope = scope
			q.DryRun = true
			result, err := f.e.Delete(f.m.Context, q)
			if err != nil {
				return err
			}
			return f.m.View("Deletion preview", func(out io.Writer) error { return printDeleteResult(out, result) })
		})
		preview.Blocked = blocked
		execute := cliui.Action{Label: "Delete…", Description: "Confirm containers and saved data separately", Run: func() (bool, error) {
			var result app.DeleteResult
			err := f.foreground("Delete environments", func(ctx context.Context) error {
				q := options
				q.Scope = ""
				confirmation := deletionConfirmation{ui: f.m.Runner, singleTarget: len(q.Selection.Targets) == 1}
				q.Confirm = confirmation.confirm
				var err error
				result, err = f.e.Delete(ctx, q)
				if printErr := printDeleteResult(f.cmd.OutOrStdout(), result); err == nil {
					err = printErr
				}
				return err
			})
			if err != nil {
				return false, f.m.report(err)
			}
			return len(result.Containers)+len(result.Sessions) > 0, nil
		}}
		execute.Danger = true
		execute.Blocked = blocked
		return append(actions, preview, execute)
	})
}
