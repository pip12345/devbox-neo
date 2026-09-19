package docker

import (
	"context"
	"fmt"
)

func (r Runtime) RestartPolicy(ctx context.Context, c Container, owner Owner, policy string) error {
	if err := c.Verify(owner); err != nil {
		return err
	}
	if policy != "no" && policy != "unless-stopped" {
		return fmt.Errorf("invalid managed restart policy")
	}
	if c.HostConfig.RestartPolicy.Name == policy {
		return nil
	}
	_, err := r.capture(ctx, "update", "--restart="+policy, c.ID)
	return err
}
