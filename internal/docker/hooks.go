package docker

import (
	"context"
	"path/filepath"
)

func OpenHookPath(hash string) string { return "/devbox/hooks/" + hash + ".sh" }

// Publish complete scripts from staging. A cancelled copy cannot truncate an
// applied script, and different content never shares a filename with old hooks.
func (r Runtime) InstallOpenHooks(ctx context.Context, c Container, o Owner, source string) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	if _, err := r.capture(ctx, "exec", "--user", "root", c.ID, "sh", "-c", `set -eu
 test ! -L /devbox && test ! -L /devbox/hooks
 mkdir -p /devbox/hooks
 chown root:root /devbox /devbox/hooks
 chmod 0755 /devbox /devbox/hooks
 rm -rf /devbox/hooks/.incoming
 mkdir -m 0700 /devbox/hooks/.incoming`, "dbx-stage-hooks"); err != nil {
		return err
	}
	if _, err := r.capture(ctx, "cp", source+string(filepath.Separator)+".", c.ID+":/devbox/hooks/.incoming"); err != nil {
		return err
	}
	_, err := r.capture(ctx, "exec", "--user", "root", c.ID, "sh", "-c", `set -eu
 for file in /devbox/hooks/.incoming/*.sh; do
   test -f "$file"
   chown root:root "$file"
   chmod 0444 "$file"
   mv -f "$file" /devbox/hooks/
 done
 rmdir /devbox/hooks/.incoming`, "dbx-publish-hooks")
	return err
}

func (r Runtime) CheckOpenHooks(ctx context.Context, c Container, o Owner, paths []string) error {
	return r.Exec(ctx, c, o, append([]string{"sh", "-c", `for file do test -f "$file" && test ! -L "$file" || exit 1; done`, "dbx-hooks"}, paths...), nil, Streams{})
}
