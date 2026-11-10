package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func successfulSessions[T any](report BatchReport[T]) []string {
	var sessions []string
	for _, item := range report.Results {
		if item.Status == BatchSucceeded {
			sessions = append(sessions, item.Session)
		}
	}
	return sessions
}

func TestRecreateAllContinuesAfterExecutionFailures(t *testing.T) {
	for _, failName := range []string{"a", "b", "c"} {
		t.Run(failName, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			var records []store.Record
			for _, name := range []string{"a", "b", "c"} {
				q.LocalName = name
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, sessionRecord(t, e, made.SessionID))
			}
			cause := &docker.ExitError{Operation: "rm", Code: 31}
			d.Fail = func(args []string) error {
				for _, r := range records {
					if r.Settings.LocalName == failName && slices.Equal(args, []string{"rm", r.Applied.ContainerID}) {
						return cause
					}
				}
				return nil
			}
			report, err := e.RecreateAll(ctx, false, RecreateOptions{ForceContainer: true})
			if !errors.Is(err, cause) || len(report.Results) != 3 || len(successfulSessions(report)) != 2 {
				t.Fatal("execution failure stopped independent sessions or lost its cause", report, err)
			}
			for _, r := range records {
				after := sessionRecord(t, e, r.ID)
				if (after.Applied.ContainerID == r.Applied.ContainerID) != (r.Settings.LocalName == failName) {
					t.Fatal("wrong session replaced", r.Settings.LocalName)
				}
				if r.Settings.LocalName == failName && !reflect.DeepEqual(r, after) {
					t.Fatal("failure before removal advanced the record")
				}
			}
		})
	}
}

func TestSessionBatchCancellationStopsCallbacksAndReleasesLocks(t *testing.T) {
	e, _, q := fixture(t)
	var records []store.Record
	for _, name := range []string{"a", "b", "c"} {
		q.LocalName = name
		made, err := e.Create(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, sessionRecord(t, e, made.SessionID))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	targets, err := e.selectSavedSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := newBatch[Result](targets)
	b.run(ctx, func(ctx context.Context, target SelectedTarget, _ *BatchResult[Result]) error {
		lock, r, err := e.lockSelectedSession(ctx, target)
		if err != nil {
			return err
		}
		defer lock.Close()
		calls++
		if !lock.Held() || lock.ID != r.ID {
			t.Fatal("callback did not own the selected session lock")
		}
		cancel()
		return nil
	})
	report, err := b.report, b.err(ctx)
	if !errors.Is(err, context.Canceled) || calls != 1 || len(report.Results) != 3 || len(successfulSessions(report)) != 1 {
		t.Fatal("cancellation lost completed work or continued processing", report, err, calls)
	}
	for _, item := range report.Results[1:] {
		if item.Status != BatchSkipped || item.Reason == "" {
			t.Fatal("cancelled work was not explicitly reported", item)
		}
	}
	for _, r := range records {
		check, done := context.WithTimeout(context.Background(), time.Second)
		lock, err := e.Store.Lock(check, r.ID)
		if err != nil {
			done()
			t.Fatal("batch leaked a session lock", err)
		}
		lock.Close()
		done()
	}
}

func TestBatchPreservesPartialResultsAndStopsCompletedTargets(t *testing.T) {
	cause := errors.New("phase failed")
	selectionErr := errors.New("selection failed")
	b := newBatch[int](selectedTargets{{name: "a"}, {name: "b"}, {name: "c"}, {name: "invalid", err: selectionErr}})
	ctx := context.Background()
	b.run(ctx, func(_ context.Context, target SelectedTarget, item *BatchResult[int]) error {
		if target.name == "invalid" {
			t.Fatal("executed failed selection")
		}
		item.Result = new(int)
		*item.Result = 1
		return nil
	})
	b.run(ctx, func(_ context.Context, target SelectedTarget, item *BatchResult[int]) error {
		*item.Result++
		if target.name == "a" {
			item.complete = true
		}
		if target.name == "b" {
			return cause
		}
		return nil
	})
	b.run(ctx, func(_ context.Context, target SelectedTarget, item *BatchResult[int]) error {
		if target.name != "c" {
			t.Fatal("completed or failed target advanced", target.name)
		}
		*item.Result++
		item.complete = true
		return nil
	})
	if !errors.Is(b.err(ctx), cause) || !errors.Is(b.err(ctx), selectionErr) {
		t.Fatal(b.err(ctx))
	}
	if *b.report.Results[0].Result != 2 || *b.report.Results[1].Result != 2 || *b.report.Results[2].Result != 3 {
		t.Fatal("phase transition lost partial results", b.report)
	}
}

func TestSessionBatchDoesNotExecuteWithUnavailableInventory(t *testing.T) {
	e, d, q := fixture(t)
	if _, err := e.Create(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(e.Store.Home, "sessions")
	if err := os.Rename(root, root+".saved"); err != nil {
		t.Fatal(err)
	}
	write(t, root, "not a directory")
	firstCall := len(d.History())
	report, err := e.RecreateAll(context.Background(), false, RecreateOptions{})
	if err == nil || len(report.Results) != 0 || len(d.History()) != firstCall {
		t.Fatal("batch executed without a trustworthy selection", report, err)
	}
}
