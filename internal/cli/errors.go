package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"devbox/internal/commanderror"
	"github.com/spf13/cobra"
)

type errorReport struct {
	Code      string              `json:"error"`
	Message   string              `json:"message"`
	Operation string              `json:"operation,omitempty"`
	Target    string              `json:"target,omitempty"`
	Next      []commanderror.Step `json:"next_steps,omitempty"`
	Related   []errorReport       `json:"related_errors,omitempty"`
	Partial   any                 `json:"partial_result,omitempty"`
}

// Partial results are presentation data supplied by the command that knows
// which work completed. They never replace the underlying failure or exit code.
type partialResultError struct {
	cause  error
	result any
}

func (e *partialResultError) Error() string { return e.cause.Error() }
func (e *partialResultError) Unwrap() error { return e.cause }

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
		_, writeErr = fmt.Fprint(cmd.ErrOrStderr(), humanErrorText(report))
	}
	if writeErr != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Could not write the error report.")
	}
	// Keep the foreground process's status even when cleanup also failed.
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

func humanErrorText(report errorReport) string {
	var out strings.Builder
	var write func(errorReport)
	write = func(r errorReport) {
		fmt.Fprintf(&out, "Error: %s\n", displayCell(r.Message))
		if r.Target != "" {
			fmt.Fprintf(&out, "Target: %s\n", displayCell(r.Target))
		}
		if len(r.Next) > 0 {
			fmt.Fprintf(&out, "\n%s", stepsText(r.Next))
		}
		for _, related := range r.Related {
			out.WriteByte('\n')
			write(related)
		}
	}
	write(report)
	return out.String()
}

func describeError(err error) errorReport {
	if partial, ok := err.(*partialResultError); ok {
		r := describeError(partial.cause)
		r.Partial = partial.result
		return r
	}
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
		r.Code, r.Message = "cancelled", "Cancelled."
	case errors.Is(err, context.DeadlineExceeded):
		r.Code, r.Message = "deadline_exceeded", "Operation timed out."
	case errors.As(err, &path):
		r.Code, r.Target = "path_unavailable", path.Path
		r.Message = "Cannot access path: " + path.Err.Error()
	}
	return r
}

func bindCommandErrors(root *cobra.Command) {
	help := func(cmd *cobra.Command) commanderror.Step {
		return commanderror.Step{Command: append(strings.Fields(cmd.CommandPath()), "--help"), Reason: "Usage"}
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// Flag parser errors can include a rejected --env value. Keep the cause
		// for programmatic inspection, not in the public message.
		return commanderror.New("invalid_flags", "Invalid command-line options.", "", err, help(cmd))
	})
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Args == nil && cmd.HasSubCommands() {
			entry := cmd.RunE
			// Own group validation before Cobra flattens an unknown command and
			// its suggestions into one multiline string. User text stays quoted;
			// known command suggestions use the normal structured step renderer.
			cmd.Args = cobra.ArbitraryArgs
			cmd.RunE = func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					if entry != nil {
						return entry(cmd, args)
					}
					return cmd.Help()
				}
				var steps []commanderror.Step
				if !cmd.DisableSuggestions {
					if cmd.SuggestionsMinimumDistance <= 0 {
						cmd.SuggestionsMinimumDistance = 2
					}
					for _, suggestion := range cmd.SuggestionsFor(args[0]) {
						argv := append(strings.Fields(cmd.CommandPath()), suggestion)
						steps = append(steps, commanderror.Step{Command: argv, Reason: "Did you mean"})
						if len(steps) == 3 {
							break
						}
					}
				}
				if len(steps) == 0 {
					steps = append(steps, help(cmd))
				}
				return commanderror.New("unknown_command", fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()), "", nil, steps...)
			}
		}
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
		if explicit && len(args) > 0 && args[0] == "dbx" {
			args = append([]string{args[0], "--home", home}, args[1:]...)
		}
		result = append(result, commanderror.Step{Command: args, Reason: step.Reason})
	}
	return result
}

func stepsText(steps []commanderror.Step) string {
	var out strings.Builder
	for i, step := range steps {
		if i > 0 {
			out.WriteByte('\n')
		}
		if step.Reason != "" {
			fmt.Fprintf(&out, "%s:\n", displayCell(step.Reason))
		}
		args := make([]string, len(step.Command))
		for i, arg := range step.Command {
			args[i] = shellQuote(arg)
		}
		fmt.Fprintf(&out, "  %s\n", strings.Join(args, " "))
	}
	return out.String()
}
