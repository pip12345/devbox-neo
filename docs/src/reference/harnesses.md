# Harnesses

A harness is the coding tool Devbox launches. Pi, OpenCode, and Claude Code are built in. Select one through your config's **Harness** setting.

## Built-in harnesses

| Harness | Default launch | Continue | Config directory in container |
|---|---|---|---|
| Pi | `pi --tui-mode fullscreen` | `-c` | `/home/devuser/.pi/agent` |
| OpenCode 2 | `opencode` | `-c` | `/home/devuser/.config/opencode` |
| Claude Code | `claude --dangerously-skip-permissions` | `--continue` | `/home/devuser/.claude` |

**Claude bypasses its permission prompts.** OpenCode's built-in launch disables sharing, automatic updates, and the `opencode` provider.

Set `harness_args` in the same config as its `harness`. Only arguments matching the final selected harness apply. They follow the built-in launch arguments; continuation and one-off arguments follow them.

For Pi's regular terminal mode, set `harness_args` to `["--tui-mode", "regular"]`. Explicit launch arguments override Pi's saved `tuiMode` setting.

### Managed JSON keys

Devbox updates these top-level keys while preserving other keys:

| Harness / file | Owned keys |
|---|---|
| Pi `settings.json` | `packages`, `extensions`, `skills`, `prompts`, `themes`, `npmCommand` |
| Pi `models.json` | `providers` (whole object) |
| Claude `settings.json` | `tui`, `pluginConfigs` |

OpenCode declares no shared-JSON merges. Claude's defaults enable fullscreen TUI and the `agents-md@builtin` plugin; its `CLAUDE.md` imports `/devbox/AGENTS.md`.

See [storage mappings](state-and-sessions.md#built-in-storage-mappings) for history, caches, and authentication paths.

## Custom definitions

Place a definition at `<home>/harnesses/<name>/harness.json`, with optional config defaults in its sibling `defaults/` directory and installation files in `install/`. A user definition replaces a built-in of the same name; an invalid override is an error.

Recreate existing sessions to adopt a changed definition. Definitions use strict JSON, version `1`; names match `[a-z][a-z0-9_-]{0,47}`.

## Fields

| Field | Meaning |
|---|---|
| `version` | `1` |
| `name` | Harness name matching its directory |
| `binary` | Non-empty executable to launch and verify |
| `install.shell` | Inline Bash installation code; cannot combine with `install.script` |
| `install.script` | Clean relative Bash entry-point path under `install/`; cannot combine with `install.shell` |
| `install.path` | Clean absolute PATH entries |
| `launch.args` | Default arguments |
| `launch.continue_args` | Arguments for `open --continue` |
| `env` | Container environment defaults; `DEVBOX_*` reserved |
| `stores` | Persistent mounts; fields below |
| `config.store` | An environment-scoped store name |
| `config.path` | Relative directory within that store; default `.` |
| `config_merge` | Shared-JSON ownership; fields below |
| `auth` | Managed authentication mounts; fields below |
| `session.clone` | Supports copying declared state |
| `session.relocate` | Supports moving declared state |
| `prepare` | Array of non-empty preparation argv arrays |

### Installation files

`install.script: "install.sh"` selects `<harness>/install/install.sh`. The image captures all regular files under `install/`; symlinks and special entries are skipped. The selected script must exist as a regular file.

Scripts run as `devuser` with Bash and `pipefail`. Resolve companion files relative to the script, and install executables outside persistent mounts. Installation files are literal, without `${user}` expansion, and must not contain credentials. Changes require recreation.

### Store entries

| Field | Rule |
|---|---|
| `name` | Valid, unique name |
| `scope` | `environment` for session state; `cache` for shared cache |
| `target` | Clean absolute path under `/home/devuser/` |

Stores cannot overlap. Writable paths outside mounts are not persistent. Keep image-installed executables outside mounted state directories.

### Shared-JSON entries

| Field | Rule |
|---|---|
| `path` | Relative `.json` path within the config directory |
| `strategy` | `json-keys` |
| `owned_keys` | Non-empty list of unique top-level key names |

Paths cannot overlap. Desired keys replace live values; omitted owned keys are removed.

### Authentication entries

| Field | Rule |
|---|---|
| `source` | Relative path beneath `<home>/auth/<harness>/`; not `.` |
| `target` | Clean absolute path under `/home/devuser/` |
| `kind` | `file` or `directory` |
| `create` | May initialize an absent source |

Auth may overlay files inside a store, but cannot hide the store itself. Targets must be unique. Auth is not transferred with session state.

## Paths and templates

Paths must stay within their declared owner, without NUL or line breaks. `${user}` expands to `devuser` in supported definition strings. Host `${env:NAME}` substitution belongs to config directories, not definitions.

Defaults use the normal [managed-file rules](configuration.md#managed-harness-configuration). Use auth or environment inputs for secrets, not install arguments or ordinary public fields.
