package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// The old projection mirrors paths, and leaves links behind when their staged
// entry disappears. Only that exact shape, inside an existing verified stage,
// establishes a stale generated entry. A missing stage or a differently named
// link does not establish what the old mount contained and remains a blocker.
func staleProjection(stage, relative, link string) bool {
	if !filepath.IsLocal(relative) || link != "/devbox/harness-config/"+filepath.ToSlash(relative) {
		return false
	}
	info, err := os.Lstat(stage)
	return err == nil && info.IsDir()
}

func projectionOmission(path, link string) string {
	return fmt.Sprintf("Omit stale generated config link %s -> %s: its entry is absent from the existing source stage. No target contents were available; the original link remains in the old installation.", strconv.QuoteToASCII(path), strconv.QuoteToASCII(link))
}
