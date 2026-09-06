// Package docs exposes the same human documentation shipped with the CLI.
package docs

import "embed"

//go:embed src dev/*.md
var Files embed.FS
