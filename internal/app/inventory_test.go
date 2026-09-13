package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/environment"
)

func TestMissingContainersStillHaveConfigurationStatus(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		change       environment.Change
		invalid      bool
	}{
		{"unchanged", `{"version":1,"harness":"pi","on_exit":"stop"}`, environment.NoChange, false},
		{"changed", `{"version":1,"harness":"pi","network":"host"}`, environment.Recreate, false},
		{"invalid", "broken", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			d.Forget(created.Name)
			write(t, filepath.Join(e.Store.Home, "profiles", q.Profile, "config.json"), tc.config)
			view, err := e.Status(ctx, created.Name, "")
			if err != nil || view.Exists || view.Running || (view.ConfigError != "") != tc.invalid || view.Desired != tc.change {
				t.Fatal(view, err)
			}
			report, err := e.StatusAll(ctx, q.Profile)
			if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Desired != view.Desired || report.Sessions[0].ConfigError != view.ConfigError {
				t.Fatal(report, err)
			}
			list, err := e.List(ctx, q.Profile)
			if err != nil || len(list.Sessions) != 1 || list.Sessions[0].ConfigError != "" || list.Sessions[0].Desired != "" {
				t.Fatal("listing resolved configuration", list, err)
			}
		})
	}
}

func TestInventoryDistinguishesCorruptAndAbsentRecords(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.Store.RecordPath(created.Name)
	write(t, p, "broken")
	report, err := e.List(ctx, "")
	if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Error == "" || len(report.UnmatchedContainers) != 0 {
		t.Fatal(report, err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	report, err = e.List(ctx, "")
	if err != nil || len(report.Sessions) != 0 || len(report.UnmatchedContainers) != 1 {
		t.Fatal(report, err)
	}
	d.Forget(created.Name)
	report, err = e.List(ctx, "")
	if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Error == "" || len(report.UnmatchedContainers) != 0 {
		t.Fatal("incomplete saved state was hidden", report, err)
	}
}
