package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLayeredBuildUsesTypedPlans(t *testing.T) {
	e, d, q := fixture(t)
	root := filepath.Join(e.Store.Home, "profiles/test")
	file := "Dockerfile"
	write(t, filepath.Join(root, file), "FROM debian:bookworm-slim\nCOPY asset /opt/asset\n")
	write(t, filepath.Join(root, "asset"), "context data")
	var built []string
	d.Fail = func(args []string) error {
		if args[0] != "build" {
			return nil
		}
		path := args[slices.Index(args, "--file")+1]
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		built = append(built, string(body))
		contextDir := args[len(args)-1]
		data, err := os.ReadFile(filepath.Join(contextDir, "asset"))
		if err != nil {
			return err
		}
		if string(data) != "context data" {
			t.Fatal("build did not use captured context")
		}
		return nil
	}
	result, err := e.Open(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	want := 2
	if len(built) != want {
		t.Fatal("wrong stage count", len(built))
	}
	if !strings.Contains(built[1], "FROM sha256:") {
		t.Fatal("runtime did not use exact intermediate ID")
	}
	before := count(d, "build")
	if _, err = e.Open(context.Background(), q); err != nil || count(d, "build") != before {
		t.Fatal("reopen rebuilt", err)
	}
	if _, err = e.Recreate(context.Background(), q, true); err != nil {
		t.Fatal(err)
	}
	history := d.History()
	seen := 0
	for i := len(history) - 1; i >= 0 && seen < want; i-- {
		if history[i][0] == "build" {
			if !slices.Contains(history[i], "--no-cache") {
				t.Fatal("forced rebuild reused controlled-stage cache")
			}
			seen++
		}
	}
	if record(t, e, result.Name).ID == "" {
		t.Fatal("creation did not commit")
	}
}
func TestSeedingHigherPriorityDockerfileWarnsWithoutReplacement(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	q.Profile = ""
	first, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/Dockerfile"), "FROM debian:bookworm-slim\n")
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != result.Name || count(d, "create") != 1 || count(d, "build") != 1 || len(result.Diagnostics) == 0 {
		t.Fatal("Dockerfile seeding did not remain non-destructive drift")
	}
}
