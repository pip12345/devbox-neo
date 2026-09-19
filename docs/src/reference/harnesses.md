# Harness definitions

A harness is a coding tool Devbox launches, such as Pi or OpenCode. Its definition specifies installation, launch commands, persistent files, and configuration management. Pi and OpenCode definitions are built in.

## Built-in harnesses

| Harness | Launch | Continuation | Config target |
|---|---|---|---|
| Pi | `pi --tui-mode fullscreen` | `-c` | `/home/devuser/.pi/agent` |
| OpenCode | `opencode` | `-c` | `/home/devuser/.config/opencode` |

Configured `harness_args` follow built-in launch arguments. Configured arguments require `harness` in the same file; layers naming other harnesses contribute no arguments. `open --continue` adds continuation arguments; one-off `--harness-arg` values and arguments after `--` follow, without being saved. For Pi regular mode, set `harness_args` to `["--tui-mode", "regular"]` or pass those arguments after `open <folder|session> --`. The built-in fullscreen flag takes precedence over Pi's saved `tuiMode` setting.

Pi's shared JSON ownership is:

| File | Devbox-owned top-level keys |
|---|---|
| `settings.json` | `packages`, `extensions`, `skills`, `prompts`, `themes`, `npmCommand` |
| `models.json` | `providers` |

The whole `providers` object follows the selected desired file; providers are not deep-merged. Other top-level keys remain under Pi's control. OpenCode declares no shared-JSON key merges.

See [state mappings](state-and-sessions.md#built-in-storage-mappings) for stores, caches, and authentication targets.

## Definition location and selection

User definitions live at `<home>/harnesses/<name>/harness.json`; optional default config files live in its sibling `defaults/` directory. A user definition replaces the built-in with the same name. An invalid selected override fails rather than falling back.

Changing a definition requires recreation of environments that recorded the previous one. Lifecycle behavior is driven by declarations, not special cases for built-in names.

## Fields

Definitions use strict JSON and schema version `1`.

| Field | Type / meaning |
|---|---|
| `version` | Integer; `1` |
| `name` | Valid harness name matching its directory |
| `binary` | Non-empty command to launch and verify |
| `install.shell` | Shell code run during harness installation |
| `install.path` | Absolute, clean PATH entries |
| `launch.args` | Default launch arguments |
| `launch.continue_args` | Arguments added by `open --continue` |
| `env` | String map of container environment defaults; `DEVBOX_*` names reserved |
| `stores` | Named persistent mounts; entries below |
| `config.store` | Name of an environment-scoped store |
| `config.path` | Relative config directory within that store; default `.` |
| `config_merge` | Shared-JSON ownership declarations; entries below |
| `auth` | Managed authentication mounts; entries below |
| `session.clone` | Whether declared state supports cloning |
| `session.relocate` | Whether declared state supports relocation |
| `prepare` | Array of non-empty argv arrays for container preparation |

### Store entries

| Field | Rule |
|---|---|
| `name` | Valid, unique name |
| `scope` | `environment` for per-session state; `cache` for shared harness cache |
| `target` | Clean absolute path under `/home/devuser/` |

Store targets cannot overlap each other. A writable directory is not persistent unless covered by a mount.

### Shared-JSON entries

| Field | Rule |
|---|---|
| `path` | Canonical relative `.json` file path within config; not `.` |
| `strategy` | `json-keys` |
| `owned_keys` | Non-empty list of unique, non-empty top-level key names |

Merge paths cannot overlap. Desired owned keys replace live values; missing owned keys are removed. Undeclared keys survive.

### Authentication entries

| Field | Rule |
|---|---|
| `source` | Canonical relative path beneath `<home>/auth/<harness>/`; not `.` |
| `target` | Clean absolute path under `/home/devuser/` |
| `kind` | `file` or `directory` |
| `create` | Whether Devbox may initialize an absent source |

Auth can overlay a path inside a store, but cannot obscure a declared store. Targets must be unique. Auth is separate from transferable session state.

## Paths and templates

Definition path fields must remain inside their declared owner. Absolute paths must be clean; relative paths cannot escape their root. Paths containing NUL or line breaks are invalid.

The `${user}` template expands to `devuser` in executable/install/launch strings, env values, store targets, config path, auth paths, and preparation argv. Host `${env:NAME}` substitution belongs to profile/project/global configuration, not harness definitions.

Default config trees follow the same [regular-file and overlay rules](configuration.md#managed-harness-configuration) as profile/project harness config. Use auth or environment inputs for secrets, not installation arguments or ordinary config fields.
