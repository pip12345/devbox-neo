# Implementation progress

The approved scope is [rewrite-plan.md](rewrite-plan.md). The migration utility remains a separate delivery described in [migration-plan.md](migration-plan.md).

## Phase 0 — complete

- Independent Go module; Makefile targets for format, unit/race tests, build, and opt-in Docker integration.
- Linux-only implementation and toolchain installer.
- Development binary `bin/devbox-neo`, default home `~/.devbox-neo`, and separate Docker resource names/ownership labels.
- `--home` > `DEVBOX_HOME` > development default; the conventional `~/.devbox` and its descendants are rejected.
- Cobra smoke tests, process-boundary Docker recorder, and a stateful Docker fake.

## Phase 1 — initial automated Docker gate reported passed

Implemented and covered by unit/fake-backed tests:

- Parsed embedded Pi definition, generic stores/auth, and ordinary/`json-keys` managed configuration.
- Pure layer/artifact selection, explicit-profile isolation, and standalone project inheritance exclusion.
- Immutable resolved inputs, keyed env/definition fingerprints, and actual image ID in the applied container fingerprint.
- One strict session record, atomic writes, stable installation identity, external operation/record locks, Linux PID/start/boot leases, and last-attached-command cleanup.
- Installation-owned image builds, session-specific final tags, and exact container ownership/instance checks.
- Explicit `create` prepares a new environment and leaves it stopped without launching its harness. Plain `open`/`start` require an existing session; `open --create` opts into creation if missing. Both retain recorded missing-container recovery without the flag. Unit/fake-backed tests cover missing-session rejection, creation/reopen, concurrency, refusal of existing/invalid state, recovery, and final-stop failure. `make test` and focused app/CLI/resource race tests pass. The real-Docker lifecycle now uses standalone create followed by plain open; it compiles with integration tags, but this updated path has not been executed here.
- Non-blocking creation drift, stopped-only config synchronization, running-container deferral, and explicit recreation with durable state preserved.
- No-cache forced rebuilds; explicit recreation builds a missing image, whereas recorded recovery refuses it.
- Per-container setup and every-open entrypoint scripts. Setup inputs belong to creation fingerprints because synchronization cannot claim a changed setup script already ran.
- Config-independent existing-container start/shell/exec and recorded recovery for the currently supported inputs. Definition-sourced env values are not stored in session JSON. Terminal display variables are forwarded at creation/recovery and refreshed for attached commands without entering session records or fingerprints; unit/fake-backed coverage passes, while the added live-Docker forwarding check and manual Bash prompt rerun remain pending.
- Failure propagation, failed-commit cleanup, cancellation cleanup, concurrent-lease safety, and preserved foreground exit/signal status.
- A custom fixture already exercises the same generic engine; this does not complete the full Phase 2 schema/acceptance gate.

The user reported the original Pi real-Docker lifecycle test passing on their Linux host. That test exercised non-interactive version launch and state preservation, not provider login or conversation continuation. Those manual checks remain unverified. The user explicitly approved continuing implementation rather than blocking on them.

## Phase 2 — candidate implemented, expanded Docker acceptance pending

- Added the OpenCode definition, separate config/data stores, shared cache, managed auth, and continuation/reset declarations.
- Built-in and user defaults share one recursive regular-file reader. Harness config trees now warn and skip symlinks and other non-regular entries instead of blocking open; source copying and seeding also report omissions. Root symlinks and read errors remain fatal. Unit/fake-backed coverage checks skips and warning propagation; real-Docker extension behavior remains unverified.
- Registry enumeration reports invalid overrides separately and retains valid choices; selected loading remains isolated from unrelated invalid definitions.
- Pi, OpenCode, and a third custom fixture pass the same fake-backed lifecycle, storage mapping, auth/cache preservation, recreation, and recovery tests.
- The real-Docker suite now covers all three definitions, creates profiles through the resource service, and checks in-container auth writes and preservation. These expanded cases have compiled but have not been executed here; schema freeze remains pending acceptance.

## Configuration-owner workflow — implemented

- `profile create|init|list|set|delete` and `project create|init` are available without hand-written JSON.
- Create publishes sparse config with no implicit harness or default profile. Init accepts explicit automation flags and terminal choices, keeps existing selections, and never overwrites artifacts.
- Optional seeds: harness config, `setup.sh`, `entrypoint.sh`, and `Dockerfile`. Existing artifacts are never overwritten.
- `project create --from-profile` copies supported source artifacts once, preserves expressions, and writes `inherit_profile: false`. It refuses existing destinations, including empty directories.
- Configuration mutations use external owner locks; Linux no-replace publication prevents replacement races. Source edits do not flatten global/host values.
- Project inheritance preview uses the normal resolver. Create/init return structured next steps scoped to the selected home.

