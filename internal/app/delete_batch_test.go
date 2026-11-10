package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/store"
)

func deletionBatchFixture(t *testing.T) (*Engine, []store.Record) {
	t.Helper()
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
	return e, records
}

func TestDeleteBatchKeepsFailuresLocalAndLocksAcrossPhases(t *testing.T) {
	for _, failIndex := range []int{0, 1, 2} {
		t.Run(string(rune('a'+failIndex)), func(t *testing.T) {
			e, records := deletionBatchFixture(t)
			ctx := context.Background()
			bad := records[failIndex]
			if err := e.SetDefault(ctx, bad); err != nil {
				t.Fatal(err)
			}
			cause := &docker.ExitError{Code: 29, Operation: "rm"}
			fake := e.Docker.Runner.(*dockertest.Daemon)
			fake.Fail = func(args []string) error {
				if slices.Equal(args, []string{"rm", bad.Applied.ContainerID}) {
					return cause
				}
				return nil
			}
			prompts := 0
			result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession, Confirm: func(prompt DeletePrompt) (bool, error) {
				prompts++
				for _, r := range records {
					attempt, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
					lock, lockErr := e.Store.Lock(attempt, r.ID)
					cancel()
					if lockErr == nil {
						lock.Close()
						t.Fatal("deletion released a target lock during confirmation")
					}
					if !errors.Is(lockErr, context.DeadlineExceeded) {
						t.Fatal(lockErr)
					}
				}
				if prompts == 1 && len(prompt.Containers) != 3 {
					t.Fatal(prompt)
				}
				if prompts == 2 && (len(prompt.RemovedContainers) != 2 || len(prompt.Sessions) != 2 || slices.Contains(prompt.Sessions, bad.PublicName())) {
					t.Fatal("saved-data prompt included failed work or overstated removals", prompt)
				}
				return true, nil
			}})
			if !errors.Is(err, cause) || len(result.Containers) != 2 || len(result.Sessions) != 2 || !slices.Equal(result.Retained, []string{bad.PublicName()}) || prompts != 2 {
				t.Fatal(result, err, prompts)
			}
			if len(result.Batch.Results) != 3 {
				t.Fatal(result.Batch)
			}
			for _, item := range result.Batch.Results {
				if item.Session == bad.PublicName() {
					if item.Status != BatchFailed || item.Result.Container != "" || item.Result.RetainedSession != bad.PublicName() {
						t.Fatal(item)
					}
				} else if item.Status != BatchSucceeded || item.Result.Container == "" || item.Result.Session == "" {
					t.Fatal("independent target did not complete", item)
				}
			}
			for _, r := range records {
				_, readErr := e.Store.Read(ctx, r.ID)
				if (readErr == nil) != (r.ID == bad.ID) {
					t.Fatal("wrong saved-state outcome", r.ID, readErr)
				}
				check, cancel := context.WithTimeout(ctx, time.Second)
				lock, lockErr := e.Store.Lock(check, r.ID)
				if lockErr != nil {
					cancel()
					t.Fatal("batch leaked a lock", lockErr)
				}
				lock.Close()
				cancel()
			}
			if selected, err := e.Store.ReadDefault(ctx, bad.Settings.Workspace); err != nil || selected == nil || selected.ID != bad.ID {
				t.Fatal("failed deletion lost its default", selected, err)
			}
		})
	}
}

func TestDeleteBatchContinuesAfterImageCleanupFailure(t *testing.T) {
	e, records := deletionBatchFixture(t)
	bad := records[1]
	cause := &docker.ExitError{Code: 37, Operation: "image"}
	d := e.Docker.Runner.(*dockertest.Daemon)
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"image", "rm", bad.Applied.ImageTag}) {
			return cause
		}
		return nil
	}
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession})
	if !errors.Is(err, cause) || len(result.Containers) != 3 || len(result.Sessions) != 3 || len(result.Retained) != 0 {
		t.Fatal("cleanup failure hid committed deletion or blocked other targets", result, err)
	}
	for _, item := range result.Batch.Results {
		if item.Result.Session == "" || item.Result.Container == "" || item.Result.RetainedSession != "" {
			t.Fatal("partial effects were lost", item)
		}
		if (item.Status == BatchFailed) != (item.Session == bad.PublicName()) {
			t.Fatal(item)
		}
	}
}

