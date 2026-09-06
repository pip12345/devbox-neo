// Package assets owns the in-container runtime documentation contract.
package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"strings"

	"devbox/docs"
)

const AgentGuide = `# Devbox environment

You are inside a persistent Linux development container. /workspace is the host
workspace. Docker and the host devbox-neo CLI normally live outside this container.
Do not try to manage this container or start a Docker daemon inside it.

Read /devbox/docs/index.md for the runtime's documentation. /devbox/network/env
contains shell-safe network facts, and /devbox/network/inspect.json has live facts.
Use DEVBOX_HOST to reach host services; services need to listen on a reachable
interface, not host loopback alone on a bridge network. Listen on 0.0.0.0 for
container services that must be reached outside the container.

Install development tools in the container when needed. Container-layer changes
survive stop/start but are lost on recreation. Workspace files and declared
harness stores persist on the host. Managed authentication and shared caches are
separate from resettable session state.

Do not modify /devbox: it is Devbox-owned runtime data. Do not add a project
Dockerfile to make an ad-hoc tool installation persistent without user approval.
Devbox does not enforce network egress restrictions or provide an offline mode.
`

func Files() (map[string][]byte, error) {
	files := map[string][]byte{"AGENTS.md": []byte(AgentGuide)}
	err := fs.WalkDir(docs.Files, "src", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := docs.Files.ReadFile(p)
		if err != nil {
			return err
		}
		files["docs/"+strings.TrimPrefix(p, "src/")] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries, err := docs.Files.ReadDir("dev")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		b, err := docs.Files.ReadFile("dev/" + entry.Name())
		if err != nil {
			return nil, err
		}
		files["dev/"+entry.Name()] = b
	}
	return files, nil
}
func Hash() (string, error) {
	files, err := Files()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