## Layered images and container/session commands — implemented

- User-approved simplification: removed `Dockerfile.full`. Custom Debian-compatible bases always receive the Devbox runtime and harness layer.
- One immutable image plan captures the selected Dockerfile, context files/permissions, ignore rules, host-ID build arguments, and runtime layer. Forced rebuilds disable cache for both stages; temporary intermediate tags are ownership-checked before cleanup.
- Artifact-only projects participate through the shared resolver. Source copies preserve the active build context without mistaking excluded directories for harness configuration.
- Container list/status/logs/delete, bulk recreation, and network inspect/env/connect/disconnect are wired into the CLI. Inventory batches Docker inspection; status reports broken desired config separately from live state. List tables include profile and relative activity, with name/activity sorting and `--wide` for harness, exact activity/creation times, and last action. Stopped/missing rows are dimmed after alignment while running rows and separate diagnostics stay undimmed; terminal capability/opt-out checks preserve plain output.
- Exact `open` targets keep their recorded slots, and explicit-profile access avoids unrelated corrupt session records.
- Container deletion preserves durable state and image tags; complete selection locks and preflight precede bulk mutations. Fully labelled recordless owned containers can be deleted without adoption.
- Session list/show/reset/prune/delete are wired. Reset requires stopped/absent containers and idle leases, preserves declared history by default, and never clears auth/shared caches or stable bind roots. Dry runs do not reap leases or change files.
- Session deletion requires container absence, verifies image-tag association, and keeps external locks stable. Prune requires filters plus confirmation and rechecks age under lock.
- These additions pass unit/fake-backed checks; real-Docker acceptance for the new image/network/session cases remains outstanding.

## Configuration and environment — implemented

- User-approved sensitivity rule: env/auth values are sensitive; ordinary configuration names, paths, networks, argv, and Docker arguments are public fields, including their substitutions.
- `${env:NAME}` expands decoded strings from a captured host snapshot, not keys/raw JSON. Unset references fail; empty values are present; replacement text is not expanded recursively.
- Global passthrough and layer/CLI env are supported. Config env recovery uses verified source entries; CLI-only values remain unavailable for exact missing-container recovery. Session records never contain env values.
- Extra bind/volume mounts, published ports, protected raw Docker options, IDE metadata, and root/recreate creation flags are implemented. Host networking rejects published ports and managed targets/labels/env remain protected.
- Scoped global/profile/project config `--show` and JSON output use the shared resolver, expose origins/exclusions/references, and redact env. Numbered settings menus support scalar/list edits and reset-to-inherited without new dependencies. Each valid operation saves immediately; Back only navigates, with no draft or confirmation stage. Source edits use owner locks and same-field conflict checks, retain expressions and unrelated concurrent edits, and never save redacted display values. Resolution errors remain visible while local editing stays available. Config submenus, profile/project init, and profile selection share bold headings, aligned choices, wrapped text, and dim secondary instructions without changing input rules. Automated coverage includes Linux pseudo-terminal config/init commands and styled/plain list/menu checks; manual terminal usability acceptance remains pending.

## Runtime documentation and network files — implemented

- Embedded human docs, linked development notes, and `/devbox/AGENTS.md` are copied into verified running containers with root ownership and read-only access for the container user.
- `/devbox/network/env` and `/devbox/network/inspect.json` use fresh Docker facts, not persisted session authority. Preparation/access and managed network changes refresh them.
- Pi/OpenCode defaults include the generic `devbox` skill pointing to the runtime docs. Init leaves it inherited rather than copying it into profile/project config; explicit user overrides still take precedence. Init remains non-overwriting.
- Unit tests cover bundle links, network contents/refresh, staging cleanup, failed-copy launch prevention, and updated seed counts. The real-Docker suite now checks runtime file readability/permissions, but has only been compiled here.

## Phase 5 — transfers implemented, broader acceptance pending

- Clone/relocate share one explicit state machine, sorted endpoint locks, and a single external journal reserving both names. Clone allocates a new ID and requires a stopped/absent source; relocate preserves ID and restores running intent.
- Copies declared environment stores and managed-config ownership manifests. Auth overlays, shared caches, leases, workspace files, and container-layer data are excluded. Opaque links are preserved without traversal; unsupported special files fail clearly.
- Destination creation resolves normal destination configuration. Same-folder `--from`/`--to` slots map exactly to profiles or `.project`; cross-folder clone may override the destination profile. Dry run does not create session state or journals.
- Preparation failures attempt bounded destination cleanup and source restart. Pending retries preserve the allocated identity and recopy the authoritative source with unchanged destination inputs. Committed retries use recorded destination recovery and finish source cleanup without recopying or consulting desired config.
- Pending state appears in inventory/show and blocks ordinary mutations, including forced deletion. External journals remain discoverable after source-directory deletion; completion removes the journal without permanent lineage.
- Fake-backed tests cover running/stopped rules, IDs/state, same-folder slots, dry run, ownership/lease rejection, portability policy, auth/cache exclusions, cancellation, incomplete preparation, retained prepared destinations, committed cleanup interruption, and recovery after source deletion/container loss.
- Real-Docker transfer acceptance and process-kill/power-loss testing remain unrun; fake-backed failures are not evidence of those gates passing.

