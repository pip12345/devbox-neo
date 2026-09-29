package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

func TestReadTreeWarnsAndSkipsUnsupportedEntries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "regular"), []byte("kept"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"relative-link": "regular",
		"broken-link":   "missing",
		"outside-link":  outside,
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := ReadTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Files) != 1 || string(tree.Files["regular"].Data) != "kept" || tree.Files["regular"].Mode != 0700 {
		t.Fatal("regular file was lost or unsupported entry was copied", tree.Files)
	}
	if len(tree.Warnings) != 4 {
		t.Fatal(tree.Warnings)
	}
	for i, name := range []string{"broken-link", "outside-link", "pipe", "relative-link"} {
		if !strings.Contains(tree.Warnings[i], filepath.Join(root, name)) || !strings.Contains(tree.Warnings[i], "will not be copied") {
			t.Fatal("missing qualified warning", tree.Warnings[i])
		}
	}
}

func TestReadTreeBoundsWarningsPerSource(t *testing.T) {
	for _, count := range []int{0, 10, 11, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			for source := 0; source < 2; source++ {
				root := t.TempDir()
				for i := 0; i < count; i++ {
					if err := os.Symlink("missing", filepath.Join(root, fmt.Sprintf("link-%03d", i))); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "regular"), []byte("kept"), 0600); err != nil {
					t.Fatal(err)
				}
				tree, err := ReadTree(root)
				if err != nil {
					t.Fatal(err)
				}
				if len(tree.Files) != 1 || string(tree.Files["regular"].Data) != "kept" {
					t.Fatal("copy behavior changed", tree.Files)
				}
				want := count
				if count > 10 {
					want = 11
				}
				if len(tree.Warnings) != want {
					t.Fatal("unexpected warning count", tree.Warnings)
				}
				for i := 0; i < min(count, 10); i++ {
					if !strings.Contains(tree.Warnings[i], filepath.Join(root, fmt.Sprintf("link-%03d", i))) {
						t.Fatal("missing example path", tree.Warnings[i])
					}
				}
				if count > 10 {
					wantSummary := fmt.Sprintf("skipping %d more non-regular config entries under %q; they will not be copied", count-10, root)
					if tree.Warnings[10] != wantSummary {
						t.Fatal("incorrect summary", tree.Warnings[10])
					}
				}
			}
		})
	}
}

type failingTreeFS struct {
	fs.FS
	path string
}

func (f failingTreeFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, fs.ErrPermission
	}
	return f.FS.Open(name)
}

func TestReadTreeKeepsReadFailuresFatal(t *testing.T) {
	for _, name := range []string{".", "file"} {
		t.Run(name, func(t *testing.T) {
			_, err := readTree(failingTreeFS{FS: fstest.MapFS{"file": {Data: []byte("value")}}, path: name}, "defaults")
			if !errors.Is(err, fs.ErrPermission) {
				t.Fatal("read failure was hidden", err)
			}
		})
	}
}

func TestReadTreeStillRejectsSymlinkRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTree(root); err == nil {
		t.Fatal("symlink config root was followed")
	}
}
