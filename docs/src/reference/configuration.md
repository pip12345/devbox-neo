# Configuration

## Locations and references

Home selection is `--home` → `DEVBOX_HOME` → `~/.devbox-neo`. The old `~/.devbox` home and its descendants are rejected.

Each config directory contains `config.json` and optional artifacts. Every session saves an explicit, ordered source chain. No global settings file or workspace discovery participates.

| Reference | Initial resolution | Saved form |
|---|---|---|
| `base` | `<home>/configs/base` | Fixed absolute path |
| `./devconfig`, `configs/local`, `../shared` | Relative to the invoking working directory | Workspace-relative path |
| `.` or `..` | Current or parent directory | Workspace-relative path |
| `~/configs/personal` | User-home-relative path | Fixed absolute path |
| `/absolute/config` | Absolute path | Fixed absolute path |

A reference containing `/` is a path; bare names use the selected home. Use `./base` for a local directory named `base`. There is no local-then-home fallback. Duplicate canonical source directories, including symlink aliases, are rejected.

Relative references follow the workspace during copy/move. Fixed references do not, even when they point inside the original workspace. Devbox does not copy config directories.

Files use strict JSON: unknown fields, duplicate keys, comments, trailing commas, and unsupported versions fail. Harness names match `[a-z][a-z0-9_-]{0,47}`. Session-name rules are separate; see [state and sessions](state-and-sessions.md#names-and-ownership).

## Config fields

Defaults below are applied once, before the explicit source chain. An absent field contributes nothing.

| Field | Default | Merge / accepted values |
|---|---|---|
| `version` | `1` | Schema version |
| `base_image` | `"debian:bookworm-slim"` | Replace; Debian/Ubuntu-compatible upstream image |
| `harness` | `""` | Later explicit value replaces earlier selection; runnable configuration requires a harness |
| `shell` | `["bash"]` | Replace complete non-empty argv |
| `network` | `"default"` | Replace; `default`, `host`, or existing Docker network name |
| `harness_args` | `[]` | Append only from sources explicitly naming the final selected harness; requires `harness` in the same file |
| `env` | `[]` | Append `KEY=VALUE`; later assignments to the same variable win |
| `mounts` | `[]` | Append `SOURCE:/absolute/target[:options]` |
| `ports` | `[]` | Append numeric Docker port declarations |
| `docker_args` | `[]` | Append validated Docker options |
| `vscode.extensions` | `[]` | Append extension names to container IDE metadata |

Empty additive lists do not erase earlier entries. Conflicting mounts or ports remain errors. Configs have no session-name field, inheritance cutoff, or recursive includes.

## Selection and precedence

Settings come from built-in defaults followed by the saved sources in order. All explicit sources participate. Config choices never determine or rename session identity.

Creation, opening, and recreation require at least one source, accessible valid configs, and a selected harness. Source editing may temporarily save an incomplete chain. Missing sources do not prevent lookup, listing, stopping, deletion, default selection, or opening the source-chain repair menu.

## Editing and inspection

| Command | Scope |
|---|---|
| `config create <reference>` | Create a new config and offer initial harness/artifact setup |
| `config edit <reference>` | Edit one existing directory's settings or add missing optional files |
| `config list [--json]` | Show names, harnesses, and directory paths under `<home>/configs/`, including invalid or incomplete configs; does not discover arbitrary path-based configs |
| `config delete <name> [--force] [--json]` | Remove a named config directory and all its files; `--force` skips confirmation, not reference checks |
| `config sources <folder>` | Pick a saved session, then edit its source chain |
| `config sources <folder> --name NAME` | Edit that named session's source chain directly |
| `config sources <full-name> --show [--json]` | Inspect combined settings and provenance |

For folder-targeted `--show`, supply `--name`. JSON requires `--show`; source editing otherwise requires a terminal. Config-directory setup also supports [explicit automation flags](commands.md#configuration-commands).

`config create` fails if `config.json` exists, including an empty or invalid file, and points to `config edit`. An existing directory without `config.json` is allowed; existing artifacts are kept. `config edit` never creates a missing config.

`config delete` accepts only a direct named directory under the selected home's `configs/`; symlink entries and arbitrary directory paths are refused. It can remove incomplete directories, but refuses while saved sessions use the config as a desired or committed source. The blocked-deletion error lists every known session once with a command to inspect it. The interactive directory editor shows the same plain list. Invalid session state and pending transfers block deletion because use cannot be checked completely; the editor labels a partial report. This is a check of current saved state, not an atomic guarantee against concurrent session creation or source edits.

Each completed setting/source-chain edit saves immediately. `0` or `q` navigates back or exits; `:back` cancels text input. Removing a setting removes its local key. List editors change only the selected directory's entries. Source-chain changes affect one session; directory changes affect all referencing sessions.

Saves preserve expressions and unrelated fields. A stale same-field edit or changed session/source-list snapshot is rejected. The directory dashboard shows that source over built-in defaults; combined inspection is read-only. Env values are redacted, but typed input is visible.

## Substitution, environment, and creation options

### Host substitution

`${env:NAME}` expands decoded string values from one host snapshot per operation. Unset references fail; empty values are present. Expansion is non-recursive, leaves property names untouched, and reads neither `.env` files nor shell commands. Editing preserves expressions.

Env/auth values are sensitive. Paths, names, networks, argv, and Docker options remain public even when supplied through substitution. Do not put credentials in those fields.

### Environment precedence

At creation, later values win: `terminal → harness defaults → explicit config sources in order`.

Explicit raw Docker `--env=KEY=VALUE` options take Docker CLI precedence. `DEVBOX_*` is reserved. Config `env` requires assignments, not bare passthrough names; values must be single-line and contain no NUL.

Config env recovery verifies the committed source entries and their original expansions. Editing the desired source chain does not rewrite those recovery references. Changed or missing committed entries require recreation. Existing-container access does not need old env values.

### Terminal forwarding

Attached `open`, `shell`, and `exec` commands forward present host display variables, including empty values:

`TERM`, `COLORTERM`, `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`, `FORCE_COLOR`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, `WEZTERM_EXECUTABLE`, `KITTY_WINDOW_ID`, `VTE_VERSION`, `KONSOLE_VERSION`, `ITERM_SESSION_ID`.

Unset variables add no override. These refresh per invocation, require no recreation, and are not saved configuration. Host dotfiles are not imported.

### Container settings

Set lasting settings in selected config directories. `create` and `recreate` apply them without container-setting override flags.

- **Mounts:** bind sources must exist. Relative bind sources resolve against the workspace; bare names designate user volumes. Targets cannot overlap workspace, runtime, harness stores, or auth mounts.
- **Ports:** numeric ports in `1–65535`; mapped ranges have equal sizes. Host networking cannot publish ports.
- **Raw Docker options:** value-taking options use one `--option=value` token; supported booleans may stand alone. Raw bind sources must be absolute.
- **Protected settings:** identity, ownership labels, user/workdir, entrypoint, primary network, restart policy, managed mounts/env, IDE metadata, and host gateway alias cannot be replaced.

## Artifacts

Artifacts live beside each directory's `config.json`.

| Path | Composition | Applied when |
|---|---|---|
| `Dockerfile` | Build in source order, extending the preceding image | Image build |
| `setup.sh` | Run in source order | Container creation/recreation |
| `before-open.sh` | Run in source order | Each `open`, before harness launch |
| `<harness>/` | Overlay defaults, then each source by relative path; later files win | Managed-file synchronization |

Scripts are separate development-user processes in `/workspace`, with sudo available. Failure stops the chain without rolling back completed effects. Setup changes require recreation; before-open scripts use the current chain at launch.

Creation and the editor's **Add optional files** operation add only missing files. `harness-config` omits the inherited `skills/devbox/SKILL.md`; explicit source overrides remain supported. The file-generation target is independent of the config's persistent harness selection.

## Image inputs

The selected `base_image` first receives the Devbox user/runtime. Source Dockerfiles then extend `DEVBOX_BASE` in order; Devbox installs the harness last.

Each Dockerfile starts as `devuser`, with `/home/devuser` as home and `/workspace` as workdir. Custom PATH additions carry forward. Use sudo for system changes. The base must be Debian/Ubuntu-compatible; conflicting users/UIDs fail rather than being renamed or recursively chowned. Tools hidden beneath managed mounts are not visible at runtime.

| Input / behavior | Rule |
|---|---|
| Build context | Each Dockerfile's own directory; contexts are not merged |
| Ignore rules | `Dockerfile.dockerignore` takes precedence over `.dockerignore` |
| Included entries | Regular files, directories, and permissions; exclude symlinks/special files |
| Build arguments | `DEVBOX_BASE`, `DEVBOX_USER`, `DEVBOX_USER_HOME`, `DEVBOX_WORKSPACE`, `DEVBOX_UID`, `DEVBOX_GID` |
| Ordinary build | Cache enabled |
| `recreate --image` | Disable cache for controlled stages; does not guarantee refreshed upstream images |

Build arguments are not a secret channel. `DEVBOX_WORKSPACE` is the in-container path, not host workspace access during builds.

Bundled tools include Bash, CA certificates, curl, Git, sudo, procps, OpenSSH clients, util-linux, Vim, zip, unzip, jq, net-tools, and iputils-ping. Interactive Bash provides `ll='ls -alF'` and `vi='vim'`.

## Managed harness configuration

Trees copy regular files and skip symlinks/special entries with warnings. Skipped entries do not override lower sources. Root symlinks within a harness tree and filesystem read errors fail.

| File kind | Synchronization rule |
|---|---|
| Ordinary managed file | Replace live bytes/mode; remove obsolete managed paths |
| Declared shared JSON | Replace/remove owned keys; preserve other keys and existing permissions |
| Unmanaged file | Leave untouched |

Ordinary files use private `0600`/`0700` modes. Invalid live shared JSON blocks synchronization because undeclared keys cannot be preserved safely. See [built-in harnesses](harnesses.md#built-in-harnesses) for owned keys.

Synchronization runs during creation/recreation, transfer destination creation, and before stopped-container access through `open`, `start`, `shell`, `exec`, or `ssh`. Running access does not synchronize. Changed layouts require recreation; ordinary file changes require only stop/start.
