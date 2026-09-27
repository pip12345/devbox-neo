package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devbox/internal/docker"
)

func TestLayeredBuildUsesTypedPlans(t *testing.T) {
	e, d, q := fixture(t)
	root := filepath.Join(e.Store.Home, "profiles/test")
	file := "Dockerfile"
	write(t, filepath.Join(root, file), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nCOPY asset /opt/asset\n")
	write(t, filepath.Join(root, "asset"), "context data")
	var built, tags []string
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
		tags = append(tags, args[slices.Index(args, "--tag")+1])
		contextDir := args[len(args)-1]
		if !strings.Contains(string(body), "COPY asset") {
			if _, err := os.Stat(filepath.Join(contextDir, "asset")); !os.IsNotExist(err) {
				t.Fatal("runtime stage inherited a user context")
			}
			return nil
		}
		data, err := os.ReadFile(filepath.Join(contextDir, "asset"))
		if err != nil {
			return err
		}
		if string(data) != "context data" {
			t.Fatal("build did not use captured context")
		}
		return nil
	}
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	want := 3
	if len(built) != want {
		t.Fatal("wrong stage count", len(built))
	}
	if !strings.HasPrefix(tags[0], docker.Namespace+"/build:") || !strings.Contains(built[1], "FROM ${DEVBOX_BASE}\n") {
		t.Fatal("custom stage does not extend the prepared base", built)
	}
	stage := 0
	for _, args := range d.History() {
		if args[0] != "build" {
			continue
		}
		if stage > 0 && !slices.Contains(args, "DEVBOX_BASE="+tags[stage-1]) {
			t.Fatal("build chain lost predecessor", args)
		}
		stage++
	}
	if !slices.ContainsFunc(d.History(), func(args []string) bool {
		return slices.Equal(args, []string{"image", "rm", tags[0]})
	}) {
		t.Fatal("intermediate tag was not cleaned up")
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
	if sessionRecord(t, e, result.SessionID).ID == "" {
		t.Fatal("creation did not commit")
	}
}
func TestLayeredBuildCleansBaseTagAfterRuntimeFailure(t *testing.T) {
	e, d, q := fixture(t)
	write(t, filepath.Join(e.Store.Home, "profiles/test/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
	failure := errors.New("runtime build failed")
	var baseTag string
	d.Fail = func(args []string) error {
		if args[0] != "build" {
			return nil
		}
		if baseTag == "" {
			baseTag = args[slices.Index(args, "--tag")+1]
			return nil
		}
		return failure
	}
	if _, err := e.Create(context.Background(), q); !errors.Is(err, failure) {
		t.Fatal("runtime build failure was not returned", err)
	}
	for _, args := range d.History() {
		if slices.Equal(args, []string{"image", "rm", baseTag}) {
			return
		}
	}
	t.Fatal("intermediate tag was not cleaned up after runtime failure")
}
func TestSeedingHigherPriorityDockerfileWarnsWithoutReplacement(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
	q.Sources = q.Sources[1:]
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Workspace, ".devbox/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != result.SessionID || count(d, "create") != 1 || count(d, "build") != 2 || len(result.Diagnostics) == 0 {
		t.Fatal("Dockerfile seeding did not remain non-destructive drift")
	}
}
