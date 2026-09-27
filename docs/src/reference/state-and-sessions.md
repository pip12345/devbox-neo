# State and sessions

A session saves an environment's identity and harness state. Its container can be replaced. The default home is `~/.devbox-neo`; select another with `--home` or `DEVBOX_HOME`.

## What survives

| Data | Stop/start | Recreate | Copy/move |
|---|---|---|---|
| Project files | Retained on host | Retained on host | Not copied |
| Declared session harness stores | Retained | Retained | Copied |
| Managed auth and shared caches | Retained separately | Retained separately | Not copied; destination uses managed auth/caches |
| Container-local files/tools | Retained | Lost | Not copied |
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
| `sessions/<directory>/session.json` | Session ID, editable `settings`, recorded `applied` runtime, and activity |
| `state/leases/<session-id>/` | Active command records |
| `sessions/<directory>/harnesses/<harness>/stores/<store>/` | Per-session harness state |
| `sessions/<directory>/harnesses/<harness>/managed-config.json` | Managed file/key tracking |
| `sessions/<directory>/runtime/ssh/` | Temporary shared SSH connections |
| `state/installation-id` | Installation identity |
| `state/workspaces/<workspace-key>.json` | Folder default |
| `state/transfers/<source-directory>.json` | Pending transfer |
| `state/locks/` | Installation, config, folder, and session locks |

Pending transfers reserve both endpoints. Retry the reported command; do not delete their files to unblock another operation. [State architecture](../architecture/state.md) documents file schemas, keys, and locking.

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
| OpenCode | Auth: `auth.json` | `/home/devuser/.local/share/opencode/auth.json` |
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

Exact session targets are immutable 32-hex IDs, shown by `list --wide`. Readable directory and Docker names are independently allocated hints, not identity; their hashes do not track the current workspace. Editing settings does not rename either resource.

Folder-only commands require an explicit default ID. Docker operations verify installation/session ownership and the recorded container association, not name equality.

## Recovery

Open and Start can restore a missing container from its recorded image and inputs. If required inputs are unavailable, follow the error or recreate from current configs.

Recreation preserves identity, harness state, and keep-running intent. Copy creates a separate session identity; Move preserves the original. [Manage environments](../guides/managing-environments.md) covers those workflows.

Recorded activity describes Devbox operations, not filesystem changes. For record validation, applied snapshots, and recovery verification, see [lifecycle architecture](../architecture/lifecycle.md).
