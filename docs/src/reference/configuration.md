# Configuration — current development checkpoint

The complete intended schema is in [the rewrite plan](../../dev/rewrite-plan.md#configuration). This page describes what the initial runtime can use now, not a completed release.

## Home

Resolution: `--home` → `DEVBOX_HOME` → `~/.devbox-neo`.

The old `~/.devbox` and its descendants are rejected. Container/session names use `devbox-`; Docker ownership labels and image tags retain the `devbox-rewrite` namespace.

## Usable fields

Global `config.json`:

| Field | Default |
|---|---|
| `version` | `1` |
| `default_profile` | empty |
| `default_harness` | empty |
| `ignore_project_overrides` | `false` |
| `global_env` | empty array; `KEY=VALUE` or `KEY` host passthrough when present |

Profile/project `config.json`:

| Field | Default | Merge |
|---|---|---|
| `version` | `1` | validate |
| `harness` | empty, then global fallback | replace |
| `on_exit` | `stop` | replace; `stop` or `running` |
| `default_shell` | `["bash"]` | replace the whole argv |
| `network` | `default` | replace; default, host, or an existing network |
| `harness_args` | empty array | append |
| `extra_env` | empty array | append `KEY=VALUE`; later assignments win |
| `extra_mounts` | empty array | append `SOURCE:/absolute/target[:options]` |
| `extra_ports` | empty array | append Docker numeric port declarations |
| `docker_args` | empty array | append validated `--option=value` or supported boolean options |
| `vscode.extensions` | empty array | append; emitted in container IDE metadata |
| `inherit_profile` | `true` | project-only participation setting |

Strict standard JSON: unknown fields, duplicate keys, comments, trailing commas, and unsupported versions fail. No old-format aliases are accepted.

Explicit `--profile` excludes every project artifact. Without it, the applicable default profile is the base and participating project configuration wins. Project `inherit_profile: false` excludes the profile, including missing/invalid profile files. Global `ignore_project_overrides` excludes the project and its inheritance setting.

## Configuration-owner commands

| Command | Contract |
|---|---|
| `profile create <name>` | Sparse `config.json` only; no prompts or implicit harness |
| `project create <folder>` | Sparse `.devbox/config.json`; existing folder required |
| `project create <folder> --from-profile <name>` | Copy supported source artifacts; write `inherit_profile: false`; refuse existing destination |
| `profile/project init <target> --harness <name>` | Select a valid effective harness without prompting |
| `project init <folder> --harness inherit` | Remove the explicit harness and validate inherited selection through the shared resolver |
| `init --artifact <name,...>` | Seed only missing `harness-config`, `setup.sh`, `entrypoint.sh`, or `Dockerfile`; `harness-config` excludes `skills/devbox/SKILL.md`, which remains an overridable harness default |
| `profile list [--json]` | Sorted profiles, default marker, and invalid-config entries |
| `profile delete <name> [--force] [--json]` | Delete profile config/artifacts only; retain global defaults, containers, and sessions. Force skips confirmation |
| `profile set [name]` / `profile set --clear` | Set or clear the global default; omitted name prompts in a terminal |
| `global config`, `profile config <name>`, `project config <folder>` | Numbered terminal menus for local settings, effective values, and reset-to-inherited; each valid operation saves immediately |

Create/init support `--json` result and next-step output, including a `warnings` array when config entries are skipped. Profile/project next-step commands carry `--home` only when it was explicitly supplied; default-home and `DEVBOX_HOME` selection are left to normal resolution. Text output separates the explanation from suggested commands. Init without flags prompts in a terminal, keeps an already selected harness, and offers optional artifacts. Non-interactive init requires an existing selection or `--harness`; it never guesses. Explicit `--harness` does not prompt for artifacts. Source edits preserve unrelated fields and expressions without expanding them. Init and profile-selection menus share the config menus' bold headings, aligned numbered choices, and width-aware wrapping. Secondary instructions are dimmed. These styles honor `NO_COLOR`, `TERM=dumb`, and non-terminal output without changing input rules.

## Substitution, environment, and creation options

`${env:NAME}` expands decoded string values from one host snapshot per operation. Property names are not expanded. Unset references fail; empty values count as present. Expansion is non-recursive and does not load `.env`, run shell commands, or interpret default-value syntax. Source copies and config edits retain expressions.

Sensitivity is determined by field: env/auth values are sensitive; names, paths, networks, argv, Docker arguments, and other ordinary settings are public, including substitutions. Do not put credentials in public fields. Config output redacts env values and reports variable references separately.

Creation environment precedence is terminal passthrough → harness defaults → global → profile → project → CLI. Within a layer, later assignments win. Explicit public raw `--env=KEY=VALUE` options take Docker CLI precedence. `DEVBOX_*` is reserved. Env values must be single-line with no NUL; creation values travel in a private env file, not process arguments.

Terminal passthrough forwards present host values for `TERM`, `COLORTERM`, `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`, `FORCE_COLOR`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, `WEZTERM_EXECUTABLE`, `KITTY_WINDOW_ID`, `VTE_VERSION`, `KONSOLE_VERSION`, and `ITERM_SESSION_ID`, including empty values. Each attached `open`, `shell`, or `exec` refreshes these values using Docker exec env overrides, even for existing containers and non-TTY commands. Unset host keys add no override. This display metadata is not recorded as env sources, persisted in session records, or included in fingerprints; changing terminals does not require recreation. Devbox does not copy host shell dotfiles or alter the recorded shell command.

`create`, `open`, and `recreate` accept `--harness`, `--harness-arg`, `--env`, `--volume`, `--port`, `--docker-arg`, `--network`, `--on-exit`, and `--read-only`. Bind sources must exist; relative extra binds resolve against the workspace, while bare source names designate user volumes. Extra/raw mounts cannot overlap workspace, runtime, store, or auth targets. Published ports are numeric, within 1–65535, with equal-size mapped ranges; host networking cannot publish ports.

Raw Docker options cannot replace identity, ownership labels, user/workdir, entrypoint, primary network, managed mounts/env, IDE metadata, or the host gateway alias. Value-taking options must use one `--option=value` token; raw bind sources must be absolute.

## Effective config display

`global config --show`, `profile config <name> --show`, and `project config <folder> --show [--profile NAME]` display the normal resolver's effective values, layers, exclusions, contributors, artifact winners, and harness origin. Human output keeps scalars and shell commands inline and prints other list entries as indented bullets, with a source beside each entry. Long values wrap rather than truncate; output uses at most 80 characters per line, or the narrower terminal width. Nested fields use dotted names such as `vscode.extensions`. Add `--json` for structured values without wrapping or flattening. `trace.entry_sources` contains source-layer names indexed in the same order as each resolved list (including duplicates); dotted keys identify nested lists such as `vscode.extensions`. A sparse owner can be inspected before selecting a harness. Without `--show`, these commands open numbered menus and require terminal input. `--json` and project config's `--profile` option require `--show`.

Menus use a short scope title and Setting/Value/Source columns. Source labels are `default`, the edited layer (`global`, `profile`, or `project`), or `inherited - global` / `inherited - profile` for lower layers. Non-empty lists leave the heading's source blank and label each entry. Sources come from resolver provenance, not key presence or value matching. Source labels and `None` are dimmed on supported terminal output; `NO_COLOR`, `TERM=dumb`, and non-terminal output disable styling. Empty values display as `None`. If effective resolution fails, displayed source values are labelled with their owning scope and unavailable values have source `unknown`. Each list entry is shown below its setting, and wrapped continuation lines have no extra bullet or selection number. Menus edit only the selected source layer. Lists open directly and edit entries configured here, not inherited entries. Add is always available; edit/remove require entries, and reset requires an existing source key. Enter submits a value; add, edit, remove, and reset each save immediately after validation, without a separate save or confirmation step. Reset deletes the source key. Saves preserve expressions and unrelated fields, validate source types and supported literal values, and reject concurrent edits to the same field. Unresolved expressions, cross-field constraints, and runtime resource checks remain the effective resolver/runtime's responsibility; a resolution error is shown without disabling local editing. Environment values are redacted in menu displays and feedback; typed input is visible, so prefer host references. `0`/`q` goes back or exits, and `:back` cancels text entry. EOF or cancellation abandons incomplete input but does not undo completed operations. Invalid or conflicting operations leave the saved field unchanged; the menu reloads its current value.

`setup.sh` runs once per successful container creation; changes require recreation. `entrypoint.sh` runs on each `open`. Both run inside the container, never on the host.

## Image inputs

An optional `Dockerfile` customizes a Debian-compatible base. Devbox builds it as an intermediate image, then always installs its runtime and selected harness on top. Without one, the base is `debian:bookworm-slim`. There is no `Dockerfile.full` mode.

The runtime layer includes Bash, CA certificates, curl, git, sudo, procps, vim, zip, unzip, jq, net-tools, and iputils-ping. Interactive Bash provides `ll='ls -alF'` and `vi='vim'` through `/etc/bash.bashrc`. These are the same for default and custom-base images; host dotfiles are not imported. Changes to bundled packages or aliases are image inputs, so ordinary `recreate` rebuilds when they change. `--image` is only needed to force a no-cache build despite unchanged inputs.

The selected Dockerfile's directory is the build context. `Dockerfile.dockerignore` takes precedence over `.dockerignore`; included regular files, directories, permissions, and ignore rules contribute to the image fingerprint. Symlinks and special files are rejected unless excluded. `HOST_UID` and `HOST_GID` build arguments are available. Ordinary builds use cache; `recreate --image` disables cache for both controlled stages.

Profile-to-project copying includes the active build context and its ignore rules, preserving context-file permissions. Existing source files are never refreshed by init.

## Harness configuration

Pi and OpenCode are embedded. A selected user definition at `harnesses/<name>/harness.json` replaces the built-in of the same name; invalid overrides never fall back. Registry enumeration reports invalid entries separately and never substitutes a built-in for an invalid override. The expanded real-Docker acceptance gate remains outstanding before schema freeze. Store/auth targets must be clean absolute paths under `/home/devuser/`; auth mounts cannot obscure declared stores. Relative paths must be canonical and remain inside their owner. Installation PATH entries are absolute paths. The config subpath defaults to `.` when omitted.

The built-in Pi launch is `pi --tui-mode fullscreen`; upstream labels fullscreen experimental. This startup flag overrides Pi's saved `tuiMode` setting. Set `harness_args` to `["--tui-mode", "regular"]` or pass `open <target> -- --tui-mode regular` to override it. `--continue` still appends `-c`. User-defined harnesses replacing Pi keep their own launch defaults. Existing recorded Pi environments require recreation to adopt the changed definition; changing launch defaults does not add a special lifecycle path or a new config field.

Desired harness files overlay defaults → participating profile → participating project. Harness config trees copy regular files and traverse directories. Symlinks and other non-regular entries are skipped with a warning naming each source path; symlinks are not followed. This also applies to user harness defaults, harness-config seeding, and profile-to-project harness config copies. Skipped entries do not override lower-layer files. Config-root symlinks and actual filesystem read errors still fail; Docker build-context rules are unchanged. Ordinary managed files preserve executable intent using private `0600`/`0700` modes; later content or mode edits cause conflicts rather than overwrites. Structured JSON preserves live permissions and undeclared keys, while the definition's owned keys follow desired config. Pi declares `settings.json` keys `packages`, `extensions`, `skills`, `prompts`, `themes`, and `npmCommand`, plus `models.json` key `providers`. The complete `providers` object follows the selected desired file; providers are not deep-merged.

Secrets are supported only through auth/environment inputs. Do not put them in launch argv, Docker arguments, or ordinary settings. Definition env is reconstructed from its verified definition source. Config env uses recorded file/field/index references with keyed fingerprints of the original expression and resolved assignment. Changing either makes exact missing-container recovery unavailable; unrelated source edits do not. CLI-only env has no durable source and requires explicit recreation with its inputs after container loss. Existing-container access does not require those old values. No env values are saved in session records.
