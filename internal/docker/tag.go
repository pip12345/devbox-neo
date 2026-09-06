package docker

import (
	"context"
	"fmt"
	"strings"
)

// Tag restores the source's tag after a failed relocation build. Both the
// concrete image and any current tag occupant must belong to this installation.
func (r Runtime) Tag(ctx context.Context, id, tag, installation string) error {
	if !strings.HasPrefix(tag, Namespace+"/session:") {
		return fmt.Errorf("not a session image tag")
	}
	image, err := r.InspectImage(ctx, id)
	if err != nil {
		return err
	}
	if err = image.Verify(installation); err != nil {
		return err
	}
	occupant, exists, err := r.TaggedImage(ctx, tag)
	if err != nil {
		return err
	}
	if exists {
		if err = occupant.Verify(installation); err != nil {
			return err
		}
	}
	_, err = r.capture(ctx, "image", "tag", id, tag)
	return err
}
