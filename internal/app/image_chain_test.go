package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImageChainHasSeparateContextsAndPreparedUserBoundaries(t *testing.T) {
	e, d, q := fixture(t)
	profile := filepath.Join(e.Store.Home, "profiles/test")
	project := filepath.Join(q.Workspace, ".devbox")
	write(t, filepath.Join(project, "config.json"), `{"base_image":"ubuntu:24.04"}`)
	for _, pair := range []struct{ root, marker string }{{profile, "profile_asset"}, {project, "project_asset"}} {
		write(t, filepath.Join(pair.root, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nCOPY "+pair.marker+" /opt/asset\nENV PATH=\"/custom/bin:${PATH}\"\nUSER root\n")
		write(t, filepath.Join(pair.root, "docker", pair.marker), pair.marker)
	}
	var stages []string
	d.Fail = func(args []string) error {
		if args[0] != "build" {
			return nil
		}
		data, err := os.ReadFile(args[slices.Index(args, "--file")+1])
		if err != nil {
			return err
		}
		body := string(data)
		stages = append(stages, body)
		root := args[len(args)-1]
		for _, marker := range []string{"profile_asset", "project_asset"} {
			_, err := os.Stat(filepath.Join(root, marker))
			if strings.Contains(body, "COPY "+marker) {
				if err != nil {
					t.Fatal(err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("build contexts leaked across stages", body, marker)
			}
		}
		for _, argument := range []string{"DEVBOX_USER=devuser", "DEVBOX_USER_HOME=/home/devuser", "DEVBOX_WORKSPACE=/workspace", "DEVBOX_UID=1000", "DEVBOX_GID=1000"} {
			if !slices.Contains(args, argument) {
				t.Fatal("missing build argument", argument, args)
			}
		}
		return nil
	}
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 5 || !strings.HasPrefix(stages[0], "FROM ubuntu:24.04\n") || !strings.Contains(stages[1], "COPY profile_asset") || !strings.Contains(stages[3], "COPY project_asset") {
		t.Fatal(stages)
	}
	for _, index := range []int{2, 4} {
		if !strings.Contains(stages[index], "USER devuser\nENV HOME=/home/devuser USER=devuser\nWORKDIR /workspace") {
			t.Fatal("customization boundary lost development user", stages[index])
		}
	}
	if !strings.Contains(stages[4], "${PATH}") {
		t.Fatal("harness finalization replaced user PATH", stages[4])
	}
	if len(sessionRecord(t, e, made.SessionID).Applied.Inputs.Image.Stages) != 2 {
		t.Fatal("record lost ordered image inputs")
	}
}

func TestCustomDockerfileCannotReplacePreparedBase(t *testing.T) {
	e, _, q := fixture(t)
	write(t, filepath.Join(e.Store.Home, "profiles/test/docker/Dockerfile"), "FROM debian:bookworm-slim\nRUN true\n")
	if _, err := e.Create(context.Background(), q); err == nil || !strings.Contains(err.Error(), "must extend DEVBOX_BASE") {
		t.Fatal("accepted independent image", err)
	}
}
