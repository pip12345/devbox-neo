package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/resource"
)

func TestResourceSuggestionsOnlyCarryExplicitHome(t *testing.T) {
	for _, mode := range []string{"default", "environment", "explicit", "explicit-default", "explicit-over-environment"} {
		t.Run(mode, func(t *testing.T) {
			userHome := t.TempDir()
			t.Setenv("HOME", userHome)
			t.Setenv("DEVBOX_HOME", "")
			home := filepath.Join(userHome, ".devbox-neo")
			explicit := strings.HasPrefix(mode, "explicit")
			if mode == "environment" || mode == "explicit-over-environment" {
				home = filepath.Join(userHome, "env home")
				t.Setenv("DEVBOX_HOME", home)
			}
			if explicit && mode != "explicit-default" {
				home = filepath.Join(userHome, "explicit home")
			}
			run := func(args ...string) (string, error) {
				cmd := New()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				cmd.SetIn(strings.NewReader(""))
				if explicit {
					args = append([]string{"--home", home}, args...)
				}
				cmd.SetArgs(args)
				invoked, err := cmd.ExecuteC()
				if err != nil {
					RenderError(invoked, err)
				}
				return out.String(), err
			}
			check := func(text string) {
				t.Helper()
				if strings.Contains(text, "\nNext:\n") || !strings.Contains(text, ":\n  devbox-neo ") {
					t.Fatal("missing labeled command", text)
				}
				if strings.Contains(text, "--home") != explicit {
					t.Fatal("wrong home propagation", text)
				}
				if explicit && !strings.Contains(text, shellQuote(home)) {
					t.Fatal("explicit resolved home was not quoted", text)
				}
			}
			out, err := run("config", "create", "basic")
			if err != nil {
				t.Fatal(err)
			}
			check(out)
			if !explicit && !strings.Contains(out, "  devbox-neo create ") {
				t.Fatal("default suggestion is not minimal", out)
			}
			out, err = run("config", "create", "basic")
			if err == nil {
				t.Fatal("expected existing-config edit guidance")
			}
			check(out)
			out, err = run("config", "edit", "missing", "--artifact", "Dockerfile")
			if err == nil {
				t.Fatal("expected missing-profile guidance")
			}
			check(out)
			out, err = run("config", "create", "json", "--json")
			if err != nil {
				t.Fatal(err)
			}
			var result resource.Result
			if err = json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			want := []string{"devbox-neo"}
			if explicit {
				want = append(want, "--home", home)
			}
			want = append(want, "create", "<folder>", "--config", filepath.Join(home, "configs/json"))
			if len(result.Next) != 1 || !reflect.DeepEqual(result.Next[0].Command, want) {
				t.Fatalf("wrong JSON next steps: %#v", result.Next)
			}
		})
	}
}
