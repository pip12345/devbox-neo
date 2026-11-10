package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"devbox/internal/app"
	"github.com/spf13/cobra"
)

func validateBatchTarget(args []string, all bool, localName string) error {
	if all {
		if len(args) != 0 {
			return fmt.Errorf("--all does not accept an exact target")
		}
		if localName != "" {
			return fmt.Errorf("--name requires an explicit folder target")
		}
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("provide a target or --all")
	}
	return nil
}

type batchRow struct {
	Session string          `json:"session"`
	Status  app.BatchStatus `json:"status"`
	Reason  string          `json:"reason,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *errorReport    `json:"error,omitempty"`
}

type batchSummary struct {
	Succeeded int `json:"succeeded"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type batchOutput struct {
	Results []batchRow   `json:"results"`
	Summary batchSummary `json:"summary"`
}

func batchView[T any](cmd *cobra.Command, report app.BatchReport[T]) batchOutput {
	output := batchOutput{Results: []batchRow{}}
	for _, item := range report.Results {
		row := batchRow{Session: item.Session, Status: item.Status, Reason: item.Reason}
		if item.Result != nil {
			row.Result = item.Result
		}
		if item.Err != nil {
			detail := describeError(item.Err)
			if detail.Target == "" {
				detail.Target = item.Session
			}
			scopeErrorReport(cmd, &detail)
			row.Error = &detail
		}
		switch item.Status {
		case app.BatchSucceeded:
			output.Summary.Succeeded++
		case app.BatchUnchanged:
			output.Summary.Unchanged++
		case app.BatchSkipped:
			output.Summary.Skipped++
		case app.BatchFailed:
			output.Summary.Failed++
		}
		output.Results = append(output.Results, row)
	}
	return output
}

// Direct commands and foreground menu actions use the same report. The returned
// error preserves failure status after successful and partial work is displayed.
func presentBatch[T any](cmd *cobra.Command, report app.BatchReport[T], operationErr error, asJSON bool) (err error) {
	defer func() { err = errors.Join(operationErr, err) }()
	if len(report.Results) == 0 && operationErr != nil {
		return nil
	}
	output := batchView(cmd, report)
	if asJSON {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
			return err
		}
	} else {
		for _, row := range output.Results {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", displayCell(row.Session), row.Status); err != nil {
				return err
			}
			if row.Reason != "" {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), " (%s)", displayCell(row.Reason)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
				return err
			}
			if row.Error != nil {
				if _, err := fmt.Fprint(cmd.ErrOrStderr(), humanErrorText(*row.Error)); err != nil {
					return err
				}
			}
		}
		s := output.Summary
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Summary: %d succeeded, %d unchanged, %d skipped, %d failed.\n", s.Succeeded, s.Unchanged, s.Skipped, s.Failed); err != nil {
			return err
		}
	}
	return nil
}
