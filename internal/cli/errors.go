package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"github.com/spf13/cobra"
)

type errorReport struct {
	Code      string              `json:"error"`
	Message   string              `json:"message"`
	Operation string              `json:"operation,omitempty"`
	Target    string              `json:"target,omitempty"`
	Next      []commanderror.Step `json:"next_steps,omitempty"`
	Related   []errorReport       `json:"related_errors,omitempty"`
}

// Execute is the process presentation boundary. Handlers return errors rather
// than printing them, so JSON failures and human failures are each emitted once.
func Execute(ctx context.Context, root *cobra.Command) int {
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	if cmd == nil {
		cmd = root
	}
	return RenderError(cmd, err)
}

func RenderError(cmd *cobra.Command, err error) int {
	report := describeError(err)
	home, _ := cmd.Flags().GetString("home")
	var scope func(*errorReport)
	scope = func(r *errorReport) {
		r.Operation = cmd.CommandPath()
		r.Next = scopedSteps(cmd, r.Next, home)
		for i := range r.Related {
			scope(&r.Related[i])
		}
	}
	scope(&report)
	asJSON, _ := cmd.Flags().GetBool("json")
	var writeErr error
	if asJSON {
		writeErr = json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	} else {
		var print func(errorReport)
		print = func(r errorReport) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error [%s] during %s: %s\n", displayCell(r.Code), displayCell(r.Operation), displayCell(r.Message))
			if r.Target != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "Target: %s\n", displayCell(r.Target))
			}
			if len(r.Next) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "\nNext:\n%s", stepsText(r.Next))
			}
			for _, related := range r.Related {
				print(related)
			}
		}
		print(report)
	}
	if writeErr != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Could not write the error report.")
	}
	// Keep the foreground process's status even when cleanup also failed.
	var exit *docker.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}

func describeError(err error) errorReport {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		r := describeError(causes[0])
		for _, cause := range causes[1:] {
			r.Related = append(r.Related, describeError(cause))
		}
		return r
	}
	r := errorReport{Code: "command_failed", Message: err.Error()}
	var actionable *commanderror.Error
	var path *os.PathError
	switch {
	case errors.As(err, &actionable):
		r.Code, r.Target, r.Next = actionable.Code, actionable.Target, actionable.Next
	case errors.Is(err, context.Canceled):
		r.Code = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		r.Code = "deadline_exceeded"
	case errors.As(err, &path):
		r.Code, r.Target = "path_unavailable", path.Path
	}
	return r
}

func bindCommandErrors(root *cobra.Command) {
	help := func(cmd *cobra.Command) commanderror.Step {
		return commanderror.Step{Command: append(strings.Fields(cmd.CommandPath()), "--help"), Reason: "Check command usage"}
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// Flag parser errors can include a rejected --env value. Keep the cause
		// for programmatic inspection, not in the public message.
		return commanderror.New("invalid_flags", "Invalid command-line flags; check their names and values.", "", err, help(cmd))
	})
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if validate := cmd.Args; validate != nil {
			cmd.Args = func(cmd *cobra.Command, args []string) error {
				if err := validate(cmd, args); err != nil {
					return commanderror.New("invalid_arguments", err.Error(), "", err, help(cmd))
				}
				return nil
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"\\$`;&|<>()*?[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func scopedSteps(cmd *cobra.Command, steps []commanderror.Step, home string) []commanderror.Step {
	flag := cmd.Flag("home")
	explicit := flag != nil && flag.Changed
	result := make([]commanderror.Step, 0, len(steps))
	for _, step := range steps {
		args := append([]string(nil), step.Command...)
		if explicit && len(args) > 0 && args[0] == "devbox-neo" {
			args = append([]string{args[0], "--home", home}, args[1:]...)
		}
		result = append(result, commanderror.Step{Command: args, Reason: step.Reason})
	}
	return result
}

func stepsText(steps []commanderror.Step) string {
	var out strings.Builder
	for _, step := range steps {
		args := make([]string, len(step.Command))
		for i, arg := range step.Command {
			args[i] = shellQuote(arg)
		}
		fmt.Fprintf(&out, "  %s\n", strings.Join(args, " "))
	}
	return out.String()
}
