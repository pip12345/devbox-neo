# Configuration

## Locations

| Scope | File |
|---|---|
| Global | `<home>/config.json` |
| Profile | `<home>/profiles/<name>/config.json` |
| Project | `<workspace>/.devbox/config.json` |

Home selection is `--home` → `DEVBOX_HOME` → `~/.devbox-neo`. The path `~/.devbox` and its descendants are reserved and rejected.

Files use strict JSON: unknown fields, duplicate keys, comments, trailing commas, and unsupported versions fail. Profile and harness names match `[a-z][a-z0-9_-]{0,47}`.

## Global fields

| Field | Default | Meaning |
|---|---|---|
| `version` | `1` | Schema version |
| `default_profile` | `""` | Profile used without an explicit selection |
| `default_harness` | `""` | Harness fallback when the resolved selection is empty |
| `ignore_project_overrides` | `false` | Exclude all project configuration and artifacts |
| `global_env` | `[]` | `KEY=VALUE` assignments, or `KEY` to pass through a present host variable |

## Profile and project fields

| Field | Default | Merge / accepted values |
|---|---|---|
| `version` | `1` | Validate |
| `harness` | `""` | Replace; then use global fallback if empty |
| `on_exit` | `"stop"` | Replace; `stop` or `running` |
| `default_shell` | `["bash"]` | Replace entire non-empty argv |
| `network` | `"default"` | Replace; `default`, `host`, or existing Docker network name |
| `harness_args` | `[]` | Append arguments |
| `extra_env` | `[]` | Append `KEY=VALUE`; later assignments win |
| `extra_mounts` | `[]` | Append `SOURCE:/absolute/target[:options]` |
| `extra_ports` | `[]` | Append numeric Docker port declarations |
| `docker_args` | `[]` | Append validated Docker options |
| `vscode.extensions` | `[]` | Append extension names to container IDE metadata |
| `inherit_profile` | `true` | Project-only; choose whether the profile participates |

## Selection and precedence

1. Explicit `--profile` selects that profile and excludes every project artifact.
2. Otherwise, `default_profile` supplies the base profile and project settings apply above it.
3. Project `inherit_profile: false` excludes the profile, including missing or invalid profile files.
4. Global `ignore_project_overrides` excludes the project and its inheritance setting.
5. CLI overrides apply last. An empty resolved harness falls back to `default_harness`.

At least one profile or project layer must participate. Excluded layers contribute neither settings nor artifacts. Scalar and list merges follow the field table above.

## Editing and inspection

Use `global config`, `profile config <name>`, or `project config <folder>` for numbered terminal menus. Add `--show [--json]` to inspect effective values, participating layers, sources, and artifact winners without editing. Project inspection also accepts `--profile NAME`; `--json` and that profile option require `--show`.

| Menu action | Effect |
|---|---|
| Submit with Enter | Validate and save this operation immediately |
| Add / edit / remove | Change entries owned by the selected layer only |
| Reset | Remove the local source key; expose inherited values |
| `0` / `q` | Go back or exit |
| `:back` | Cancel text entry |

Completed edits remain saved on exit or cancellation. Saves preserve expressions and unrelated fields; conflicting edits to the same field fail without replacing it. Effective-resolution errors remain visible while local settings can still be edited. Env values are redacted in displays, but typed input is visible.

## Substitution, environment, and creation options

### Host substitution

`${env:NAME}` expands decoded string values from one host snapshot per operation. Unset references fail; empty values are present. Expansion is non-recursive, leaves property names untouched, and does not read `.env` files or execute shell commands. Config edits and source copies preserve expressions.

Env/auth values are sensitive. Paths, names, networks, argv, and Docker options are public even when supplied through substitution. Do not put credentials in those fields.

### Environment precedence

At creation, later values win:

`terminal → harness defaults → global → profile → project → CLI`

Explicit raw Docker `--env=KEY=VALUE` options take Docker CLI precedence. `DEVBOX_*` is reserved. Env values must be single-line and contain no NUL.

