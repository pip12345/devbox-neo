package environment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageFingerprintIsScopedToDockerDirectory(t *testing.T) {
	for _, tc := range []struct {
		path, content string
		imageChange   bool
	}{
		{"pi/custom.json", `{"changed":true}`, false},
		{"setup.sh", "echo changed setup\n", false},
		{"before-open.sh", "echo changed preparation\n", false},
		{"config.json", `{"harness":"pi","shell":["sh"]}`, false},
		{".dockerignore", "*\n", false},
		{"unrelated/file", "unrelated", false},
		{"docker/ignored", "ignored by docker/.dockerignore", false},
		{"docker/data", "changed build input", true},
		{"docker/Dockerfile", "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nRUN true\n", true},
		{"docker/.dockerignore", "ignored\ndata\n", true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			before, q := driftFixture(t)
			root := q.Sources[0].Path
			putBuild(t, filepath.Join(root, tc.path), tc.content)
			// Unrelated special entries must not be scanned as Docker inputs.
			if err := os.Symlink("missing", filepath.Join(root, "unrelated-link")); err != nil {
				t.Fatal(err)
			}
			after, err := Resolve(q)
			if err != nil {
				t.Fatal(err)
			}
			changed := before.Fingerprints.Image != after.Fingerprints.Image
			if changed != tc.imageChange {
				t.Fatalf("image fingerprint changed = %v, want %v", changed, tc.imageChange)
			}
			for _, name := range []string{"config.json", "setup.sh", "before-open.sh", "pi/custom.json", "unrelated-link"} {
				if _, exists := after.Build.Stages[0].Context[name]; exists {
					t.Fatalf("context included non-Docker file %s", name)
				}
			}
		})
	}
}
