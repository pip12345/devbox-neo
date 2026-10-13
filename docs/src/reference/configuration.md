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

Picker order: **Named configs** → **Workspace configs** → **Current-directory configs**. Relative names in the picker refer to their group's directory. Relative paths you type, including `--config` arguments, refer to your current directory.

## Config fields

Built-in defaults apply first, then selected configs in order. An absent field contributes nothing.

| Field | Default | Rule |
|---|---|---|
| `version` | `1` | Schema version |
| `base_image` | `"debian:bookworm-slim"` | Replace (Debian/Ubuntu-compatible image) |
| `harness` | `""` | Replace (required for Create/Recreate) |
| `shell` | `["bash"]` | Replace complete non-empty argv |
| `network` | `"default"` | Replace with `default`, `host`, or an existing Docker network |
| `harness_args` | `[]` | Append only from configs naming the final harness (requires `harness` in that file) |
| `env` | `[]` | Append `KEY=VALUE`. Later assignments win |
| `mounts` | `[]` | Append `SOURCE:/absolute/target[:options]` |
| `ports` | `[]` | Append numeric Docker port mappings |
| `docker_args` | `[]` | Append validated Docker options |
| `vscode.extensions` | `[]` | Append extension names |

Empty additive lists do not erase earlier values. Conflicting mounts/ports fail. JSON is strict: unknown fields, duplicate keys, comments, trailing commas, and unsupported versions are rejected.

## Selection and precedence

All selected configs participate. Later scalar values replace earlier ones. Most lists append. Config selection does not rename the session.

Create and Recreate require valid configs and a harness selection. Broken configs do not prevent access to an existing session. Saving a config selection requires at least one reference, not runnable settings.

## Editing and inspection

| Task | Command |
|---|---|
| Create a config | `dbx config create [name\|path]` |
| Edit one directory | `dbx config edit <name\|path>` |
| List named configs | `dbx config list [--json]` |
| Inspect one config over defaults | `dbx config show <name\|path> [--json]` |
| List saved-session users | `dbx config users <name\|path> [--json]` |
| Change a session's selected configs interactively | `dbx edit <folder> --name NAME` |
| Replace the complete ordered selection | `dbx edit <folder> --name NAME --config REF [--config REF…] [--json]` |
| Inspect combined values | `dbx edit <folder> --name NAME --show [--json]` |
| Delete an unused named config | `dbx config delete <name>` |

Directory edits save immediately. Every referencing session uses the changed settings on its next Recreate. Existing containers do not change when you save.

Removing a setting removes its local key. List editors change only entries stored in that directory. The directory view shows its values over built-in defaults. Combined inspection shows all selected configs and their contributions.