## Mount-parent permissions — regression fix awaiting Docker rerun

- The user's expanded Linux Docker run failed in OpenCode 1.18.29: `EACCES` creating `/home/devuser/.local/state` after a successful image build. The generated image had not prepared `.local/share`, the parent of its data mount.
- Generic mount-parent planning now separates image-owned ancestors from ancestors inside other managed mounts. The runtime layer creates/checks image parents as `devuser`; recorded create/start prepares nested parents in host store/auth/cache sources. No recursive chown, new mounts, or persistence mapping changes were added.
- Unit tests cover parent ownership classification, literal path arguments, both image build modes, nested auth/cache parents, reset/start restoration without desired config, and rejection of missing roots/symlinks. The Docker gate probes sibling-directory creation for all three harnesses before and after reset/start; the custom fixture now uses nested state/auth targets.
- The corrected Docker gate has not been rerun here. The reported failure is not marked resolved by a real-Docker pass.

## Layered-build base reference — regression fix awaiting Docker rerun

- The user's custom-base build succeeded, but BuildKit interpreted the runtime's `FROM sha256:...` as a Docker Hub reference and failed to pull it.
- The runtime build now references the unique temporary base tag; cleanup still verifies the recorded image ID and installation ownership. Regression tests check the generated reference and cleanup after success or runtime-build failure.
- A real-Docker rerun of this fix remains pending; fake-backed tests do not validate BuildKit resolution.

## Tools, session listing, and completion — implemented

- The mandatory runtime layer restores vim, zip, unzip, jq, net-tools, iputils-ping, and interactive Bash aliases `ll='ls -alF'` / `vi='vim'`. Both image modes share these inputs; ordinary recreation rebuilds when they change. No lifecycle changes or tmux installation were added.
- Session listing now has name, harness, profile, last activity, container cross-reference, and folder columns. It shares existing formatting/styling and name/activity sorting, retains missing-container/corrupt/pending entries, and applies the same sorting to JSON. Prune flags and deletion behavior are unchanged.
- Cobra completion suggests session/live-container targets, profiles, harnesses, transfer slots, and fixed option values. It bypasses initialization and locking readers, honors home selection, and uses bounded read-only Docker inventory. Tests verify no host writes, source-failure handling, invalid overrides, argument positions, and installation isolation.
- Guide/reference/architecture docs and embedded guidance are aligned. A general doctor command is [not planned](doctor-plan.md); status and actionable command failures cover the selected workflows.
- Rewrite `make check` and parent `make test` pass; `go vet ./...` passes and integration-tagged tests compile. The added real-Docker tool/alias check is unrun because this workspace has no Docker CLI. Actual shell completion in an interactive host shell also remains a manual acceptance check; automated tests exercise Cobra's completion protocol. Generated scripts now register both `devbox-neo` and an existing `dbx` shortcut without defining aliases. Bash subprocess tests cover alias/function preservation and flag forwarding; all four shell generators and `--no-descriptions` are covered. Live Zsh/Fish/PowerShell checks remain unrun.

## Bulk status and early drift warning — implemented

- `status --all [--profile NAME] [--json]` shows existing managed containers with separate live state and local-input drift. It shares inventory ownership/instance checks and single-target desired comparison, retains per-container failures, and excludes missing-container sessions. Ordinary listing remains free of desired resolution.
- Container/image drift recommends ordinary recreation; runtime changes do not imply rebuilding. `environment.Inputs` now supplies both fingerprints and detailed reasons shared by open and single/bulk status, including safe setting values, changed files/permissions, and env variable names without values. Upstream-version discovery and general doctor checks remain out of scope.
- Session schema 2 requires a complete applied-input snapshot and validates it against committed fingerprints. Creation/recreation commits all inputs; runtime application advances its snapshot and hash together, including recovery. Transfers commit destination inputs. Older development records require a clean reset; no compatibility, migration, or guessed baselines were added.
- Open reports creation drift before resolution warnings, recovery, synchronization, startup, and entrypoint/harness output, then continues immediately without an artificial delay.
- Fake-backed tests cover drift classifications, broken records/config, pending transfers, profile selection, Docker inventory failure, JSON/table output, warning ordering, immediate continuation, cancellation, detailed reason coverage/redaction, strict record validation, baseline commit/failure/defer behavior, and transfer destination baselines. Rewrite `make test` and `make check`, plus parent `make test`, pass. Live-Docker acceptance has not been run because this workspace has no Docker CLI.

