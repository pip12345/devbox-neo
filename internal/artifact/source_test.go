package artifact

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/config"
)

func TestDockerArtifactUsesOnlyDedicatedDirectory(t *testing.T) {
	source := testSource(t, "base", `{"harness":"pi"}`)
	// Unrecognized config-root files are not Docker inputs or artifact sources.
	put(t, filepath.Join(source.Path, "Dockerfile"), "not a build stage")
	put(t, filepath.Join(source.Path, ".dockerignore"), "*\n")
	put(t, filepath.Join(source.Path, "docker/asset"), "build input")
	r, err := Resolve([]config.Source{source}, config.Host{})
	if err != nil || len(r.Trace.Artifacts) != 0 {
		t.Fatal("discovered an image stage without docker/Dockerfile", r.Trace, err)
	}
	file := filepath.Join(source.Path, Dockerfile)
	put(t, file, "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nCOPY asset /asset\n")
	put(t, filepath.Join(source.Path, "docker/.dockerignore"), "omitted\n")
	put(t, filepath.Join(source.Path, "docker/omitted"), "not a build input")
	if err := os.Symlink("missing", filepath.Join(source.Path, "unrelated-link")); err != nil {
		t.Fatal(err)
	}
	r, err = Resolve([]config.Source{source}, config.Host{})
	if err != nil || !reflect.DeepEqual(r.Trace.Artifacts[Dockerfile], []string{file}) {
		t.Fatal("wrong Dockerfile discovery", r.Trace, err)
	}
	context, err := ReadBuildContext(r.Trace.Artifacts[Dockerfile][0])
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Files) != 3 || string(context.Files["asset"].Data) != "build input" {
		t.Fatal("context escaped docker/ or used root ignore rules", context.Files)
	}
}

func TestDockerArtifactRejectsSymlinkDirectory(t *testing.T) {
	source := testSource(t, "base", `{"harness":"pi"}`)
	external := t.TempDir()
	put(t, filepath.Join(external, "Dockerfile"), "FROM scratch\n")
	if err := os.Symlink(external, filepath.Join(source.Path, "docker")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve([]config.Source{source}, config.Host{}); err == nil {
		t.Fatal("discovery followed a linked build directory")
	}
}
