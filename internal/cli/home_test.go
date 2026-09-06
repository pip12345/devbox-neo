package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultHomeIsNeo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEVBOX_HOME", "")
	cmd := New()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"open", t.TempDir()})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no profile or project configuration") {
		t.Fatalf("expected fresh-home guidance, got %v", err)
	}
	if _, err = os.Stat(filepath.Join(home, ".devbox-neo/config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(home, ".devbox")); !os.IsNotExist(err) {
		t.Fatal("touched old home")
	}
}
func TestInheritedOldHomeIsRejectedBeforeMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEVBOX_HOME", filepath.Join(home, ".devbox"))
	cmd := New()
	cmd.SetArgs([]string{"open", t.TempDir()})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use ~/.devbox") {
		t.Fatalf("missing home guard: %v", err)
	}
	if _, err = os.Stat(filepath.Join(home, ".devbox")); !os.IsNotExist(err) {
		t.Fatal("old home initialized")
	}
}
func TestNullDeviceIsNotATerminal(t *testing.T) {
	file, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if terminal(file) {
		t.Fatal("character device incorrectly treated as terminal")
	}
}