## Shared command errors and Pi fullscreen — implemented

- Resource and lifecycle failures share `commanderror.Error`/`Step`; the process boundary prints errors once. Existing JSON commands emit structured failures on stdout, while streaming commands keep human stderr and unchanged child output. Codes/targets/next steps come from owners, not message matching; causes and foreground exit statuses are retained. Unknown errors remain `command_failed` rather than guessed categories.
- Human errors now use short `Error:` messages and separate target context, without internal code/operation banners. Existing step reasons label copyable commands, with `Then`/`Or` distinguishing sequences and alternatives; generic creation guidance leads with `create`, with `open --create` as a shortcut. These hints omit profile flags and use normal configuration selection; profile-default setup is labeled optional. Resource success guidance shares the labeled renderer. JSON fields, error codes, private causes, and exit statuses remain intact. Exact-output, action-label escaping, and existing JSON/privacy tests pass, along with `make test`, affected-package race tests, vet, build, and integration-test compilation. A temporary-home CLI smoke check confirms the missing-environment output and nonzero exit; live Docker remains unrun.
- Added actionable handling for configuration/harness selection, missing/ambiguous targets, invalid records, missing containers, active leases, ownership/instance failures, managed-file conflicts, recovery, pending transfers, and Docker failures. Retry argv uses journal endpoints and retains explicit home selection. No automatic repair or force-next-step behavior was added.
- Pi defaults to `--tui-mode fullscreen` through its built-in launch definition; profile/project harness arguments or one-off `--tui-mode regular` override it. Existing recorded environments require recreation to adopt the definition. Fullscreen is experimental upstream; live Pi acceptance remains unrun here.
- Unit tests cover JSON/human routing, private causes, next-step scoping, cleanup diagnostics, exit codes, absence/corruption boundaries, transfer retry commands, and Pi launch/override ordering. Rewrite `make test`/`make check`, vet, integration-test compilation, and parent `make test` pass. Live Docker and fullscreen UI checks remain unrun here.
- The executable remains `devbox-neo`. The obsolete rename task and deferred doctor scope were removed.

## Validation

- `make check`: unit tests, race tests, and build pass.
- Terminal forwarding and list/menu styling update: `make test`, `make test-race`, and `make build` pass; integration-tagged tests compile. Live terminal/Docker acceptance remains pending.
- `go vet ./...`: passes.
- `make install-go`: downloaded and checksum-verified the pinned Linux amd64 toolchain successfully; arm64 has a verified official checksum but was not executed here.
- Integration-tagged tests compile.
- Original Pi `make test-integration`: **passed on the user's Linux host, per user report**.
- Expanded Pi/OpenCode/custom `make test-integration`: **not run here** because this workspace has no Docker CLI/socket. The target fails explicitly rather than reporting a skipped gate as a pass.
- The parent repository's `make test` remains a separate check; it does not validate the rewrite.

Run the gate on a Linux host as a non-root user with Docker access:

```sh
cd rewrite
make test-integration
```

It uses temporary homes and rewrite-only ownership labels, not the existing installation or the persistent `~/.devbox-neo` home.

## Remaining work

Continue the runtime/configuration work; do not claim interactive harness acceptance or schema freeze from fake-backed tests.

- Phase 2: expanded real-Docker built-in/custom mapping and auth acceptance, provider login/continuation checks, and schema freeze.
- Phase 3: remaining source-snapshot/provenance hardening and real-Docker acceptance for expanded creation inputs.
- Phase 4: remaining target/creation-option integration, runtime-copy performance hardening, and expanded real-Docker lifecycle/crash testing.
- Phase 5: real-Docker transfer/kill-point acceptance and additional acceptance coverage for reset/prune/delete.
- Phase 6: remaining guided artifact workflow audit, menu usability acceptance, and documentation coverage; shared error/next-step handling is implemented and a doctor command is not planned.
- Phase 7: release hardening, performance/secret audits, and remaining acceptance tests.
- Separate migration utility: not implemented.

Container creation env uses a private temporary env file, keeping configured sensitive values out of process arguments and avoiding container-env overrides of the host Docker client's environment. Attached commands forward only the captured terminal display allowlist through Docker exec env arguments. Multiline/NUL env values are rejected before Docker work.

Unsupported inputs are rejected instead of being silently ignored. Initial-creation failures can leave uncommitted host state when it cannot be safely classified; retries refuse to adopt it. This is not a production-ready cutover build.
