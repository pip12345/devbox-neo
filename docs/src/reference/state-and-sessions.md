# State and sessions

A **session** is the saved identity and harness state of an environment. Its Docker container is replaceable. The default Devbox home is `~/.devbox-neo`; `--home` or `DEVBOX_HOME` can select another home.

## Home layout

Paths below are relative to the selected home.

| Path | Purpose |
|---|---|
| `configs/<name>/` | Convenient location for named config directories |
| `harnesses/<name>/` | User harness definition and optional defaults |
| `auth/<harness>/` | Persistent managed authentication |
| `cache/harnesses/<harness>/<store>/` | Shared harness caches |
| `sessions/<container>/session.json` | Session identity, recorded creation/launch settings, applied inputs, and activity |
| `sessions/<container>/active/` | Attached-command records |
| `sessions/<container>/harnesses/<harness>/stores/<store>/` | Per-environment harness state |
| `sessions/<container>/harnesses/<harness>/managed-config.json` | Managed file/key ownership manifest |
| `sessions/<container>/runtime/ssh/` | Transient shared SSH sockets and generated client config |
| `state/installation-id` | Installation identity used for Docker ownership |
| `state/workspaces/<workspace-key>.json` | Folder default: full session name plus durable ID, or null |
| `state/transfers/<source-container>.json` | Pending transfer journal; keeps both environments reserved until completion |
| `state/locks/installation.lock` | Home initialization lock |
| `state/locks/config/*.lock` | Canonical-path-keyed config-directory locks |
| `state/locks/workspaces/<workspace-key>.lock` | Stable folder-default locks |
| `state/locks/sessions/*.operation.lock` | Environment-operation locks |
| `state/locks/sessions/*.record.lock` | Short session-record locks |

`<workspace-key>` is the full SHA-256 digest of the canonical absolute workspace path's bytes. Default records contain `version: 1`, `workspace`, and `default_session` (`name` and `id`, or null). Missing records mean no default; malformed records are errors. Reading or clearing an absent default does not create its state record.

Temporary work uses `.build-*` and `.runtime-*` under the home, private same-directory publication files, and private creation-env files in the OS temporary directory. These are not saved configuration.

## What survives

| Data | Stop/start | Recreate | Copy/move |
|---|---|---|---|
| Workspace files | Retained on host | Retained on host | Not copied; prepare destination separately |
| Declared environment stores | Retained | Retained | Copied |
| Managed-config manifest | Retained | Retained and synchronized | Copied and synchronized for destination |
| Managed auth | Retained separately | Retained separately | Not copied; destination uses managed auth |
| Shared caches | Retained separately | Retained separately | Not copied; destination uses shared caches |
| Container-local files/tools | Retained | Lost | Not copied |
| Live SSH connections | End when controller/container stops | Not retained | Not copied |

`delete --container` retains saved session data, image tags, and the folder's default selection. `delete --session` also removes saved data/history and the verified session image tag, clearing a matching default without selecting a replacement. Neither deletes workspace files, configuration, auth, or shared caches. See [deletion](commands.md#deletion).

## Built-in storage mappings

Targets are inside the container. Auth sources use the listed filenames beneath `<home>/auth/<harness>/`.

| Harness | Kind / store | Target |
|---|---|---|
| Pi | Environment: `home` | `/home/devuser/.pi/agent` |
| Pi | Cache: `npm-global` | `/home/devuser/.local` |
| Pi | Cache: `npm-cache` | `/home/devuser/.npm` |
| Pi | Auth: `auth.json` | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | Environment: `config` | `/home/devuser/.config/opencode` |
| OpenCode | Environment: `data` | `/home/devuser/.local/share/opencode` |
| OpenCode | Cache: `cache` | `/home/devuser/.cache/opencode` |
| OpenCode | Auth: `auth.json` | `/home/devuser/.local/share/opencode/auth.json` |
| Claude Code | Environment: `home` | `/home/devuser/.claude` |
| Claude Code | Auth: `.credentials.json` | `/home/devuser/.claude/.credentials.json` |
| Claude Code | Shared auth/client state: `.claude.json` | `/home/devuser/.claude.json` |

Claude's two auth files are shared across Claude sessions in the selected Devbox home and excluded from session copies. Its `.claude` environment store retains session state; it declares no shared cache.

Paths outside declared mounts remain container-local. For example, OpenCode's `/home/devuser/.local/state` is not a declared persistent store.

## Container paths

| Path | Purpose |
|---|---|
| `/workspace` | Host project bind mount |
| `/devbox/AGENTS.md` | Agent-facing container guidance |
| `/devbox/docs/index.md` | Embedded human docs |
| `/devbox/network/env` | Shell-safe inspected network facts |
| `/devbox/network/inspect.json` | Inspected network facts as JSON |
| `/devbox/ssh/config` | Available shared SSH connections |

Documentation and network files are Devbox-managed runtime data. SSH runtime data is a separate private writable mount, not a credential store.

## Names and ownership

A session is identified by its canonical workspace and explicit folder-local name. Names match `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`: 1–64 ASCII characters, beginning with a letter or digit. Case is preserved; `Main` and `main` differ. Names are never truncated or normalized.

The full session/container name is `devbox-<folder>-<12-hex-hash>.<local-name>`. The hash is the first 12 lowercase hex characters of SHA-256 over the canonical absolute workspace path. Sessions in the same workspace share this hash; the exact local name after the dot distinguishes them. The readable folder hint is lowercased, sanitized, and capped at 32 characters; the full name is at most 117 characters. Symlink aliases of a workspace share identity.

Config sources do not determine identity. Each local name needs explicit creation; a missing container does not remove the saved session. Folder-only targeting requires a default selected through `edit`, even for a sole session. Defaults pin the durable ID so reusing a deleted local name cannot silently inherit an old selection.

Names locate resources; labels prove ownership. Containers carry installation, ownership-version, session, workspace, and local-name labels under `devbox-rewrite.*`. Images retain installation ownership and `devbox-rewrite/session:<session-id>` tags.

## Record and recovery contract

Session schema 5 stores identity, editable desired `sources`, and the complete applied image/container/runtime snapshot. Each desired reference has `label`, `kind` (`relative` or `fixed`), and `path`. Relative paths resolve against the recorded workspace; fixed paths are absolute. Empty desired chains are valid for repair but not startup. Applied inputs separately retain the committed absolute source directories for environment recovery. They do not store env/auth values. Unsupported session formats require a clean development-state reset; there is no automatic migration.

`open` and `start` restore a missing container using its recorded image, mount layout, verified definition/setup inputs, and recoverable environment sources. They do not replace recorded creation settings with current configuration. Missing inputs require explicit recreation. Existing named external volumes must still exist.

Recreation keeps the session ID, local/full name, and harness state while applying the current saved source chain. It also keeps your choice to leave the container running with `start`. `copy` creates a new ID and leaves the destination stopped; `copy --move` preserves the ID and running/stopped behavior. See [lifecycle commands](commands.md#environment-lifecycle) for start/stop behavior across attachments and reboot.

`last_activity` and `last_action` describe recorded Devbox operations, not filesystem activity. List output's container creation time comes from Docker. Corrupt records remain diagnostics rather than being treated as missing state.

For locking, recovery verification, and transfer commit details, see [state architecture](../architecture/state.md).
