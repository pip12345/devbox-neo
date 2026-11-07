package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTransferReleasePrecedesPreparedRecordCleanup(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "complete"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			s := &Store{Home: t.TempDir()}
			id := "123456789abc"
			root := filepath.Join(s.Home, "state/transfers", id)
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(root, "journal.json")
			prepared := filepath.Join(root, "destination.json")
			for _, path := range []string{journal, prepared} {
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("cleanup interrupted")
			err := releaseTransferDirectory(root, func(path string) error {
				if _, err := os.Stat(journal); !os.IsNotExist(err) {
					t.Fatal("cleanup started before journal release", err)
				}
				if _, err := os.Stat(prepared); err != nil {
					t.Fatal("prepared record lost before release", err)
				}
				if interrupted {
					if err := os.Remove(prepared); err != nil {
						t.Fatal(err)
					}
					return failure
				}
				return os.RemoveAll(path)
			})
			if interrupted && !errors.Is(err, failure) || !interrupted && err != nil {
				t.Fatal(err)
			}
			if j, err := s.ReadTransfer(id); err != nil || j != nil {
				t.Fatal("cleanup left a blocking journal", j, err)
			}
			if !interrupted {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("completed cleanup retained temporary files", err)
				}
			}
		})
	}
}

func TestFailedTransferReleaseRetainsPreparedRecord(t *testing.T) {
	root := t.TempDir()
	// A nonempty directory makes journal removal fail without permissions or
	// platform-specific fault injection, including when tests run as root.
	journal := filepath.Join(root, "journal.json")
	if err := os.Mkdir(journal, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journal, "keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	prepared := filepath.Join(root, "destination.json")
	if err := os.WriteFile(prepared, []byte("prepared"), 0600); err != nil {
		t.Fatal(err)
	}
	err := releaseTransferDirectory(root, func(string) error {
		t.Fatal("cleanup ran despite failed journal release")
		return nil
	})
	if err == nil {
		t.Fatal("journal release failure ignored")
	}
	if data, err := os.ReadFile(prepared); err != nil || string(data) != "prepared" {
		t.Fatal("release failure discarded retry inputs", err)
	}
}
