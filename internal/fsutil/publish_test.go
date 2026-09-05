package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishDirectoryNeverReplacesExistingOwner(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "stage")
	destination := filepath.Join(parent, "existing")
	for _, p := range []string{source, destination} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := Write(filepath.Join(source, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PublishDirectory(source, destination); !os.IsExist(err) {
		t.Fatal("existing empty directory replaced", err)
	}
	if _, err := os.Stat(filepath.Join(source, "config.json")); err != nil {
		t.Fatal("staged data lost", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "config.json")); !os.IsNotExist(err) {
		t.Fatal("destination changed")
	}
	if err := PublishDirectory(source, filepath.Join(parent, "new")); err != nil {
		t.Fatal(err)
	}
}
