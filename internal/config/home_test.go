package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeAliasesCannotReachOldInstallation(t *testing.T) {
	user := t.TempDir()
	old := filepath.Join(user, ".devbox")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, filepath.Join(user, ".devbox-neo")); err != nil {
		t.Fatal(err)
	}
	if _, err := Home("", "", user); err == nil {
		t.Fatal("neo alias reached the old installation")
	}
	alias := filepath.Join(t.TempDir(), "user")
	if err := os.Symlink(user, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Home(old, "", alias); err == nil {
		t.Fatal("aliased user home bypassed old-home protection")
	}
}
func TestDevelopmentHome(t *testing.T) {
	user := t.TempDir()
	tests := []struct {
		name, explicit, env, want string
		fail                      bool
	}{
		{name: "default", want: filepath.Join(user, ".devbox-neo")},
		{name: "environment", env: filepath.Join(user, "isolated"), want: filepath.Join(user, "isolated")},
		{name: "explicit wins", explicit: filepath.Join(user, "explicit"), env: filepath.Join(user, "other"), want: filepath.Join(user, "explicit")},
		{name: "tilde", explicit: "~/.devbox-neo", want: filepath.Join(user, ".devbox-neo")},
		{name: "old explicit", explicit: filepath.Join(user, ".devbox"), fail: true},
		{name: "old inherited", env: filepath.Join(user, ".devbox"), fail: true},
		{name: "old subtree", explicit: filepath.Join(user, ".devbox", "nested"), fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Home(tt.explicit, tt.env, user)
			if (err != nil) != tt.fail {
				t.Fatalf("error: %v", err)
			}
			if !tt.fail && got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}
