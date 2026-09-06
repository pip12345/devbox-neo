package docker

import (
	"context"
	"path/filepath"
)

// InstallRuntime changes only the Devbox-owned in-container directory. This is
// root inside the verified container, never execution of a host setup script.
func (r Runtime) InstallRuntime(ctx context.Context, c Container, o Owner, source string) error {
	if err := c.Verify(o); err != nil {
		return err
	}
	if _, err := r.capture(ctx, "exec", "--user", "root", c.ID, "sh", "-c", `test ! -L /devbox && mkdir -p /devbox && chown root:root /devbox && chmod 0755 /devbox`); err != nil {
		return err
	}
	if _, err := r.capture(ctx, "cp", source+string(filepath.Separator)+".", c.ID+":/devbox"); err != nil {
		return err
	}
	_, err := r.capture(ctx, "exec", "--user", "root", c.ID, "sh", "-c", `chown -R root:root /devbox && chmod -R u=rwX,go=rX /devbox`)
	return err
}
