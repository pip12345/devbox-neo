# Configuration — current development checkpoint

The complete intended schema is in [the rewrite plan](../../dev/rewrite-plan.md#configuration). This page describes what the initial runtime can use now, not a completed release.

## Home

Resolution: `--home` → `DEVBOX_HOME` → `~/.devbox-neo`.

The old `~/.devbox` and its descendants are rejected. Docker resources use the `devbox-rewrite` namespace, including ownership labels.

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
| `init --artifact <name,...>` | Seed only missing `harness-config`, `setup.sh`, `entrypoint.sh`, or `Dockerfile` |
| `profile list [--json]` | Sorted profiles, default marker, and invalid-config entries |
| `profile delete <name> [--force] [--json]` | Delete profile config/artifacts only; retain global defaults, containers, and sessions. Force skips confirmation |
| `profile set [name]` / `profile set --clear` | Set or clear the global default; omitted name prompts in a terminal |

Create/init support `--json` result and next-step output. Profile/project next-step commands carry `--home` only when it was explicitly supplied; default-home and `DEVBOX_HOME` selection are left to normal resolution. Text output separates the explanation from suggested commands. Init without flags prompts in a terminal, keeps an already selected harness, and offers optional artifacts. Non-interactive init requires an existing selection or `--harness`; it never guesses. Explicit `--harness` does not prompt for artifacts. Source edits preserve unrelated fields and expressions without expanding them.

## Substitution, environment, and creation options

`${env:NAME}` expands decoded string values from one host snapshot per operation. Property names are not expanded. Unset references fail; empty values count as present. Expansion is non-recursive and does not load `.env`, run shell commands, or interpret default-value syntax. Source copies and config edits retain expressions.

Sensitivity is determined by field: env/auth values are sensitive; names, paths, networks, argv, Docker arguments, and other ordinary settings are public, including substitutions. Do not put credentials in public fields. Config output redacts env values and reports variable references separately.

Environment precedence is harness defaults → global → profile → project → CLI. Within a layer, later assignments win. Explicit public raw `--env=KEY=VALUE` options take Docker CLI precedence. `DEVBOX_*` is reserved. Env values must be single-line with no NUL; values travel in a private env file, not process arguments.

Root open and recreate accept `--harness`, `--harness-arg`, `--env`, `--volume`, `--port`, `--docker-arg`, `--network`, `--on-exit`, and `--read-only`. Bind sources must exist; relative extra binds resolve against the workspace, while bare source names designate user volumes. Extra/raw mounts cannot overlap workspace, runtime, store, or auth targets. Published ports are numeric, within 1–65535, with equal-size mapped ranges; host networking cannot publish ports.

Raw Docker options cannot replace identity, ownership labels, user/workdir, entrypoint, primary network, managed mounts/env, IDE metadata, or the host gateway alias. Value-taking options must use one `--option=value` token; raw bind sources must be absolute.

## Effective config display

`global config --show`, `profile config <name> --show`, and `project config <folder> --show [--profile NAME]` display the normal resolver's effective values, layers, exclusions, contributors, artifact winners, and harness origin. Add `--json` for structured output. A sparse owner can be inspected before selecting a harness. Interactive dashboards remain pending.

`setup.sh` runs once per successful container creation; changes require recreation. `entrypoint.sh` runs on each root open. Both run inside the container, never on the host.

## Image inputs

An optional `Dockerfile` customizes a Debian-compatible base. Devbox builds it as an intermediate image, then always installs its runtime and selected harness on top. Without one, the base is `debian:bookworm-slim`. There is no `Dockerfile.full` mode.

The selected Dockerfile's directory is the build context. `Dockerfile.dockerignore` takes precedence over `.dockerignore`; included regular files, directories, permissions, and ignore rules contribute to the image fingerprint. Symlinks and special files are rejected unless excluded. `HOST_UID` and `HOST_GID` build arguments are available. Ordinary builds use cache; `recreate --image` disables cache for both controlled stages.

Profile-to-project copying includes the active build context and its ignore rules, preserving context-file permissions. Existing source files are never refreshed by init.

## Harness configuration

Pi and OpenCode are embedded. A selected user definition at `harnesses/<name>/harness.json` replaces the built-in of the same name; invalid overrides never fall back. Registry enumeration reports invalid entries separately and never substitutes a built-in for an invalid override. The expanded real-Docker acceptance gate remains outstanding before schema freeze. Store/auth targets must be clean absolute paths under `/home/devuser/`; auth mounts cannot obscure declared stores. Relative paths must be canonical and remain inside their owner. Installation PATH entries are absolute paths. The config subpath defaults to `.` when omitted.

Desired harness files overlay defaults → participating profile → participating project. Only regular files/directories are accepted; symlinks are rejected. Ordinary managed files preserve executable intent using private `0600`/`0700` modes; later content or mode edits cause conflicts rather than overwrites. Structured JSON preserves live permissions and undeclared keys, while the definition's owned keys follow desired config.

Secrets are supported only through auth/environment inputs. Do not put them in launch argv, Docker arguments, or ordinary settings. Definition env is reconstructed from its verified definition source. Config env uses recorded file/field/index references with keyed fingerprints of the original expression and resolved assignment. Changing either makes exact missing-container recovery unavailable; unrelated source edits do not. CLI-only env has no durable source and requires explicit recreation with its inputs after container loss. Existing-container access does not require those old values. No env values are saved in session records.
