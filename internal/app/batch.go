package app

import (
	"context"
	"os"
	"sort"

	"devbox/internal/commanderror"
	"devbox/internal/store"
)

type BatchStatus string

const (
	BatchSucceeded BatchStatus = "succeeded"
	BatchUnchanged BatchStatus = "unchanged"
	BatchSkipped   BatchStatus = "skipped"
	BatchFailed    BatchStatus = "failed"
)

type BatchResult[T any] struct {
	Session string      `json:"session"`
	Status  BatchStatus `json:"status"`
	Reason  string      `json:"reason,omitempty"`
	Result  *T          `json:"result,omitempty"`
	Err     error       `json:"-"`
}

type BatchReport[T any] struct {
	Results []BatchResult[T] `json:"results"`
}

// BatchError retains individual causes and child exit statuses. Presentation
// uses the corresponding report, so one failure never hides completed work.
type BatchError struct{ Causes []error }

func (e *BatchError) Error() string {
	return "Some session operations failed or were cancelled. Completed changes are retained."
}
func (e *BatchError) Unwrap() []error { return e.Causes }

// Each callback owns one operation, including its preflight, under that
// session's lock. Do not retain locks or plans across sessions: independent work
// must not depend on acquiring or validating another target.
func runSessionBatch[T any](ctx context.Context, e *Engine, operation func(context.Context, *store.Locked, store.Record) (BatchResult[T], error)) (BatchReport[T], error) {
	report := BatchReport[T]{Results: []BatchResult[T]{}}
	entries, err := e.Store.Inventory(ctx)
	if err != nil {
		return report, err
	}
	target := func(entry store.Entry) string {
		if entry.Err == nil && entry.Record.ID != "" {
			return entry.Record.PublicName()
		}
		return entry.Name
	}
	sort.Slice(entries, func(i, j int) bool { return target(entries[i]) < target(entries[j]) })
	var failures []error
	for _, entry := range entries {
		// An allocation without a saved record is not a session. It remains
		// explicit cleanup's responsibility, not permission to create runtime.
		if entry.Pending == nil && os.IsNotExist(entry.Err) {
			continue
		}
		item := BatchResult[T]{Session: target(entry)}
		if ctx.Err() != nil {
			item.Status, item.Reason = BatchSkipped, "Not attempted: batch cancelled."
		} else {
			item, err = func() (BatchResult[T], error) {
				if entry.Pending != nil {
					return item, commanderror.New("pending_transfer", "Unfinished session transfer. Resume it first.", item.Session, nil, entry.Pending.RetryStep())
				}
				if entry.Err != nil {
					return item, entry.Err
				}
				lock, err := e.Store.Lock(ctx, entry.Record.ID)
				if err != nil {
					return item, err
				}
				defer lock.Close()
				r, err := loadSelected(lock, entry.Record)
				if err != nil {
					return item, err
				}
				result, err := operation(ctx, lock, r)
				result.Session = r.PublicName()
				return result, err
			}()
			if err != nil {
				item.Status, item.Err = BatchFailed, err
				failures = append(failures, err)
			} else if item.Status == "" {
				item.Status = BatchSucceeded
			}
		}
		report.Results = append(report.Results, item)
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	if len(failures) > 0 {
		return report, &BatchError{Causes: failures}
	}
	return report, nil
}
