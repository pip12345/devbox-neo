# State and sessions

A session saves an environment's identity and harness state. Its container can be replaced. The default home is `~/.devbox-neo`; select another with `--home` or `DEVBOX_HOME`.

## What survives

| Data | Stop/start | Recreate | Copy/move |
|---|---|---|---|
| Project files | Retained on host | Retained on host | Not copied |
| Declared session harness stores | Retained | Retained | Copied |
| Managed auth and shared caches | Retained separately | Retained separately | Not copied; destination uses managed auth/caches |
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
| `sessions/<session-id>/session.json` | Complete 12-character ID, current `state_tree`, desired `settings`, recorded `applied` runtime, and activity |
| `state/leases/<session-id>/` | Active command records |
| `sessions/<session-id>/<state_tree>/harnesses/<harness>/stores/<store>/` | Per-session harness state; initial tree is `data/base` |
| `sessions/<session-id>/<state_tree>/harnesses/<harness>/managed-config.json` | Managed file/key tracking |
| `sessions/<session-id>/ssh/` | Temporary shared SSH connections |
| `state/installation-id` | Installation identity |
| `state/folder-defaults.json` | Folder-to-default-session selections |
| `state/transfers/<source-id>/` | Pending journal and private prepared destination record |
| `state/locks/` | Installation, config, folder, and session locks |

Finish or abort pending transfers with the [transfer commands](commands.md#transfers), rather than deleting their files.

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

Public names are `dbx-<workspace-hint>-<session-id>.<local-name>`, shown by `list` and `status`. The ID directly identifies the storage directory; the workspace hint and local name come from JSON. Rename or a changed workspace basename updates the public name without moving storage or renaming containers. Old names are not aliases. Docker names and raw IDs cannot be used as session targets.

Portable Move prepares another tree under the same ID directory. Commitment switches the recorded tree/runtime before old-state cleanup; it never renames backing paths underneath container mounts.

Folder-only commands require a saved default. Devbox checks that Docker resources belong to the selected installation and session before changing them.

## Recovery

Recreate uses current configs, not a saved copy of an earlier environment. It cannot restore deleted harness history. Missing saved-data directories cause an error rather than being replaced with empty history. A missing image does not prevent access to an existing container.

Recreation preserves identity, harness state, and keep-running intent. Copy creates a separate session identity; Move preserves the original. [Manage environments](../guides/managing-environments.md) covers those workflows.

Recorded activity describes Devbox operations, not filesystem changes.