Creation keeps Name/location, Harness, and Optional files editable until Create config. An existing `config.json` is never overwritten. Other existing files are preserved. [Setup flags](commands.md#configuration-commands) support automation.

Config deletion accepts named directories only, not symlinks or arbitrary paths. It refuses while saved sessions still select the directory or need its previously applied files. Remove the dependency and recreate affected sessions before retrying. Incomplete usage information blocks deletion.

## Environment and container settings

### Host substitution

Use host variables in JSON string values:

```json
{
  "version": 1,
  "env": ["WORK_TOKEN=${env:WORK_TOKEN}"]
}
```

Set the variable on the host before running Devbox. Unset variables fail. Empty values are allowed. Expansion is single-pass and does not read `.env` files or execute shell commands. Editing preserves expressions.

Devbox treats env/auth values as sensitive. Environment-value input is masked in terminal editors, and env values are redacted in inspection output. **Names, paths, command arguments, and ordinary settings are not redacted** and may appear in output or saved records. Never put credentials in those fields.

### Environment precedence

At creation, later values win: terminal defaults → harness defaults → selected configs. Raw Docker `--env=KEY=VALUE` options take Docker CLI precedence.

`env` requires assignments, not bare passthrough names. Values must be single-line and contain no NUL. `DEVBOX_*` names are reserved.

### Terminal forwarding

Open, Shell, and Exec forward current host display variables without saving them as configuration:

`TERM`, `COLORTERM`, `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`, `FORCE_COLOR`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, `WEZTERM_EXECUTABLE`, `KITTY_WINDOW_ID`, `VTE_VERSION`, `KONSOLE_VERSION`, `ITERM_SESSION_ID`.

Present empty values are forwarded. Absent ones add no override. Host dotfiles are not imported.

### Container settings

| Setting | Constraints |
|---|---|
| Mounts | Bind sources must be existing directories, regular files, or Unix sockets. Relative sources use the workspace. Bare names denote volumes. Targets cannot overlap managed mounts. |
| Ports | `1–65535`. Mapped ranges must have equal sizes. Host networking cannot publish ports. |
| Raw Docker options | Value-taking options use `--option=value`. Raw bind sources must be absolute. |
| Protected values | Devbox owns identity/labels, user/workdir, entrypoint, primary network, restart policy, managed mounts/env, IDE metadata, and the host gateway alias. |

Set these in configs, not as overrides to Create/Recreate.

## Artifacts

Artifact paths are relative to the directory containing `config.json`.

| Path | Order | Runs/applies |
|---|---|---|
| `docker/Dockerfile` | Each extends the preceding image | Image build (context is `docker/`) |
| `setup.sh` | Config order | Container creation/replacement |
| `before-open.sh` | Config order | Before each Open/Continue |
| `<harness>/` | Defaults, then configs. Later paths win | Managed-file synchronization |

Run `recreate` after changing scripts or harness files. Scripts run as the development user in `/workspace`, with sudo available. They are separate processes. Failure stops the chain without undoing completed effects.

Optional-file setup adds only missing files. Harness-file generation can target a different harness from the config's selection. The built-in Devbox skill remains inherited unless explicitly overridden.

## Image inputs

Devbox prepares the base image, builds selected Dockerfiles in order, then installs the harness.

| Input | Rule |
|---|---|
| Base image | Debian/Ubuntu-compatible. Conflicting development accounts are rejected |
| Dockerfile base | `ARG DEVBOX_BASE` followed by `FROM ${DEVBOX_BASE}` |
| Initial user/home/workdir | `devuser`, `/home/devuser`, `/workspace` |
| Dockerfile location | `<config>/docker/Dockerfile` |
| Build context | Only `<config>/docker/`. `COPY`/`ADD` paths are relative to this directory |
| Ignore file | `docker/Dockerfile.dockerignore`, otherwise `docker/.dockerignore` |
| Context entries | Regular files/directories and permissions. Unignored symlinks/special files are rejected |
| Build arguments | `DEVBOX_BASE`, `DEVBOX_USER`, `DEVBOX_USER_HOME`, `DEVBOX_WORKSPACE`, `DEVBOX_UID`, `DEVBOX_GID` |
| Cache | Enabled normally. `recreate --image` disables it for controlled stages |

Only `docker/Dockerfile` contributes an image stage. Other config files, harness files, and lifecycle scripts are not build-context inputs. Resolved image settings such as `base_image` still affect the image. Scripts and harness files retain their separate application rules.

Use sudo for system packages. Custom PATH additions carry forward. Build arguments are not a secret channel. `DEVBOX_WORKSPACE` is the container path. Install executables outside directories hidden by runtime mounts.

Included tools: Bash, Git, curl, sudo, procps, OpenSSH clients, util-linux, Vim, zip/unzip, jq, net-tools, and ping. Interactive Bash provides `ll='ls -alF'` and `vi='vim'`.

## Managed harness configuration

| File kind | On synchronization |
|---|---|
| Ordinary managed file | Replace live content/mode and remove obsolete managed paths |
| Shared JSON | Replace/remove declared keys, preserving other keys and permissions |
| Unmanaged file/history | Leave untouched |

Managed configs and defaults copy regular files, excluding `.git` files and directories at every depth. Skipped symlinks/special entries produce warnings. Invalid shared JSON blocks synchronization. See [harness-owned keys](harnesses.md#built-in-harnesses).
