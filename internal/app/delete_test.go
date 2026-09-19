package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDeleteSeparatesConfirmationAndSavedData(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		include, missing, dryRun   bool
		answers                    []bool
		containerGone, sessionGone bool
		prompts                    int
	}{
		{name: "unattended preserves state", containerGone: true},
		{name: "explicit unattended wipe", include: true, containerGone: true, sessionGone: true},
		{name: "decline container", answers: []bool{false}, prompts: 1},
		{name: "keep session", answers: []bool{true, false}, containerGone: true, prompts: 2},
		{name: "delete both", answers: []bool{true, true}, containerGone: true, sessionGone: true, prompts: 2},
		{name: "explicit scope skips prompts", include: true, answers: []bool{false}, containerGone: true, sessionGone: true},
		{name: "missing container prompt", missing: true, answers: []bool{true}, containerGone: true, sessionGone: true, prompts: 1},
		{name: "missing container default", missing: true, containerGone: true},
		{name: "missing container explicit", missing: true, include: true, containerGone: true, sessionGone: true},
		{name: "dry run", dryRun: true, include: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := record(t, e, created.Name)
			if tc.missing {
				d.Forget(created.Name)
			}
			p, _ := e.Store.RecordPath(created.Name)
			before := getFile(t, p)
			options := DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Scope: DeleteContainer, DryRun: tc.dryRun}
			if tc.answers != nil {
				options.Scope = ""
			}
			if tc.include {
				options.Scope = DeleteSession
			}
			prompts := []DeletePrompt{}
			if tc.answers != nil {
				options.Confirm = func(prompt DeletePrompt) (bool, error) {
					prompts = append(prompts, prompt)
					if len(prompts) > len(tc.answers) {
						t.Fatal("unexpected prompt", prompt)
					}
					if len(prompts) == 2 {
						if _, exists := d.Snapshot(created.Name); exists {
							t.Fatal("saved-data prompt ran before container removal")
						}
					}
					return tc.answers[len(prompts)-1], nil
				}
			}
			result, err := e.Delete(ctx, options)
			if err != nil {
				t.Fatal(result, err)
			}
			if len(prompts) != tc.prompts {
				t.Fatal(prompts)
			}
			_, exists := d.Snapshot(created.Name)
			if exists == tc.containerGone {
				t.Fatal("wrong container outcome", result)
			}
			_, err = os.Stat(p)
			if os.IsNotExist(err) != tc.sessionGone {
				t.Fatal("wrong session outcome", result, err)
			}
			if !tc.sessionGone && !tc.dryRun && len(result.Retained) != 1 {
				t.Fatal("retention not reported", result)
			}
			if tc.sessionGone && (len(result.Sessions) != 1 || len(result.Retained) != 0) {
				t.Fatal("deletion not reported", result)
			}
			if tc.dryRun && !reflect.DeepEqual(before, getFile(t, p)) {
				t.Fatal("dry run changed record")
			}
			if _, tagged := d.Images[r.ImageTag]; tagged == tc.sessionGone {
				t.Fatal("image retention did not follow session deletion")
			}
		})
	}
}

func TestDeleteFailureNeverDeletesSession(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.Store.RecordPath(created.Name)
	d.Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "rm" {
			return errors.New("remove failed")
		}
		return nil
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Scope: DeleteSession}); err == nil {
		t.Fatal("ignored remove failure")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("failure removed session", err)
	}
}

func TestDeleteForceNeverImpliesSavedDataDeletion(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, _ := e.Store.Lock(ctx, created.Name)
	lease, err := lock.Lease("exec")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	options := DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Force: true, Scope: DeleteSession}
	if _, err := e.Delete(ctx, options); err == nil {
		t.Fatal("force bypassed saved-state idle protection")
	}
	if _, exists := d.Snapshot(created.Name); !exists {
		t.Fatal("explicit deletion mutated before saved-state preflight")
	}
	options.Scope = DeleteContainer
	result, err := e.Delete(ctx, options)
	if err != nil || len(result.Containers) != 1 || len(result.Sessions) != 0 {
		t.Fatal(result, err)
	}
	if _, err := e.Store.Read(ctx, created.Name); err != nil {
		t.Fatal("force lost saved state", err)
	}
	lock, _ = e.Store.Lock(ctx, created.Name)
	lock.Release(lease.ID)
	lock.Close()
}

func TestDeleteAllIncludesMissingSessionsAndPreflightsWholeSelection(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Forget(second.Name)
	p, _ := e.Store.RecordPath(second.Name)
	original := getFile(t, p)
	write(t, p, "broken")
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession}); err == nil {
		t.Fatal("ignored corrupt bulk target")
	}
	if _, exists := d.Snapshot(first.Name); !exists {
		t.Fatal("partial deletion before full preflight")
	}
	write(t, p, string(original))
	result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession})
	if err != nil || len(result.Containers) != 1 || len(result.Sessions) != 2 {
		t.Fatal(result, err)
	}
}

func TestDeleteCancellationAfterContainerKeepsSession(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Confirm: func(prompt DeletePrompt) (bool, error) {
		calls++
		if calls == 2 {
			return false, context.Canceled
		}
		return true, nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", created.Name, "session.json")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteKeepsEndpointLockAcrossBothPrompts(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Confirm: func(DeletePrompt) (bool, error) {
		calls++
		attempt, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()
		lock, err := e.Store.Lock(attempt, created.Name)
		if err == nil {
			lock.Close()
			t.Fatal("another operation acquired the endpoint during confirmation")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		return true, nil
	}})
	if err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
}

func TestDeleteExplicitSavedDataPreflightsImageAssociation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, created.Name)
	image := d.Images[r.ImageTag]
	image.ID = "different-image"
	d.Images[r.ImageTag] = image
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Scope: DeleteSession})
	if err == nil {
		t.Fatal("image reassociation accepted")
	}
	if _, exists := d.Snapshot(created.Name); !exists {
		t.Fatal("container removed before saved-state preflight")
	}
	if _, err := e.Store.Read(ctx, created.Name); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSavedDataRechecksContainerAfterConfirmation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	container, _ := d.Snapshot(created.Name)
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.Name}}, Confirm: func(prompt DeletePrompt) (bool, error) {
		if len(prompt.Containers) == 0 {
			// Direct Docker changes do not participate in Devbox's operation lock.
			d.SetContainer(container)
		}
		return true, nil
	}})
	if err == nil {
		t.Fatal("container absence was not rechecked")
	}
	if _, err := e.Store.Read(ctx, created.Name); err != nil {
		t.Fatal("session removed under a container", err)
	}
}

func TestDeleteUnmatchedContainerNeverAdoptsState(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, created.Name)
	p, _ := e.Store.RecordPath(created.Name)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(p), "preserve-uncommitted-data")
	write(t, marker, "keep")
	result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession})
	if err != nil || len(result.Containers) != 1 || len(result.Sessions) != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("orphan was adopted", err)
	}
	if string(getFile(t, marker)) != "keep" {
		t.Fatal("uncommitted state was deleted")
	}
	if _, exists := d.Images[r.ImageTag]; !exists {
		t.Fatal("unverifiable tag was deleted")
	}
}
