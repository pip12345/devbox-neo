package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestReadableResourceNamesAreIndependentOfSessionIdentity(t *testing.T) {
	e, d, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	for _, name := range []string{r.Directory, r.Applied.Creation.Name} {
		if !strings.HasPrefix(name, "dbx-"+filepath.Base(q.Workspace)+"-") || !strings.HasSuffix(name, "."+q.LocalName) {
			t.Fatal(name)
		}
	}
	if r.Directory == r.Applied.Creation.Name || r.ID != made.SessionID {
		t.Fatal("storage/runtime names became identity")
	}
	if _, err := os.Stat(filepath.Join(e.Store.Home, "sessions", r.Directory, "session.json")); err != nil {
		t.Fatal(err)
	}
	c, ok := d.Snapshot(r.Applied.Creation.Name)
	if !ok || c.Config.Labels[docker.Namespace+".managed"] != "true" || c.Config.Labels[docker.Namespace+".session"] != r.ID {
		t.Fatal("ownership no longer uses session ID")
	}
	if r.Applied.ImageTag != environment.ImageTag(q.Workspace, q.LocalName, r.ID) {
		t.Fatal(r.Applied.ImageTag)
	}
	r.Version = 5
	if err := r.Validate(r.Directory); err == nil {
		t.Fatal("old record schema accepted")
	}
}
