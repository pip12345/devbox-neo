package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestAccessRequiresExplicitRecreateForMissingRuntime(t *testing.T) {
	for _, action := range []string{"open", "start", "shell", "exec", "ssh"} {
		t.Run(action, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			old := sessionRecord(t, e, made.SessionID)
			forgetSession(t, e, old.ID)
			delete(d.Images, old.Applied.ImageID)
			delete(d.Images, old.Applied.ImageTag)
			before := len(d.History())
			switch action {
			case "open":
				_, err = e.Open(ctx, openRequest(q))
			case "start":
				_, err = e.Start(ctx, old.ID, "")
			case "shell", "exec":
				err = e.Exec(ctx, old.ID, "", []string{"true"}, action == "shell")
			case "ssh":
				err = e.SSH(ctx, old.ID, "", "user@example.com", SSHOptions{})
			}
			var missing *commanderror.Error
			if !errors.As(err, &missing) || missing.Code != "container_missing" || missing.Target != old.Directory || len(missing.Next) != 1 || !reflect.DeepEqual(missing.Next[0].Command, []string{"dbx", "recreate", old.Directory}) {
				t.Fatal("missing recreate guidance", err)
			}
			for _, args := range d.History()[before:] {
				if args[0] != "container" {
					t.Fatal("access changed runtime", args)
				}
			}
			if current := sessionRecord(t, e, old.ID); !reflect.DeepEqual(current, old) {
				t.Fatal("access changed saved session")
			}
			if _, exists := sessionSnapshot(t, e, old.ID); exists {
				t.Fatal("access rebuilt missing runtime")
			}
			if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
				t.Fatal(err)
			}
			current := sessionRecord(t, e, old.ID)
			if current.ID != old.ID || current.Applied.SetupContainer == old.Applied.SetupContainer || current.Settings.ManualStart {
				t.Fatal("explicit recreation changed identity or intent")
			}
		})
	}
}

func TestPrunedImageDoesNotRecreateHealthyContainer(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, made.SessionID)
	delete(d.Images, old.Applied.ImageID)
	delete(d.Images, old.Applied.ImageTag)
	status, err := e.Status(ctx, old.ID, "")
	if err != nil || !status.ImageMissing || !status.Exists {
		t.Fatal(status, err)
	}
	if err := e.Exec(ctx, old.ID, "", []string{"true"}, false); err != nil {
		t.Fatal(err)
	}
	if current := sessionRecord(t, e, old.ID); current.Applied.SetupContainer != old.Applied.SetupContainer || count(d, "create") != 1 {
		t.Fatal("healthy runtime recreated because image was pruned")
	}
}

func TestRuntimeRebuildResolvesCurrentEnvAndSetup(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	source := filepath.Join(e.Store.Home, "profiles/test")
	write(t, filepath.Join(source, "config.json"), `{"version":1,"harness":"pi","env":["TOKEN=${env:TOKEN}"],"docker_args":["--env=RAW=${env:TOKEN}"]}`)
	t.Setenv("TOKEN", "old-secret")
	write(t, filepath.Join(source, "setup.sh"), "echo old-setup")
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, made.SessionID)
	forgetSession(t, e, old.ID)
	t.Setenv("TOKEN", "new-secret")
	write(t, filepath.Join(source, "setup.sh"), "echo new-setup")
	var createdEnv []string
	var setup strings.Builder
	d.Fail = func(args []string) error {
		if args[0] == "create" {
			index := slices.Index(args, "--env-file")
			if index < 0 {
				return errors.New("creation env missing")
			}
			data, err := os.ReadFile(args[index+1])
			if err != nil {
				return err
			}
			createdEnv = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			for _, arg := range args {
				if assignment, ok := strings.CutPrefix(arg, "--env="); ok {
					createdEnv = append(createdEnv, assignment)
				}
			}
		}
		return nil
	}
	d.Attached = func(_ context.Context, command docker.Command) error {
		if argvSuffix(command.Args, []string{"bash", "-s"}) {
			_, err := io.Copy(&setup, command.Stdin)
			return err
		}
		return nil
	}
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(createdEnv, "TOKEN=new-secret") || !slices.Contains(createdEnv, "RAW=new-secret") || strings.Contains(strings.Join(createdEnv, "\n"), "old-secret") || setup.String() != "echo new-setup" {
		t.Fatal("materialization did not consume current env/setup inputs")
	}
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", old.Directory, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"old-secret", "new-secret", "env_sources", "echo old-setup", "echo new-setup"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("historical/env execution input persisted", forbidden)
		}
	}
	var stored struct {
		Applied map[string]json.RawMessage `json:"applied"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, exists := stored.Applied["setup"]; exists {
		t.Fatal("historical setup execution input persisted")
	}
	r := sessionRecord(t, e, old.ID)
	if r.Applied.Inputs.Container.Env["TOKEN"] != environment.Fingerprint(e.Store.Installation, "TOKEN=new-secret") {
		t.Fatal("new environment was not committed")
	}
	if len(r.Applied.Inputs.Container.Setup) != 1 {
		t.Fatal("current setup was not used")
	}
}

func TestMissingDurableStoreBlocksRebuildWithoutReplacingHistory(t *testing.T) {
	e, d, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	forgetSession(t, e, r.ID)
	root := filepath.Join(e.Store.Home, "sessions", r.Directory, "harnesses/pi/stores/home")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Recreate(context.Background(), recreateRequest(q), false); err == nil {
		t.Fatal("lost store replaced by empty history")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("lost store recreated")
	}
	if count(d, "create") != 1 {
		t.Fatal("failed root validation materialized runtime")
	}
}

func TestSessionComparisonSizeDoesNotScaleWithTrees(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	path := filepath.Join(e.Store.Home, "sessions", r.Directory, "session.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(e.Store.Home, "profiles/test")
	write(t, filepath.Join(source, "docker/Dockerfile"), "FROM ${DEVBOX_BASE}\n")
	for i := 0; i < 1000; i++ {
		write(t, filepath.Join(source, "pi", fmt.Sprintf("file-%04d", i)), "managed")
		write(t, filepath.Join(source, "docker", fmt.Sprintf("file-%04d", i)), "build")
	}
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after)-len(before) > 2500 || len(after) > 15000 || strings.Contains(string(after), "file-0999") {
		t.Fatalf("comparison inventories scale with file count: %d -> %d", len(before), len(after))
	}
	write(t, filepath.Join(source, "pi/file-0999"), "changed")
	status, err := e.Status(ctx, r.ID, "")
	if err != nil || status.Desired != environment.RuntimeSync || len(status.PendingInputChanges) != 1 || status.PendingInputChanges[0].Field != "managed_config" {
		t.Fatal("compact baseline lost category drift", status, err)
	}
}