func TestDeleteBatchPreflightProtectsBusyTargetWithoutBlockingOthers(t *testing.T) {
	e, records := deletionBatchFixture(t)
	bad := records[1]
	lock, err := e.Store.Lock(context.Background(), bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Lease("exec"); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	before := sessionRecord(t, e, bad.ID)
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession, Force: true})
	if err == nil || len(result.Containers) != 2 || len(result.Sessions) != 2 {
		t.Fatal(result, err)
	}
	if after := sessionRecord(t, e, bad.ID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed saved-state preflight mutated the busy target")
	}
	if _, exists := sessionSnapshot(t, e, bad.ID); !exists {
		t.Fatal("force bypassed saved-state idleness")
	}
}

func TestDeleteBatchSelectionFailureDoesNotBlockExactTargets(t *testing.T) {
	e, _, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{made.SessionID, strings.Repeat("f", 12)}}, Scope: DeleteSession})
	if err == nil || len(result.Sessions) != 1 || result.Sessions[0] != made.Session || len(result.Batch.Results) != 2 {
		t.Fatal(result, err)
	}
}

func TestDeleteBatchCancellationKeepsPartialEffectsAndDoesNotPromptAgain(t *testing.T) {
	for _, scope := range []DeleteScope{DeleteContainer, DeleteSession} {
		t.Run(string(scope), func(t *testing.T) {
			e, records := deletionBatchFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := e.Docker.Runner.(*dockertest.Daemon)
			removed := false
			d.Fail = func(args []string) error {
				if args[0] == "rm" && !removed {
					removed = true
					cancel()
				}
				return nil
			}
			prompts := 0
			result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: scope, Confirm: func(prompt DeletePrompt) (bool, error) {
				prompts++
				if len(prompt.Sessions) > 0 {
					t.Fatal("prompted for saved data after cancellation")
				}
				return true, nil
			}})
			if !errors.Is(err, context.Canceled) || len(result.Containers) != 1 || len(result.Sessions) != 0 || prompts != 1 {
				t.Fatal(result, err, prompts)
			}
			for _, r := range records {
				if _, err := e.Store.Read(context.Background(), r.ID); err != nil {
					t.Fatal("cancellation removed saved state", err)
				}
			}
			for _, item := range result.Batch.Results {
				if item.Result != nil && item.Result.Container != "" && scope == DeleteContainer {
					if item.Status != BatchSucceeded {
						t.Fatal("completed target was relabelled as cancelled", item)
					}
				} else if item.Status != BatchSkipped || item.Reason == "" {
					t.Fatal(item)
				}
			}
		})
	}
}

func TestDeleteBatchFailedSharedOwnerRuntimeProtectsSavedState(t *testing.T) {
	e, records := deletionBatchFixture(t)
	owner := records[0]
	c, _ := sessionSnapshot(t, e, owner.ID)
	c.ID, c.Name = strings.Repeat("e", 64), "/extra-owned"
	d := e.Docker.Runner.(*dockertest.Daemon)
	d.SetContainer(c)
	cause := errors.New("extra runtime removal failed")
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"rm", c.ID}) {
			return cause
		}
		return nil
	}
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession})
	if !errors.Is(err, cause) || len(result.Containers) != 3 || len(result.Sessions) != 2 || !slices.Equal(result.Retained, []string{owner.PublicName()}) {
		t.Fatal("shared-owner failure deleted backing state or blocked unrelated sessions", result, err)
	}
	if _, err := e.Store.Read(context.Background(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot("extra-owned"); !exists {
		t.Fatal("failed runtime was unexpectedly removed")
	}
}

func TestDeleteBatchPreviewReportsValidTargetsWithoutMutation(t *testing.T) {
	e, records := deletionBatchFixture(t)
	bad := records[1]
	path, _ := e.Store.RecordPath(bad.ID)
	write(t, path, "broken")
	d := e.Docker.Runner.(*dockertest.Daemon)
	before := runtimeMutations(d)
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession, DryRun: true, Confirm: func(DeletePrompt) (bool, error) { t.Fatal("preview prompted"); return false, nil }})
	if err == nil || len(result.Sessions) != 2 || runtimeMutations(d) != before {
		t.Fatal(result, err)
	}
	for _, r := range records {
		if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", r.ID)); err != nil {
			t.Fatal("preview removed state", err)
		}
	}
}
