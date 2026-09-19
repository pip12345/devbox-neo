package cli

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestCreationAndShutdownOverridesAreRejectedBeforeInitialization(t *testing.T) {
	for _, command := range []string{"create", "recreate", "open"} {
		flags := []string{"--on-exit=running", "--read-only", "--network=host", "--harness=pi", "--volume=data:/data", "--port=8080:80", "--env=TOKEN=private-value", "--docker-arg=--restart=always"}
		if command != "open" {
			flags = append(flags, "--harness-arg=--version")
		}
		for _, flag := range flags {
			t.Run(command+"/"+flag, func(t *testing.T) {
				home := t.TempDir()
				before := completionSnapshot(t, home)
				cmd := New()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				cmd.SetArgs([]string{"--home", home, command, ".", flag})
				if code := Execute(context.Background(), cmd); code != 1 {
					t.Fatal("removed flag accepted", code, out.String())
				}
				if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
					t.Fatal("rejected flag initialized home")
				}
			})
		}
	}
}
