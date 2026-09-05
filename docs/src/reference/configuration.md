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

Profile/project `config.json`:

| Field | Default | Merge |
|---|---|---|
| `version` | `1` | validate |
| `harness` | empty, then global fallback | replace |
| `on_exit` | `stop` | replace; `stop` or `running` |
| `default_shell` | `["bash"]` | replace the whole argv |
| `network` | `default` | replace; default, host, or an existing network |
| `harness_args` | empty array | append |
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
| `init --artifact <name,...>` | Seed only missing `harness-config`, `setup.sh`, or `entrypoint.sh` |
| `profile list [--json]` | Sorted profiles, default marker, and invalid-config entries |
| `profile set [name]` / `profile set --clear` | Set or clear the global default; omitted name prompts in a terminal |

Create/init support `--json` result and next-step output. Init without flags prompts in a terminal, keeps an already selected harness, and offers optional artifacts. Non-interactive init requires an existing selection or `--harness`; it never guesses. Explicit `--harness` does not prompt for artifacts. Source edits preserve unrelated fields and expressions without expanding them.

## Pending options

Non-empty `global_env`, `extra_env`, `extra_mounts`, `extra_ports`, `docker_args`, and `vscode.extensions` are rejected by the current runtime. `${env:...}` substitution and custom `Dockerfile`/`Dockerfile.full` build contexts also require Phase 3. These are explicit implementation gates, not removed features or ignored values.

`setup.sh` runs once per successful container creation; changes require recreation. `entrypoint.sh` runs on each root open. Both run inside the container, never on the host.

## Harness configuration

Pi and OpenCode are embedded. A selected user definition at `harnesses/<name>/harness.json` replaces the built-in of the same name; invalid overrides never fall back. Registry enumeration reports invalid entries separately and never substitutes a built-in for an invalid override. The expanded real-Docker acceptance gate remains outstanding before schema freeze. Store/auth targets must be clean absolute paths under `/home/devuser/`; auth mounts cannot obscure declared stores. Relative paths must be canonical and remain inside their owner. Installation PATH entries are absolute paths. The config subpath defaults to `.` when omitted.

Desired harness files overlay defaults → participating profile → participating project. Only regular files/directories are accepted; symlinks are rejected. Ordinary managed files preserve executable intent using private `0600`/`0700` modes; later content or mode edits cause conflicts rather than overwrites. Structured JSON preserves live permissions and undeclared keys, while the definition's owned keys follow desired config.

Secrets are supported only through auth/environment inputs. Do not put them in launch argv, Docker arguments, or ordinary settings. Definition-sourced runtime env is reconstructed from a verified source for recovery; values are not saved in session records. Values travel through a private temporary env file, not Docker process arguments; container env does not override the host client's environment. Multiline and NUL values are rejected before Docker work; broader host-env support remains pending.
