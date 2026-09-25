# Configuration

A config directory contains `config.json` and optional artifacts. Sessions explicitly select an ordered list of these directories. For a walkthrough, see [Choose configs](../guides/configuration.md).

## Locations and references

Home selection: `--home` → `DEVBOX_HOME` → `~/.devbox-neo`. The old `~/.devbox` home and its descendants are rejected.

| Reference | Resolves from | After copy/move |
|---|---|---|
| `base` | `<home>/configs/base` | Same fixed path |
| `./devconfig`, `../shared`, `configs/local`, `.`, `..` | Invoking working directory | Follows the session workspace |
| `~/configs/personal` | User home | Same fixed path |
| `/absolute/config` | Absolute path | Same fixed path |

Use `./base` for a local directory named `base`. Duplicate directories, including symlink aliases, are rejected. Config directories are not copied during session transfers.

## Config fields

Built-in defaults apply first, then selected configs in order. An absent field contributes nothing.

| Field | Default | Rule |
|---|---|---|
| `version` | `1` | Schema version |
| `base_image` | `"debian:bookworm-slim"` | Replace; Debian/Ubuntu-compatible image |
| `harness` | `""` | Replace; required for a runnable session |
| `shell` | `["bash"]` | Replace complete non-empty argv |
| `network` | `"default"` | Replace; `default`, `host`, or an existing Docker network |
| `harness_args` | `[]` | Append only from configs naming the final harness; requires `harness` in that file |
| `env` | `[]` | Append `KEY=VALUE`; later assignments win |
| `mounts` | `[]` | Append `SOURCE:/absolute/target[:options]` |
| `ports` | `[]` | Append numeric Docker port mappings |
| `docker_args` | `[]` | Append validated Docker options |
| `vscode.extensions` | `[]` | Append extension names |

Empty additive lists do not erase earlier values. Conflicting mounts/ports fail. JSON is strict: unknown fields, duplicate keys, comments, trailing commas, and unsupported versions are rejected.

## Selection and precedence

All selected configs participate. Later scalar values replace earlier ones; most lists append. Config selection does not rename the session.

Creation, Open, and Recreate require valid configs and a final harness selection. The config-selection editor can save an incomplete list for repair. Broken configs do not prevent selecting a session or opening its repair menu.

## Editing and inspection

| Task | Command |
|---|---|
| Create a config | `config create [name\|path]` |
| Edit one directory | `config edit <name\|path>` |
| List named configs | `config list [--json]` |
| Change a session's selected configs | `edit <folder> --name NAME` |
| Inspect combined values | `edit <folder> --name NAME --show [--json]` |
| Delete an unused named config | `config delete <name>` |

Directory edits save immediately and affect every referencing session. Removing a setting removes its local key; list editors change only entries stored in that directory. The directory view shows its values over built-in defaults; combined inspection shows all selected configs and their contributions.

