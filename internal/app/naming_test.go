package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadableResourceNamesAreIndependentOfSessionIdentity(t *testing.T) {
	e, d, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	for _, name := range []string{r.Directory, r.Applied.Creation.Name} {
		if !strings.HasPrefix(name, "dbx-") || !strings.HasSuffix(name, "."+q.LocalName) || len(name) != len("dbx-")+12+1+len(q.LocalName) {
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
	if !ok || c.Config.Labels["devbox-rewrite.managed"] != "true" || c.Config.Labels["devbox-rewrite.session"] != r.ID {
		t.Fatal("ownership no longer uses session ID")
	}
	if r.Applied.ImageTag != "devbox-rewrite/session:"+r.ID {
		t.Fatal(r.Applied.ImageTag)
	}
	r.Version = 5
	if err := r.Validate(r.Directory); err == nil {
		t.Fatal("old record schema accepted")
	}
}
