package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDocsAndNetworkFactsAreStagedAndCleaned(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var staged []string
	var facts NetworkFacts
	d.Fail = func(args []string) error {
		if args[0] != "cp" {
			return nil
		}
		source := strings.TrimSuffix(args[1], "/.")
		staged = append(staged, source)
		names := []string{"network/env", "network/inspect.json"}
		if len(staged) == 1 {
			names = append(names, "AGENTS.md", "docs/index.md")
		}
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(source, name)); err != nil {
				return err
			}
		}
		b, err := os.ReadFile(filepath.Join(source, "network/inspect.json"))
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &facts)
	}
	result, err := createAndOpen(ctx, e, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) == 0 || facts.Name != result.SessionID || facts.Host == "" {
		t.Fatal("runtime facts missing", facts)
	}
	for _, source := range staged {
		if _, err = os.Stat(source); !os.IsNotExist(err) {
			t.Fatal("runtime staging remained", source)
		}
	}
	if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
		t.Fatal("running access reloaded desired config", err)
	}
	if err = e.ChangeNetwork(ctx, result.SessionID, "", "secondary", true); err != nil {
		t.Fatal(err)
	}
	if len(facts.Networks) != 2 {
		t.Fatal("network mutation did not refresh runtime facts")
	}
}
func TestFailedRuntimeCopyDoesNotLaunchOrLeaveStaging(t *testing.T) {
	e, d, q := fixture(t)
	spec, err := e.Resolve(resolveRequest(q))
	if err != nil {
		t.Fatal(err)
	}
	launch := append([]string{spec.Harness.Definition.Binary}, spec.Harness.Definition.Launch.Args...)
	d.Fail = func(args []string) error {
		if args[0] == "cp" {
			return errors.New("copy failed")
		}
		return nil
	}
	if _, err := e.Create(context.Background(), q); err == nil {
		t.Fatal("copy failure ignored")
	}
	files, err := filepath.Glob(filepath.Join(e.Store.Home, ".runtime-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("staging cleanup failed", files, err)
	}
	for _, args := range d.History() {
		if args[0] == "exec" && argvSuffix(args, launch) {
			t.Fatal("harness launched after runtime failure")
		}
	}
}
