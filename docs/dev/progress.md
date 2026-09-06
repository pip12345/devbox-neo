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
- Create/open/reopen, non-blocking creation drift, stopped-only config synchronization, running-container deferral, and explicit recreation with durable state preserved.
- No-cache forced rebuilds; explicit recreation builds a missing image, whereas recorded recovery refuses it.
- Per-container setup and every-open entrypoint scripts. Setup inputs belong to creation fingerprints because synchronization cannot claim a changed setup script already ran.
- Config-independent existing-container start/shell/exec and recorded recovery for the currently supported inputs. Definition-sourced env values are not stored in session JSON.
- Failure propagation, failed-commit cleanup, cancellation cleanup, concurrent-lease safety, and preserved foreground exit/signal status.
- A custom fixture already exercises the same generic engine; this does not complete the full Phase 2 schema/acceptance gate.

The user reported the original Pi real-Docker lifecycle test passing on their Linux host. That test exercised non-interactive version launch and state preservation, not provider login or conversation continuation. Those manual checks remain unverified. The user explicitly approved continuing implementation rather than blocking on them.

## Phase 2 — candidate implemented, expanded Docker acceptance pending

- Added the OpenCode definition, separate config/data stores, shared cache, managed auth, and continuation/reset declarations.
- Built-in and user defaults share one recursive regular-file reader.
- Registry enumeration reports invalid overrides separately and retains valid choices; selected loading remains isolated from unrelated invalid definitions.
- Pi, OpenCode, and a third custom fixture pass the same fake-backed lifecycle, storage mapping, auth/cache preservation, recreation, and recovery tests.
- The real-Docker suite now covers all three definitions, creates profiles through the resource service, and checks in-container auth writes and preservation. These expanded cases have compiled but have not been executed here; schema freeze remains pending acceptance.

## Configuration-owner workflow — implemented ahead of dashboards

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
- Container list/status/logs/delete, bulk recreation, and network inspect/env/connect/disconnect are wired into the CLI. Inventory batches Docker inspection; status reports broken desired config separately from live state.
- Exact root targets keep their recorded slots, and explicit-profile access avoids unrelated corrupt session records.
- Container deletion preserves durable state and image tags; complete selection locks and preflight precede bulk mutations. Fully labelled recordless owned containers can be deleted without adoption.
- Session list/show/reset/prune/delete are wired. Reset requires stopped/absent containers and idle leases, preserves declared history by default, and never clears auth/shared caches or stable bind roots. Dry runs do not reap leases or change files.
- Session deletion requires container absence, verifies image-tag association, and keeps external locks stable. Prune requires filters plus confirmation and rechecks age under lock.
- These additions pass unit/fake-backed checks; real-Docker acceptance for the new image/network/session cases remains outstanding.

## Configuration and environment — implemented

- User-approved sensitivity rule: env/auth values are sensitive; ordinary configuration names, paths, networks, argv, and Docker arguments are public fields, including their substitutions.
- `${env:NAME}` expands decoded strings from a captured host snapshot, not keys/raw JSON. Unset references fail; empty values are present; replacement text is not expanded recursively.
- Global passthrough and layer/CLI env are supported. Config env recovery uses verified source entries; CLI-only values remain unavailable for exact missing-container recovery. Session records never contain env values.
- Extra bind/volume mounts, published ports, protected raw Docker options, IDE metadata, and root/recreate creation flags are implemented. Host networking rejects published ports and managed targets/labels/env remain protected.
- Scoped global/profile/project config `--show` and JSON output use the shared resolver, expose origins/exclusions/references, and redact env. Interactive config dashboards remain pending.

## Runtime documentation and network files — implemented

- Embedded human docs, linked development notes, and `/devbox/AGENTS.md` are copied into verified running containers with root ownership and read-only access for the container user.
- `/devbox/network/env` and `/devbox/network/inspect.json` use fresh Docker facts, not persisted session authority. Preparation/access and managed network changes refresh them.
- Pi/OpenCode defaults include the generic `devbox` skill pointing to the runtime docs. Init remains non-overwriting.
- Unit tests cover bundle links, network contents/refresh, staging cleanup, failed-copy launch prevention, and updated seed counts. The real-Docker suite now checks runtime file readability/permissions, but has only been compiled here.

## Phase 5 — transfers implemented, broader acceptance pending

- Clone/relocate share one explicit state machine, sorted endpoint locks, and a single external journal reserving both names. Clone allocates a new ID and requires a stopped/absent source; relocate preserves ID and restores running intent.
- Copies declared environment stores and managed-config ownership manifests. Auth overlays, shared caches, leases, workspace files, and container-layer data are excluded. Opaque links are preserved without traversal; unsupported special files fail clearly.
- Destination creation resolves normal destination configuration. Same-folder `--from`/`--to` slots map exactly to profiles or `.project`; cross-folder clone may override the destination profile. Dry run does not create session state or journals.
- Preparation failures attempt bounded destination cleanup and source restart. Pending retries preserve the allocated identity and recopy the authoritative source with unchanged destination inputs. Committed retries use recorded destination recovery and finish source cleanup without recopying or consulting desired config.
- Pending state appears in inventory/show and blocks ordinary mutations, including forced deletion. External journals remain discoverable after source-directory deletion; completion removes the journal without permanent lineage.
- Fake-backed tests cover running/stopped rules, IDs/state, same-folder slots, dry run, ownership/lease rejection, portability policy, auth/cache exclusions, cancellation, incomplete preparation, retained prepared destinations, committed cleanup interruption, and recovery after source deletion/container loss.
- Real-Docker transfer acceptance and process-kill/power-loss testing remain unrun; fake-backed failures are not evidence of those gates passing.

## Validation

- `make check`: unit tests, race tests, and build pass.
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
- Phase 6: scoped config commands, dashboards, complete structured guidance, doctor, and complete documentation.
- Phase 7: release hardening, performance/secret audits, and remaining acceptance tests.
- Separate migration utility: not implemented.

Container env uses a private temporary env file, keeping sensitive values out of process arguments and avoiding container-env overrides of the host Docker client's environment. Multiline/NUL env values are rejected before Docker work.

Unsupported inputs are rejected instead of being silently ignored. Initial-creation failures can leave uncommitted host state when it cannot be safely classified; retries refuse to adopt it. This is not a production-ready cutover build.
