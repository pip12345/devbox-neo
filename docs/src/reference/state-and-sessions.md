# State and sessions

A session saves an environment's identity and harness state. Its container can be replaced. The default home is `~/.devbox-neo`. Select another with `--home` or `DEVBOX_HOME`.

## What survives

| Data | Stop/start | Recreate | Copy/move |
|---|---|---|---|
| Project files | Retained on host | Retained on host | Not copied |
| Saved harness state, including history | Retained | Retained | Copied |
| Managed auth and shared caches | Retained separately | Retained separately | Not copied. Destination uses managed auth/caches |
| Container-local files/tools | Retained | Lost only if container is replaced | Not copied |
| Live SSH connections | End when controller/container stops | Not retained | Not copied |

`delete --container` keeps saved state and its folder default. `delete --session` also removes saved history and the session image tag, clearing a matching default. Neither deletes project files, configs, managed auth, or caches.

## Home layout

Paths below are relative to the selected home. Use Devbox commands to manage session state rather than editing these files.

| Path | Purpose |
|---|---|
| `configs/<name>/` | Named config directories |
| `harnesses/<name>/` | User definitions and defaults |
| `auth/<harness>/` | Managed authentication |
| `cache/harnesses/<harness>/<store>/` | Shared caches |
| `sessions/<session-id>/` | Saved settings, harness state, and temporary SSH-sharing files for one session |
| `state/` | Internal default selections, active-command tracking, locks, and pending transfers |

Use `dbx list --wide` to find a session's storage path. Finish or abort pending transfers with the [transfer commands](commands.md#transfers), rather than deleting their files. Contributors can find record and transaction details in [State, locking, and transfers](../architecture/state.md).

## Built-in storage mappings

Targets are inside the container. Auth filenames are relative to `<home>/auth/<harness>/`.

| Harness | Kind / store | Container target |
|---|---|---|
| Pi | Session: `home` | `/home/devuser/.pi/agent` |
| Pi | Cache: `npm-global` | `/home/devuser/.local` |
| Pi | Cache: `npm-cache` | `/home/devuser/.npm` |
| Pi | Auth: `auth.json` | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | Session: `config` | `/home/devuser/.config/opencode` |
| OpenCode | Session: `data` | `/home/devuser/.local/share/opencode` |
| OpenCode | Cache: `cache` | `/home/devuser/.cache/opencode` |
| OpenCode | Auth: `shared/` | `/home/devuser/.local/share/devbox-opencode-auth` |
| Claude | Session: `home` | `/home/devuser/.claude` |
| Claude | Auth: `.credentials.json` | `/home/devuser/.claude/.credentials.json` |
| Claude | Shared auth/client state: `.claude.json` | `/home/devuser/.claude.json` |

Claude declares no shared cache. Its auth files are shared across Claude sessions and excluded from transfers. Paths outside declared mounts remain container-local, even if writable.

## Container paths

| Path | Purpose |
|---|---|
| `/workspace` | Host project |
| `/devbox/AGENTS.md` | Essential container instructions for the agent |
| `/devbox/docs/index.md` | Bundled documentation |
| `/devbox/network/env` | Shell-safe network facts |
| `/devbox/network/inspect.json` | Network facts as JSON |
| `/devbox/ssh/config` | Shared SSH aliases |

These are Devbox-managed runtime paths. Shared SSH data is transient, not a credential store.

## Names and ownership

Local names are 1–64 ASCII characters: letters, digits, `_`, and `-`, starting with a letter or digit. Case matters: `work` and `Work` differ.

Public names are `dbx-<workspace-hint>-<session-id>.<local-name>`, shown by `dbx list` and `dbx status`. The 12-character session ID is stable. The workspace hint and local name reflect the saved project folder and name.

Renaming a session or changing its workspace basename changes its public name without moving its storage. Old names are not aliases. Docker containers keep their existing names until replaced. Docker names and raw IDs cannot be used as session targets.

Folder-only commands require a saved default. Devbox checks that Docker resources belong to the selected installation and session before changing them.

## Recovery

Recreate uses current configs, not a saved copy of an earlier environment. It cannot restore deleted harness history. Missing saved-data directories cause an error rather than being replaced with empty history. A missing image does not prevent access to an existing container.

Recreation preserves identity, harness state, and keep-running intent. Copy creates a separate session identity. Move preserves the original. [Manage environments](../guides/managing-environments.md) covers those workflows.

Recorded activity describes Devbox operations, not filesystem changes.
