package cli

import (
	"fmt"
	"sort"
	"strings"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func statusViewFields(view app.View, group string) []cliui.Field {
	change := statusChange(view)
	fields := []cliui.Field{{Label: "Result", Value: change, Success: change == "No changes", Attention: sessionChangeLabel(view) != "", Warning: change == "Cannot check"}}
	for _, scope := range []struct {
		kind  environment.Scope
		label string
	}{{environment.ImageScope, "Image"}, {environment.ContainerScope, "Container"}, {environment.RuntimeScope, "Runtime"}} {
		var reasons []string
		for _, reason := range view.PendingInputChanges {
			if reason.Scope == scope.kind {
				reasons = append(reasons, reason.String())
			}
		}
		if len(reasons) > 0 {
			fields = append(fields, cliui.Field{Label: scope.label, Values: reasons})
		}
	}
	if view.Error != "" {
		fields = append(fields, cliui.Field{Label: "Error", Value: displayCell(view.Error), Warning: true})
	}
	if view.ConfigError != "" {
		fields = append(fields, cliui.Field{Label: "Config error", Value: displayCell(view.ConfigError), Warning: true})
	}
	if view.Pending != nil {
		p := view.Pending
		fields = append(fields,
			cliui.Field{Label: "Transfer", Value: displayCell(fmt.Sprintf("Pending %s (%s)", store.TransferCommand(p.Mode), p.Phase)), Attention: true},
			cliui.Field{Label: "Source", Value: displayCell(p.Source)},
			cliui.Field{Label: "Destination", Value: displayCell(p.Destination)},
			cliui.Field{Label: "Next", Value: "Retry the same transfer command."},
		)
	}
	if view.ImageMissing {
		fields = append(fields, cliui.Field{Label: "Image", Value: "Recorded image missing. Existing container access is unaffected. Recreate builds an image only if container replacement needs one.", Attention: true})
	}
	if !view.Exists && view.Error == "" && view.Pending == nil {
		fields = append(fields, cliui.Field{Label: "Container", Value: "Missing. Use explicit Recreate to rebuild from current config before accessing it.", Warning: true})
	}
	for i := range fields {
		fields[i].Group = group
	}
	return fields
}

func statusStepFields(view app.View, steps []commanderror.Step, group string) []cliui.Field {
	if !statusNeedsRecreate(view) {
		return nil
	}
	var fields []cliui.Field
	for _, step := range steps {
		command := strings.TrimSpace(stepsText([]commanderror.Step{{Command: step.Command}}))
		fields = append(fields,
			cliui.Field{Group: group, Label: "Action", Value: displayCell(step.Reason)},
			cliui.Field{Group: group, Label: "Command", Value: displayCell(command)},
		)
	}
	return fields
}

func statusDetailsScreen(details app.StatusDetails, steps []commanderror.Step) cliui.Screen {
	view := details.View
	name := view.LocalName
	if name == "" {
		name = view.Target
	}
	fields := statusViewFields(view, "Changes")
	fields = append(fields, statusStepFields(view, steps, "Next step")...)
	if details.DefaultError != "" {
		fields = append(fields, cliui.Field{Group: "Folder default", Label: "Error", Value: displayCell(details.DefaultError), Warning: true})
	}
	if r := details.Record; r != nil {
		fields = append(fields,
			cliui.Field{Group: "Session", Label: "Session", Value: displayCell(r.PublicName())},
			cliui.Field{Group: "Session", Label: "Folder", Value: displayCell(r.Settings.Workspace)},
			cliui.Field{Group: "Session", Label: "Local name", Value: displayCell(r.Settings.LocalName)},
			cliui.Field{Group: "Session", Label: "Storage", Value: displayCell(view.StoragePath)},
			cliui.Field{Group: "Session", Label: "Harness", Value: displayCell(view.Harness)},
			cliui.Field{Group: "Runtime", Label: "Container", Value: containerState(view), Status: true},
			cliui.Field{Group: "Runtime", Label: "Container ID", Value: displayCell(view.ContainerID)},
			cliui.Field{Group: "Runtime", Label: "Image ID", Value: displayCell(r.Applied.ImageID)},
		)
		if r.Settings.Workspace != r.Applied.Inputs.Container.Workspace {
			fields = append(fields, cliui.Field{Group: "Runtime", Label: "Applied folder", Value: displayCell(r.Applied.Inputs.Container.Workspace)})
		}
		lifetime := "automatic (stops after the last attached command)"
		if r.Settings.ManualStart {
			lifetime = "until stop (restarts with Docker)"
		}
		fields = append(fields,
			cliui.Field{Group: "Runtime", Label: "Lifetime", Value: lifetime},
			cliui.Field{Group: "Active commands", Label: "Count", Value: fmt.Sprint(len(details.Active))},
		)
		if len(details.Active) > 0 {
			var active []string
			for _, lease := range details.Active {
				active = append(active, fmt.Sprintf("%s (host PID %d, since %s)", displayCell(lease.Action), lease.Process.PID, exactTime(lease.Created)))
			}
			fields = append(fields, cliui.Field{Group: "Active commands", Label: "Attached", Values: active})
		}
	}
	return cliui.Screen{Title: "Status · " + displayCell(name), Summary: sessionSummary(view), Fields: fields, Back: "Back"}
}

func (f *frontend) statusOverviewScreen(report app.InventoryReport) cliui.Screen {
	var fields []cliui.Field
	if len(report.Sessions) == 0 {
		fields = append(fields, cliui.Field{Label: "Sessions", Value: "No matching saved environments."})
	}
	for _, view := range report.Sessions {
		group := sessionLabel(view)
		fields = append(fields, cliui.Field{Group: group, Label: "Container", Value: containerState(view), Status: true})
		fields = append(fields, statusViewFields(view, group)...)
		fields = append(fields, statusStepFields(view, statusRecreateSteps(f.cmd, view, f.s.Home), group)...)
		fields = append(fields,
			cliui.Field{Group: group, Label: "Session", Value: displayCell(view.Target)},
			cliui.Field{Group: group, Label: "Folder", Value: displayCell(view.Workspace)},
			cliui.Field{Group: group, Label: "Harness", Value: displayCell(view.Harness)},
		)
	}
	folders := make([]string, 0, len(report.DefaultErrors))
	for folder := range report.DefaultErrors {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	for _, folder := range folders {
		fields = append(fields,
			cliui.Field{Group: "Folder defaults", Label: "Folder", Value: displayCell(folder)},
			cliui.Field{Group: "Folder defaults", Label: "Error", Value: displayCell(report.DefaultErrors[folder]), Warning: true},
		)
	}
	for _, view := range report.UnmatchedContainers {
		fields = append(fields,
			cliui.Field{Group: "Unmatched containers", Label: "Container", Value: displayCell(view.Target)},
			cliui.Field{Group: "Unmatched containers", Label: "State", Value: containerState(view), Status: true},
			cliui.Field{Group: "Unmatched containers", Label: "Record", Value: "No saved session record", Warning: true},
		)
	}
	return cliui.Screen{Title: "Status · all sessions", Fields: fields, Back: "Back"}
}
