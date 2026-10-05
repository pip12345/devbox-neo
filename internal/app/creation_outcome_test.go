package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreationReportsConfirmedSaveIndependentlyOfCompletionErrors(t *testing.T) {
	for _, stage := range []string{"success", "build", "stop", "default", "both"} {
		t.Run(stage, func(t *testing.T) {
			e, d, q := fixture(t)
			q.MakeDefault = true
			failure := errors.New("injected failure")
			d.Fail = func(args []string) error {
				if stage == "build" && args[0] == "build" {
					return failure
				}
				if args[0] == "stop" {
					if stage == "default" || stage == "both" {
						write(t, filepath.Join(e.Store.Home, "state/folder-defaults.json"), "broken")
					}
					if stage == "stop" || stage == "both" {
						return failure
					}
				}
				return nil
			}
			result, err := e.Create(context.Background(), q)
			if result.Saved != (stage != "build") || (err != nil) != (stage != "success") {
				t.Fatal(stage, result, err)
			}
			if result.Saved {
				r, readErr := e.Store.Read(context.Background(), result.Session)
				if readErr != nil || r.ID != result.SessionID {
					t.Fatal("confirmed save has no matching session", r, readErr)
				}
			}
		})
	}
}
