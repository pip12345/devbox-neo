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
		scope                      DeleteScope
		containerGone, sessionGone bool
		prompts                    int
	}{
		{name: "unattended preserves state", containerGone: true},
		{name: "explicit unattended wipe", include: true, containerGone: true, sessionGone: true},
		{name: "decline container", answers: []bool{false}, prompts: 1},
		{name: "keep session", answers: []bool{true, false}, containerGone: true, prompts: 2},
		{name: "delete both", answers: []bool{true, true}, containerGone: true, sessionGone: true, prompts: 2},
		{name: "scoped whole confirms", scope: DeleteSession, answers: []bool{true, true}, prompts: 2, containerGone: true, sessionGone: true},
		{name: "scoped container confirms only container", scope: DeleteContainer, answers: []bool{true}, prompts: 1, containerGone: true},
		{name: "scoped container declined", scope: DeleteContainer, answers: []bool{false}, prompts: 1},
		{name: "scoped whole container declined", scope: DeleteSession, answers: []bool{false}, prompts: 1},
		{name: "scoped whole history declined", scope: DeleteSession, answers: []bool{true, false}, prompts: 2, containerGone: true},
		{name: "scoped whole missing container", scope: DeleteSession, missing: true, answers: []bool{true}, prompts: 1, containerGone: true, sessionGone: true},
		{name: "scoped container missing", scope: DeleteContainer, missing: true, answers: []bool{}, containerGone: true},
		{name: "scoped dry run never confirms", scope: DeleteSession, dryRun: true, answers: []bool{}},
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
			r := sessionRecord(t, e, created.SessionID)
			if tc.missing {
				forgetSession(t, e, created.SessionID)
			}
			p, _ := e.Store.RecordPath(sessionRecord(t, e, created.SessionID).Directory)
			before := getFile(t, p)
			options := DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Scope: DeleteContainer, DryRun: tc.dryRun}
			if tc.answers != nil {
				options.Scope = ""
			}
			if tc.include {
				options.Scope = DeleteSession
			}
			if tc.scope != "" {
				options.Scope = tc.scope
			}
			prompts := []DeletePrompt{}
			if tc.answers != nil {
				options.Confirm = func(prompt DeletePrompt) (bool, error) {
					prompts = append(prompts, prompt)
					if len(prompts) > len(tc.answers) {
						t.Fatal("unexpected prompt", prompt)
					}
					if len(prompts) == 2 {
						if _, exists := sessionSnapshot(t, e, created.SessionID); exists {
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
			_, exists := sessionSnapshot(t, e, created.SessionID)
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
			if _, tagged := d.Images[r.Applied.ImageTag]; tagged == tc.sessionGone {
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
	p, _ := e.Store.RecordPath(sessionRecord(t, e, created.SessionID).Directory)
	d.Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "rm" {
			return errors.New("remove failed")
		}
		return nil
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Scope: DeleteSession}); err == nil {
		t.Fatal("ignored remove failure")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("failure removed session", err)
	}
}

func TestDeleteForceNeverImpliesSavedDataDeletion(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, _ := e.Store.Lock(ctx, sessionRecord(t, e, created.SessionID).Directory, sessionRecord(t, e, created.SessionID).ID)
	lease, err := lock.Lease("exec")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	prompts := 0
	options := DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Force: true, Scope: DeleteSession, Confirm: func(p DeletePrompt) (bool, error) {
		prompts++
		if len(p.Sessions) > 0 {
			t.Fatal("container scope asked about saved state")
		}
		return true, nil
	}}
	if _, err := e.Delete(ctx, options); err == nil {
		t.Fatal("force bypassed saved-state idle protection")
	}
	if _, exists := sessionSnapshot(t, e, created.SessionID); !exists || prompts != 0 {
		t.Fatal("explicit deletion mutated or confirmed before saved-state preflight")
	}
	options.Scope = DeleteContainer
	result, err := e.Delete(ctx, options)
	if err != nil || len(result.Containers) != 1 || len(result.Sessions) != 0 || prompts != 1 {
		t.Fatal(result, err)
	}
	if _, err := e.Store.Find(ctx, created.SessionID, nil); err != nil {
		t.Fatal("force lost saved state", err)
	}
	lock, _ = e.Store.Lock(ctx, sessionRecord(t, e, created.SessionID).Directory, sessionRecord(t, e, created.SessionID).ID)
	lock.Release(lease.ID)
	lock.Close()
}

func TestDeleteAllIncludesMissingSessionsAndPreflightsWholeSelection(t *testing.T) {
	e, _, q := fixture(t)
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
	forgetSession(t, e, second.SessionID)
	p, _ := e.Store.RecordPath(sessionRecord(t, e, second.SessionID).Directory)
	original := getFile(t, p)
	write(t, p, "broken")
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{All: true}, Scope: DeleteSession}); err == nil {
		t.Fatal("ignored corrupt bulk target")
	}
	if _, exists := sessionSnapshot(t, e, first.SessionID); !exists {
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
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Confirm: func(prompt DeletePrompt) (bool, error) {
		calls++
		if calls == 2 {
			return false, context.Canceled
		}
		return true, nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, created.SessionID).Directory, "session.json")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteKeepsEndpointLockAcrossBothPrompts(t *testing.T) {
	for _, scope := range []DeleteScope{"", DeleteSession} {
		t.Run(string(scope), func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Scope: scope, Confirm: func(DeletePrompt) (bool, error) {
				calls++
				attempt, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
				lock, err := e.Store.Lock(attempt, sessionRecord(t, e, created.SessionID).Directory, sessionRecord(t, e, created.SessionID).ID)
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
		})
	}
}

func TestDeleteExplicitSavedDataPreflightsImageAssociation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	image := d.Images[r.Applied.ImageTag]
	image.ID = "different-image"
	d.Images[r.Applied.ImageTag] = image
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Scope: DeleteSession})
	if err == nil {
		t.Fatal("image reassociation accepted")
	}
	if _, exists := sessionSnapshot(t, e, created.SessionID); !exists {
		t.Fatal("container removed before saved-state preflight")
	}
	if _, err := e.Store.Find(ctx, created.SessionID, nil); err != nil {
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
	container, _ := sessionSnapshot(t, e, created.SessionID)
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Confirm: func(prompt DeletePrompt) (bool, error) {
		if len(prompt.Containers) == 0 {
			// Direct Docker changes do not participate in Devbox's operation lock.
			d.SetContainer(container)
		}
		return true, nil
	}})
	if err == nil {
		t.Fatal("container absence was not rechecked")
	}
	if _, err := e.Store.Find(ctx, created.SessionID, nil); err != nil {
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
	r := sessionRecord(t, e, created.SessionID)
	p, _ := e.Store.RecordPath(sessionRecord(t, e, created.SessionID).Directory)
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
	if _, exists := d.Images[r.Applied.ImageTag]; !exists {
		t.Fatal("unverifiable tag was deleted")
	}
}