Config env can be recovered after container loss only while its recorded source entries still match. CLI-only `--env` has no durable source; supply it again with explicit `recreate` after loss. Existing-container access does not need old env values. Session records never contain env/auth values.

### Terminal forwarding

Each attached `open`, `shell`, or `exec` forwards present host display variables, including empty values:

`TERM`, `COLORTERM`, `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`, `FORCE_COLOR`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, `WEZTERM_EXECUTABLE`, `KITTY_WINDOW_ID`, `VTE_VERSION`, `KONSOLE_VERSION`, `ITERM_SESSION_ID`.

Unset variables add no override. These values refresh per invocation, do not require recreation, and are not saved configuration. Host dotfiles are not imported.

### Creation options

Container-setting flags belong to `create` and `recreate`; see the [option table](commands.md#creation-and-launch-options).

- **Mounts:** bind sources must exist. Relative bind sources resolve against the workspace; bare names designate user volumes. Targets cannot overlap the workspace, runtime, harness stores, or auth mounts.
- **Ports:** numeric ports in `1–65535`; mapped ranges must have equal sizes. Host networking cannot publish ports.
- **Raw Docker options:** value-taking options use one `--option=value` token; supported booleans may stand alone. Raw bind sources must be absolute.
- **Protected Docker settings:** identity, ownership labels, user/workdir, entrypoint, primary network, managed mounts/env, IDE metadata, and host gateway alias cannot be replaced.

## Artifacts

Artifacts live beside the profile or project `config.json`.

| Path | Resolution | Applied when |
|---|---|---|
| `Dockerfile` | Highest participating layer wins | Image build |
| `setup.sh` | Highest participating layer wins | Once per successful container creation |
| `entrypoint.sh` | Highest participating layer wins | Each `open`, before the harness |
| `<harness>/` | File overlay: harness defaults → profile → project | Managed-file synchronization |

Both scripts run inside the container. `setup.sh` changes require recreation; `entrypoint.sh` is a runtime input.

`init --artifact` seeds only missing files. `harness-config` seeds the selected harness's defaults except the inherited `skills/devbox/SKILL.md`; an explicit file at that path can override it normally.

## Image inputs

An optional Dockerfile builds a Debian-compatible base. Devbox then installs its runtime and selected harness. The default base is `debian:bookworm-slim`.

| Input / behavior | Rule |
|---|---|
| Build context | Selected Dockerfile's directory |
| Ignore rules | `Dockerfile.dockerignore` takes precedence over `.dockerignore` |
| Included entries | Regular files, directories, and their permissions; symlinks/special files must be excluded |
| Build arguments | `HOST_UID`, `HOST_GID` |
| Ordinary build | Cache enabled |
| `recreate --image` | Cache disabled for both build stages; no guarantee of refreshed upstream base images |

Bundled tools include Bash, CA certificates, curl, Git, sudo, procps, OpenSSH client tools, util-linux, Vim, zip, unzip, jq, net-tools, and iputils-ping. Interactive Bash supplies `ll='ls -alF'` and `vi='vim'`. Bundled runtime and harness installation changes are image inputs.

Profile-to-project copying includes the active build context and ignore rules, preserving file permissions.

## Managed harness configuration

Configuration trees copy regular files and skip symlinks/special entries with warnings. Skipped entries do not override lower layers. Root symlinks and filesystem read errors fail.

| File kind | Synchronization rule |
|---|---|
| Ordinary managed file | Replace live bytes/mode with desired source; remove when no longer managed |
| Declared shared JSON | Replace/remove owned keys; preserve other live keys and existing permissions |
| Unmanaged file | Leave untouched |

Ordinary files use private `0600`/`0700` modes. Invalid live shared JSON blocks synchronization because undeclared keys cannot be preserved safely. See [built-in harnesses](harnesses.md#built-in-harnesses) for Pi's owned keys.

Synchronization runs before creation/recreation, transfer destination creation, and stopped-container access through `open`, `start`, `shell`, `exec`, or `ssh`. Running access, inspection, stop, deletion, and network commands do not synchronize harness files. A changed harness definition requires recreation before its managed config can use the new layout.
