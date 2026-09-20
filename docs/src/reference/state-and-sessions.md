# State and sessions

A **session** is the saved identity and harness state of an environment. Its Docker container is replaceable. The default Devbox home is `~/.devbox-neo`; `--home` or `DEVBOX_HOME` can select another home.

## Home layout

Paths below are relative to the selected home.

| Path | Purpose |
|---|---|
| `config.json` | Global defaults |
| `profiles/<name>/` | Profile configuration and artifacts |
| `harnesses/<name>/` | User harness definition and optional defaults |
| `auth/<harness>/` | Persistent managed authentication |
| `cache/harnesses/<harness>/<store>/` | Shared harness caches |
| `sessions/<container>/session.json` | Session identity, recorded creation/launch settings, applied inputs, and activity |
| `sessions/<container>/active/` | Attached-command records |
| `sessions/<container>/harnesses/<harness>/stores/<store>/` | Per-environment harness state |
| `sessions/<container>/harnesses/<harness>/managed-config.json` | Managed file/key ownership manifest |
| `sessions/<container>/runtime/ssh/` | Transient shared SSH sockets and generated client config |
| `state/installation-id` | Installation identity used for Docker ownership |
| `state/transfers/<source-container>.json` | Pending transfer journal reserving both endpoints and retaining any explicit destination selector as `requested_to` |
| `state/locks/installation.lock` | Home initialization lock |
| `state/locks/config/*.lock` | Configuration-owner locks |
| `state/locks/sessions/*.operation.lock` | Environment-operation locks |
| `state/locks/sessions/*.record.lock` | Short session-record locks |

Temporary work uses `.build-*` and `.runtime-*` under the home, `.devbox-create-*` beside configuration destinations, and private creation-env files in the OS temporary directory. These are not saved configuration.

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

`delete --container` retains saved session data and image tags. `delete --session` also removes saved data/history and the verified session image tag. Neither deletes workspace files, configuration, auth, or shared caches. See [deletion](commands.md#deletion).

## Built-in storage mappings

Targets are inside the container. Auth sources are `<home>/auth/<harness>/auth.json`.

| Harness | Kind / store | Target |
|---|---|---|
| Pi | Environment: `home` | `/home/devuser/.pi/agent` |
| Pi | Cache: `npm-global` | `/home/devuser/.local` |
| Pi | Cache: `npm-cache` | `/home/devuser/.npm` |
| Pi | Auth file | `/home/devuser/.pi/agent/auth.json` |
| OpenCode | Environment: `config` | `/home/devuser/.config/opencode` |
| OpenCode | Environment: `data` | `/home/devuser/.local/share/opencode` |
| OpenCode | Cache: `cache` | `/home/devuser/.cache/opencode` |
| OpenCode | Auth file | `/home/devuser/.local/share/opencode/auth.json` |

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

Profile/project selection determines container and session-directory names:

- `devbox-<folder>-<12-hex-hash>.profile-<name>`
- `devbox-<folder>-<12-hex-hash>.profile-<name>.project`
- `devbox-<folder>-<12-hex-hash>.project`

The hash covers the canonical workspace path and selected slot. Each identity requires separate creation. Inheritance changes never silently rename saved state. The saved session and its container share the same name; a missing container does not remove the session. The readable folder portion is lowercase, sanitized, and limited to 32 characters. Symlink aliases resolve to the same workspace identity.

Names locate resources; labels prove ownership. Containers carry installation, ownership-version, session, workspace, slot, profile, and project-participation labels under `devbox-rewrite.*`. Images carry installation ownership; final tags are `devbox-rewrite/session:<session-id>`.

## Record and recovery contract

Session schema `4` requires complete `inputs.image`, `inputs.container`, and `inputs.runtime` snapshots plus ordered `sources` references. Image inputs contain the base image, generated layers, and ordered Dockerfile/context/ignore snapshots; setup and before-open inputs are ordered arrays. Older development records require a clean reset, not migration. Records contain public settings, paths, file hashes/modes, and keyed env hashes—not file contents or env/auth values. `env_sources` identifies exact recoverable source entries, restricted to the saved config sources or global config. Project sources live in the workspace's `.devbox/` directory.

`open` and `start` restore a missing container using its recorded image, mount layout, verified definition/setup inputs, and recoverable environment sources. They do not replace recorded creation settings with current configuration. Missing inputs require explicit recreation. Existing named external volumes must still exist.

Recreation preserves the session ID and recorded profile/project combination while applying those sources' current contents. `manual_start` records keep-running intent outside configuration fingerprints: manual `start` keeps the container running until `stop`, including automatic restart with Docker after reboot. Without manual start, the last attached command stops it and it does not restart at boot. `copy` allocates a new ID and starts with automatic lifetime; `copy --move` preserves identity and manual intent. Transfers retain a journal until completion and leave no permanent lineage record.

`last_activity` and `last_action` describe recorded Devbox operations, not filesystem activity. List output's container creation time comes from Docker. Corrupt records remain diagnostics rather than being treated as missing state.

For locking, recovery verification, and transfer commit details, see [state architecture](../architecture/state.md).