Creation keeps Name/location, Harness, and Optional files editable until Create config. An existing `config.json` is never overwritten. Other existing files are preserved. [Setup flags](commands.md#configuration-commands) support automation.

Config deletion accepts named directories only, not symlinks or arbitrary paths. It refuses while saved sessions still select the directory or need its previously applied files. Remove the dependency and recreate affected sessions before retrying. Incomplete usage information blocks deletion.

## Substitution, environment, and creation options

### Host substitution

Use host variables in JSON string values:

```json
{
  "version": 1,
  "env": ["WORK_TOKEN=${env:WORK_TOKEN}"]
}
```

Set the variable on the host before running Devbox. Unset variables fail; empty values are allowed. Expansion is single-pass and does not read `.env` files or execute shell commands. Editing preserves expressions.

**Env/auth values are sensitive; names, paths, argv, and ordinary settings are not.** Do not put credentials in public fields. Menu input is visible.

### Environment precedence

At creation, later values win: terminal defaults → harness defaults → selected configs. Raw Docker `--env=KEY=VALUE` options take Docker CLI precedence.

`env` requires assignments, not bare passthrough names. Values must be single-line and contain no NUL. `DEVBOX_*` names are reserved.

### Terminal forwarding

Open, Shell, and Exec forward current host display variables without saving them as configuration:

`TERM`, `COLORTERM`, `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`, `FORCE_COLOR`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, `WEZTERM_EXECUTABLE`, `KITTY_WINDOW_ID`, `VTE_VERSION`, `KONSOLE_VERSION`, `ITERM_SESSION_ID`.

Present empty values are forwarded; absent ones add no override. Host dotfiles are not imported.

### Container settings

| Setting | Constraints |
|---|---|
| Mounts | Bind sources must exist. Relative sources use the workspace; bare names denote volumes. Targets cannot overlap managed mounts. |
| Ports | `1–65535`; mapped ranges must have equal sizes. Host networking cannot publish ports. |
| Raw Docker options | Value-taking options use `--option=value`; raw bind sources must be absolute. |
| Protected values | Devbox owns identity/labels, user/workdir, entrypoint, primary network, restart policy, managed mounts/env, IDE metadata, and the host gateway alias. |

Set these in configs, not as overrides to Create/Recreate.

## Artifacts

Artifacts live beside `config.json`.

| Path | Order | Runs/applies |
|---|---|---|
| `Dockerfile` | Each extends the preceding image | Image build |
| `setup.sh` | Config order | Container creation/recreation |
| `before-open.sh` | Config order | Before each harness launch |
| `<harness>/` | Defaults, then configs; later paths win | Managed-file synchronization |

Scripts run as the development user in `/workspace`, with sudo available. They are separate processes. Failure stops the chain without undoing completed effects.

Optional-file setup adds only missing files. Harness-file generation can target a different harness from the config's selection. The built-in Devbox skill remains inherited unless explicitly overridden.

## Image inputs

Devbox prepares the base image, builds selected Dockerfiles in order, then installs the harness.

| Input | Rule |
|---|---|
| Base image | Debian/Ubuntu-compatible; conflicting development accounts are rejected |
| Dockerfile base | `ARG DEVBOX_BASE` followed by `FROM ${DEVBOX_BASE}` |
| Initial user/home/workdir | `devuser`, `/home/devuser`, `/workspace` |
| Build context | Each Dockerfile's own directory |
| Ignore file | `Dockerfile.dockerignore`, otherwise `.dockerignore` |
| Context entries | Regular files/directories and permissions; unignored symlinks/special files are rejected |
| Build arguments | `DEVBOX_BASE`, `DEVBOX_USER`, `DEVBOX_USER_HOME`, `DEVBOX_WORKSPACE`, `DEVBOX_UID`, `DEVBOX_GID` |
| Cache | Enabled normally; `recreate --image` disables it for controlled stages |

Use sudo for system packages. Custom PATH additions carry forward. Build arguments are not a secret channel; `DEVBOX_WORKSPACE` is the container path. Install executables outside directories hidden by runtime mounts.

Included tools: Bash, Git, curl, sudo, procps, OpenSSH clients, util-linux, Vim, zip/unzip, jq, net-tools, and ping. Interactive Bash provides `ll='ls -alF'` and `vi='vim'`.

## Managed harness configuration

| File kind | On synchronization |
|---|---|
| Ordinary managed file | Replace live content/mode; remove obsolete managed paths |
| Shared JSON | Replace/remove declared keys; preserve other keys and permissions |
| Unmanaged file/history | Leave untouched |

Trees copy regular files; skipped symlinks/special entries produce warnings. Invalid shared JSON blocks synchronization. See [harness-owned keys](harnesses.md#built-in-harnesses).

Files synchronize on creation/recreation and before starting a stopped container. Attaching to an already-running container does not synchronize them. Changed harness layouts require recreation.
