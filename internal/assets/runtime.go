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

const AgentGuide = `# Devbox container

You are inside a persistent Linux development container. /workspace is the host
project: changes there affect the user's real files. Docker and devbox-neo normally
run on the host. Ask the user for container lifecycle changes; do not start a
Docker daemon or try to manage this container from inside it.

## Tools and files

- Install missing development tools here; sudo is available.
- Container-local installations survive stop/start, but not recreation. Workspace
  files and declared harness stores persist. Other writable paths may not.
- Do not add Dockerfiles or change persistent environment config without approval.
- Do not modify /devbox. It contains Devbox-managed runtime files.
- Managed harness files are overwritten on synchronization. Lasting changes belong
  in the selected host config directory, not its live container copy.

## Connections

- Read /devbox/network/env or /devbox/network/inspect.json for current network facts.
  Use DEVBOX_HOST for host services. Host loopback-only services may be unreachable;
  container servers intended for external access should listen on 0.0.0.0.
- Shared SSH aliases are in /devbox/ssh/config and its included files. Use ssh/scp
  with -F /devbox/ssh/config. If a connection is unavailable, ask the user to share
  it from a host terminal. Do not search for credentials, handle login prompts,
  or bypass the shared connection with a fresh login.

Read /devbox/docs/index.md only as needed. Task details:
- Config changes: /devbox/docs/guides/configuration.md
- Persistence: /devbox/docs/reference/state-and-sessions.md
- Networking: /devbox/docs/guides/networking.md
- SSH sharing: /devbox/docs/guides/ssh.md
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
