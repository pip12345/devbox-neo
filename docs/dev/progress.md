# Implementation progress

The approved scope is [rewrite-plan.md](rewrite-plan.md), with the [environment-model changes](environment-model-plan.md) and the replacement [folder-local sessions and explicit configs](generic-config-alternative.md). The migration utility remains separate from runtime loading; adapting its destination model to named sessions and explicit configs is also approved.

Local config discovery and CLI/menu rename implemented; `make test-fast` passes. Live Docker and manual terminal acceptance remain unrun.

## CLI/TUI alignment and editing UX — implemented, host/live acceptance pending

Implemented the [alignment plan](cli-tui-alignment-plan.md): ordered config replacement and inspection, improved text editing, and consistent failure reporting. Selection keys wrap at list boundaries.

Validation: `make test-fast`, app/CLI/UI/importer race tests, both builds, and `git diff --check` pass. Live Docker and manual host-terminal acceptance remain unrun.

## Documentation reset — complete

- Rewrote the full repository agent guide as a short development contract and reading map. Reduced the installed agent guide to container boundaries, persistence, tools, managed files, network/SSH entry points, and on-demand docs links. Pi/OpenCode skills now point to that guide without repeating it; Claude keeps its one-line import.
- Reassessed every published page. Rewrote README, the docs index, all seven guides, and all five reference pages. Guides follow a progressive task sequence; reference keeps command/field lookup without duplicating walkthroughs. Contributor detail remains in architecture, with terminal ownership moved out of the overview into its own page.
- Corrected the obsolete `create --port` example, aligned navigation labels, and checked local file/heading links. Pinned Zensical site build succeeds in strict mode. `make test-fast` passes, including embedded documentation targets. Local file and heading links pass across all 21 published/contributor-entry pages.

## Session operations and deletion UX — implemented, host/live acceptance pending

- Checkpoint `dc90780` preserves the first object-based frontend. This follow-up removes category menus in favor of direct session operations, returns successful one-shot forms to their parent, and refreshes shared navigation after both successful and failed foreground work. Failed forms keep their inputs.
- Session rows and previews use relative Last active times; the default marker remains without a duplicate session detail. Narrow panes place activity beneath the name.
- Deletion names the target and displays scope/Force inline. Preview and execution use the same scope. The existing service now combines explicit scope and optional per-phase confirmation without releasing its operation-lock set; direct CLI flags and all deletion safeguards remain intact.
- Validation passes: `make test-fast`, race tests for app/cliui/cli/migrator, both builds, and `git diff --check`. Tests cover scoped confirmation/retention/preflight/locks, direct actions, nested snapshot refresh, relative activity layout, and PTY recreation success/partial failure. Rendered session-browser, direct-menu, and deletion-scope captures were inspected. Live Docker/harness/SSH and manual host-terminal acceptance remain unrun.

## Real Bubble Tea frontend — implemented, host/live acceptance pending

- Host review rejected the first generic action-list presentation. The revised browser separates folder/session/config objects from application actions, previews structured details, and opens object-specific menus with Enter. Navigation snapshots retain the left collection; stable keys preserve selection across refreshes. Top tabs own the Tab hint, shortcuts appear only beside their options, and the UI uses Devbox Neo branding without slogans or background rectangles.
- Session menus offer Make/Clear folder default directly, with no second picker. Folder menus retain creation and clearing unavailable defaults. The direct folder editor and scripting flags remain available.
- First creation starts from an empty-state Create action, prefills an editable folder, and shares nested config setup. Build failures retain the draft; success opens the stopped session menu without a default or launch. Direct commands/flags/JSON remain supported; non-terminal bare entry points show help without opening state.
- The shared runner retains synchronous domain workflows while one Bubble Tea UI goroutine owns terminal interaction. Foreground handoff releases the reader/renderer; results are acknowledged before returning. Operation-scoped SIGINT differs from whole-command termination. No recursive Cobra execution or duplicate lifecycle implementation.
- Forms cover session lifecycle, config/default editing, logs, networks, exec and launch argv, SSH sharing, copy/move with pinned retries, and exact/filtered deletion. Config deletion and status/error/completion presentation are shared with direct commands.
- Validation passes: `make test-fast`, race tests for cliui/cli/migrator, both builds, and `git diff --check`. PTY tests cover the empty browser → nested config creation → stopped session menu → Make/Clear default flow, foreground handoff, raw-termios restoration, and signal routing. Rendered terminal captures were inspected for browser, session/config menus, and creation layouts.
- Repeated race checks exposed reader shutdown races. The dependency now includes Ultraviolet's upstream StreamEvents join fix, and command cancellation requests graceful UI shutdown rather than Bubble Tea's force-exit path. A deterministic reader-join regression test and twelve repeated race runs of affected terminal workflows pass.
- Live Docker/harness/SSH acceptance and manual host-terminal acceptance of the redesign remain unrun. PTY fixtures and rendered captures do not establish those results.

## Config creation overview and stable default picker — implemented, manual acceptance pending

- Standalone and nested config creation share an editable destination/harness/files overview with an explicit Create config action. Bare `config create` opens it; supplied destinations prefill it. Automation still requires a destination and never prompts.
- Default selection reuses the folder overview's header, session layout, status styling, and default marker. Set/Clear remain folder-level actions; successful changes update the header/marker and exit receipt without shifting rows with a notice.
- Final `make test-fast` passes, including draft preservation, no pre-create writes, bare/named/path-based entry points, automation requirements, shared nested setup, and plain/styled default-picker row alignment and cancellation. Manual host-terminal acceptance remains unrun.

## Readable session listings — implemented, manual acceptance pending

- Compact global and folder lists show local session names; wide lists add `FULL NAME`. Exact identifiers remain in single-target status, JSON, and diagnostics, with identity and sorting unchanged.
- Final `make test-fast` passes, covering compact/wide global and folder views, duplicate local names, JSON/status identity, ordering, and escaped display values. Manual host-terminal acceptance remains unrun.

## Claude Code built-in — implemented, live acceptance pending

- Promoted the existing Claude definition and defaults into `internal/harness/builtin/claude/` unchanged. Registry discovery, config setup, completion, runtime storage, and recorded recovery use the existing generic mechanisms; harness selection still starts unset and user overrides retain precedence.
- The supplied launch bypasses Claude permission prompts and continuation uses `--continue`. Its environment store is `.claude`; `.credentials.json` and `.claude.json` use separate shared auth mounts. Bundled settings own `tui`/`pluginConfigs`, and `CLAUDE.md` imports runtime guidance.
- Final `make test-fast` passes, including definition/defaults, completion, interactive setup, artifact generation, and fake-backed lifecycle/storage/recovery coverage. Native installer execution, live authentication, Claude's fullscreen/plugin behavior, and live conversation continuation/copy remain unverified; fake-backed tests do not establish those upstream behaviors.

## Shared synchronous CLI screens — implemented, manual acceptance pending

- `internal/cliui` owns the command-scoped reader, terminal lifecycle, screen dispatch, selection/toggle markers, text validation, and confirmations. Main CLI and migrator menus use handler-bound actions; workflows retain ordinary local state and service-owned mutations.
- Session creation always offers Create session and explains missing inputs. Create config reuses standalone setup and adds the result; empty pickers offer the same workflow. Cancellation preserves the parent draft, created configs persist independently, reorder needs two configs, and combined configuration is a separate Back-only view.
- Final `make test-fast` passes, including shared-runner, workflow, pseudo-terminal, and migrator tests. Live Docker and manual host-terminal acceptance remain unrun; automated tests do not establish those results.

## Workspace-only session-name hash — clean development-state cutover

- The 12-hex portion of a full session/container name now hashes only the canonical workspace path. Case-sensitive local names remain distinct in the suffix; sessions in one workspace display the same hash. Identity validation, lookup, creation, transfer targets, and Docker ownership checks use the shared naming function.
- Existing rewrite sessions with names made by the previous rule are not renamed or adopted. Users must remove old development state themselves before using the new names; no reset or migration code was added. Focused naming/app/migration tests and `make test-fast` pass; live Docker acceptance remains unrun.

## CLI status and config-selection UX — live Docker acceptance pending

- Bare `status` now performs the existing bulk check; the redundant `--all` flag is removed. Config edits that save changes point to one bare `status` command. Single-target status gives a short container-restart note for pending managed files and a recreation hint for image/container changes.
- Both list views show the saved automatic/keep-running lifetime apart from live container state. Create/edit menus consistently call the selected directories `configs` in headings, actions, save receipts, and repair hints. The picker displays named references in a fixed-path table and keeps typed-path selection immediate, without a confirmation screen; selected config lists and transfer results display only the ordered label and resolved path. Help and user/embedded docs explain `start` as a keep-running choice.
- Fake-backed CLI tests cover saved/unchanged editor receipts, bulk status and JSON, managed-file/recreation guidance, lifecycle lifetime display, and reference selection. `make test-fast`, integration-tag compilation, and `git diff --check` pass. Live Docker behavior is still untested.

## OpenCode 2 built-in — live Docker acceptance pending

- The built-in OpenCode harness now installs from the v2 endpoint. Its launch config uses a v2 provider policy to keep the `opencode` provider disabled; sharing remains disabled. The v2.0.6 CLI was checked for the `opencode` executable, `-c` continuation flag, and environment config loading. The v2.0.6 path diagnostic reports the existing config, data, and cache targets and puts its database under the data mount; the v2 auth source still uses `auth.json` there.
- Existing sessions require explicit recreation for the changed image input; no automatic OpenCode state or config migration was added. `make test-fast` and integration-tag compilation pass. `make test-integration` fails because this environment has no Docker CLI/daemon; live installation, authentication, and conversation continuation remain untested.

## Folder-local sessions and explicit configs — implemented, live acceptance pending

- First checkpoint adds explicit config-reference capture/expansion and folder-default persistence. Relative arguments resolve against the invoking directory and remain workspace-relative; fixed references retain their absolute target. Named references use the selected Devbox home. Runtime resolution rejects duplicate canonical directories, including symlink aliases, while saved-reference validation does not require accessible sources.
- Folder defaults use a path-keyed state record and stable external workspace lock. Selection pins full name plus durable ID; matching-default cleanup works from a saved identity even after the source record or workspace disappears. Absent reads/clears do not seed a default record, and malformed state fails without replacement.
- The config-directory service now creates a new `config.json` without replacement, accepts preexisting directories without overwriting their artifacts, rejects repeat creation before setup, and adds optional files to existing configs. Harness-file generation is separate from the persistent harness setting and accepts a different or unset selection. Artifact planning/publication is shared by creation and editing rather than duplicated. Tests cover concurrent creators, validation before mutation, repeated artifact preservation, independent harness trees, inherited skills, and retained source expressions.
- The cutover now connects these owners to creation, lifecycle lookup, defaults, source-chain editing, transfer, and the CLI. Session schema 5 records case-sensitive local names and relative/fixed desired references. Applied input snapshots separately retain the committed absolute source directories used to authorize environment recovery. Profile/project discovery, global settings, inheritance cutoffs, and their commands/flags have been removed without runtime compatibility readers.
- `config create`, `config edit`, `create`, and `edit` have separate entry points; `edit` also manages folder defaults. Config setup supports explicit flags/JSON; menus retain canonical line input and immediate setting/source edits. Creation leaves containers stopped and never selects a default. Read-only completion uses the selected home and folder-scoped local names without seeding state.
- Transfer journal schema 2 pins named endpoints. `copy --as` and `copy --move --as` support same-folder and cross-folder transfers. Whole-session deletion and committed move cleanup clear only a matching default under the external workspace lock; container deletion does not. Tests cover stale name reuse, concurrent default selection/deletion, incomplete source repair, source-edit conflicts, and preserved committed recovery inputs.
- The standalone importer now publishes old profiles under `configs/`, converts global settings into an ordinary `imported-global` config, and records explicit source references and initial named identities. Its own journal is schema 2; old staged runs require fresh preparation, not a compatibility reader. Existing source-format conversion remains confined to migration files. Imported sessions do not select defaults.
- Final review also retained the durable source ID across transfer folder/default lookup and setup, so a replacement using the same name cannot become an already-selected transfer's source.
- Guides, references, architecture, CLI/importer wording, completion, embedded agent guidance, and contributor instructions now use the named-session model. Earlier design documents are explicitly marked as superseded where they describe the old selection model.
- Validation passed: `make check` (format, full unit suite, full race suite, CLI build), `make build-migrate`, `go vet ./...`, integration-tag compilation, and `git diff --check`. Pseudo-terminal tests cover canonical input and styling. Docker is unavailable here, so the Docker-based documentation-site build, live Docker lifecycle/importer tests, and manual host acceptance remain unrun. Compilation and fake-backed tests are not live acceptance.

## Behavior-preserving cleanup — implemented, live Docker acceptance pending

- Split `app/engine.go` into same-package opening, creation, mount-planning, recovery, and attachment owners. Moved function bodies remain unchanged except the planned record-construction and diagnostic-delivery extractions; lifecycle sequencing, locks, cleanup, and Docker calls remain intact.
- Extracted side-effect-free creation-record assembly with explicit clock inputs. Added pre-extraction parity coverage for ordinary creation, recreation, prepared destinations, and saved JSON, retaining failed-commit and recovery tests.
- Typed diagnostics now use synchronous `Engine.OnDiagnostic` delivery and CLI-owned rendering. Resolution warnings and child streams remain unchanged. Tests retain immediate-delivery/cancellation coverage and verify exact text, stream routing, ordering, nil callbacks, and root CLI wiring. Migration callers remain intentionally silent; Docker integration tests explicitly log structured diagnostics.
- Validation passed: `make test`, `make test-race`, `go vet ./...`, `make build build-migrate`, integration-tag compilation with `go test -tags integration -run '^$' ./...`, and `git diff --check`. The Docker CLI is unavailable here; live Docker lifecycle, recovery, and transfer acceptance remain unrun. Compilation is not live acceptance.

## Config sources and image/hook chains — implemented, live Docker acceptance pending

- Profiles/projects remain the public interface over shared source composition. Configs have no `name` field. Generic `inherit: false` replaces `inherit_profile` and cuts preceding sources before reading their settings/artifacts, including explicitly selected profiles.
- Project configuration is fixed at `<workspace>/.devbox/`. The optional external-directory feature and saved-binding lookup/recreation machinery were removed from scope. Transfers use the destination workspace's project source; profile/project composition and image/hook chains remain.
- Session schema 4 records ordered source references, image stages, and setup/before-open inputs. Development state requires a clean reset; no runtime migration, old-format reader, or alias was added.
- `base_image` feeds a prepared development-user/runtime image. Profile/project Dockerfiles then chain with separate contexts, image-ancestry checks, and restored user/home/workdir/shell boundaries. Custom PATH survives harness finalization. All intermediate tags have ownership-checked cleanup; forced rebuild disables cache across stages.
- `setup.sh` and `before-open.sh` run in source order, stop on failure, and keep workspace effects explicit. Config menus, seeding, inspection, completion, runtime guidance, and human docs use the new contracts. The separate import utility was adjusted to emit/use the current APIs; it does not migrate existing rewrite state or rewrite arbitrary old Dockerfiles.
- Initial validation passed: `make check` (format, full unit suite, race suite, CLI build), `go vet ./...`, integration-tagged app test compilation, `make build-migrate`, and `git diff --check`. Fake-backed tests cover generic composition, cutoffs, project-source access/editing/recovery, profile-only separation, script order/failure, image contexts/boundaries, ancestry rejection, and destination project sources. External-directory removal validation passed: `make test`, focused race tests for source selection, recreation, transfers, resource editing, and CLI rejection, plus `git diff --check`. No compatibility reader or migration was added.
- Docker is unavailable in this container. Real Debian/Ubuntu builds, cache reuse, UID conflict behavior, user-tool installation, and live transfer/recovery remain unrun acceptance gates. Fake builds do not establish those results.

## Environment model — implemented, live reboot acceptance pending

- Profile plus project is a distinct identity (`.profile-NAME.project`). Explicit profiles retain project overrides; `--ignore-project` excludes them. Standalone projects discard preceding profiles through `inherit: false`. All folder-targeted session commands select one exact combination or fail, without scanning other sessions. Exact names and recreation pin recorded participation. Slot selectors, filtering, completion, ownership validation, and transfer retries support compound identities.
- Removed container-setting flags from create/recreate and removed workspace read-only mode. Config fields are `mounts`, `ports`, `env`, `shell`, and `ignore_project`, without runtime aliases. Sparse configuration and existing menus remain authoritative. Configured `harness_args` requires a local harness; mismatching harness layers contribute no arguments. One-off open arguments are not saved.
- Removed `on_exit` from config, CLI, records, and leases. Manual start records keep-running intent until stop and uses Docker's `unless-stopped` policy; automatic sessions use `no` and stop after their last attachment. Attachments do not change intent. Recreation/recovery and relocation preserve manual intent; clones start automatic. Raw Docker restart options are rejected. Status exposes the lifetime choice.
- Session schema is now 4 and lease schema is 2; existing development sessions require a clean reset with the previous build. No compatibility readers, aliases, automatic adoption, or live state deletion were added. The separately approved importer emits current fields/identities and reports removed settings while retaining its explicit review process.
- Regression coverage includes exact missing-target failures, unrelated corrupt records, pinned recreation after defaults change, harness filtering, source-field validation, concurrent manual start and final-attachment cleanup, policy update/stop failures, and a fake-daemon reboot model. Validation: `make check` (format, full unit and race suites, CLI build), `make build-migrate`, `go vet ./...`, integration-tagged test compilation, and `git diff --check` pass.
- Live Docker daemon/host reboot, real container recreation/recovery, provider continuation, and live importer acceptance remain unrun. The fake reboot test verifies policy mapping, not real daemon startup, host mount availability, or Docker's restart-manager timing. Docs container build is also unrun here.

Earlier milestones below describe their delivery checkpoints; the current environment model above supersedes their old selection, schema, and shutdown-policy descriptions.

## Migration — explicit merge candidate, live acceptance pending

- Delivered the standalone inventory/staging checkpoint, then added explicit `--merge` review and execution. `make build-migrate` builds the separate utility; the normal runtime does not import the migration package or read its journal.
- Running the bare migrator in a terminal now opens a compact preview/prepare/import/resume/exit menu. The landing screen shows only source/destination paths, actions, and short inline availability labels. Preview uses the same read-only path as `--dry-run`, including when staging already exists or cannot be read. Action details and warnings stay inside the selected flow. All migrator menus use the rewrite's indented bracketed numbering, `[0]` for back/cancel/exit, and `Choose a number >` prompt. Copy/import still enter separate review and approval flows; continuation only resumes approved work. Scripts retain explicit phase flags, and non-terminal bare invocation prints help. Fixture-backed menu tests cover compact output, preview equivalence and no mutations, cancellation/EOF, separate approvals, staging and merge continuation, completed/unknown work state, and custom endpoints; manual terminal acceptance remains unpassed.
- Terminal inventory reports now use grouped compact rows, status counts, and full review/conversion notes under each affected owner. Session/profile blocks have blank-line separation and bold headings, with colored status labels on terminal output. Styling follows output-terminal detection and respects `NO_COLOR`/`TERM=dumb`; saved and redirected reports remain plain. Pseudo-terminal tests verify styling, spacing, content parity, and control-character escaping. Repeated notes have report-wide numbers, sorted ascending on each owner, with the same numbered summary retained below. All errors, warnings, and exclusion reasons remain visible. `--verbose` and saved `report.txt` retain full inventory detail; this is a presentation change, not a change to selection, approvals, or validation. Tests cover note scope, output reduction, detail retention, lifecycle statuses, control-character escaping, and read-only verbose preview.
- Initial discovery now reads owner/config/session metadata without recursively walking or hashing payloads. Staging rechecks that metadata under source locks, then snapshots only approved data after stopped-writer checks. Default-excluded caches and explicitly skipped payloads remain unscanned through resume. Sizes and deep-tree validation are deferred; copy/source verification retains content, mode, link, and directory-membership checks. Throttled progress covers snapshotting, copying, and source verification. Linux access-event tests verify that discovery and excluded scopes do not open payload files/directories; snapshot-mutation tests retain fail-closed recovery.
- Inventory diagnostics now separate version mismatches, missing/null creation settings, JSON field/type/syntax errors, and workspace failure categories. Generated-link failures include the target and attempted host mapping; unrecognized layouts identify entry kinds and root symlinks. `[Metadata OK]` and `Planned staging path` explicitly distinguish read-only discovery from copied/validated data. Tests verify value redaction in errors, reports, and journals. Compatibility is defined by the source-format table in the migration plan, not by individual installation reports.
- Source conversion now covers global v1/v2 (including omitted versions and absent config), sparse layers, flat proxy fields and typed `auto_rebuild`, metadata v3/v4 with recorded creation settings, and pre-label ownership. Missing session records get stable proposed import IDs without source initialization or invented activity. Both old/current lease directories and excluded pre-label writers are checked. Source labels override metadata; foreign/partial labels never fall back to legacy ownership.
- Generated links use the recorded staging layout, not filename semantics. Present targets are copied; mirrored stale links inside an existing stage are recorded as merge decisions. A missing entire stage, ambiguous mapping, unsafe link, or special file still blocks. Changes to omitted-target presence invalidate the snapshot. Removed config settings also require owner-level conversion approval. Mixed-format Pi/OpenCode tests cover existing destination preservation, legacy config capture, interrupted/resumed imports, and redaction.
- Inventory maps Pi/OpenCode host-backed state/config/auth, preserves report-only aliases/lineage, and supports explicit dependency-aware skips. Staging never modifies the destination or project files. Merge can capture OpenCode config from a verified stopped source container; special/escaping archive entries fail closed, and unavailable config requires explicit omission or skip.
- Merge previews use the normal resolver with proposed project config and private profile trees. Existing profile conflicts require rename/reuse/skip; global settings and auth are retained by default. Approved replacements retain backups, and additions use no-replace publication. Session slots, IDs, image associations, source snapshots, destination facts, and input fingerprints are checked before use.
- `app.CreatePrepared` accepts a normal specification, an already-held operation lock, and typed identity/activity inputs. Transfers and migration share that materialization path. The migration journal owns publication intent and Linux device/inode bindings for prepared directories. A committed normal record is authoritative: retry never recopies its history or recreates it from old inputs.
- `--merge --review-pending` can reapprove changed final configuration for unfinished environments after shared publication completes, without changing scope or resetting completed sessions. Reports identify imported/pending/skipped items, accepted changes, backups, and exact next commands.
- Import blocks raw Docker `--env=...` arguments because the current ordinary engine serializes raw arguments. Users must place those values in source `extra_env` (converted to destination `env`); fixing ordinary raw-argument persistence is separate work. Custom harness definitions must retain the tested binary/env/store/auth/config mapping.
- Tests use temporary homes and fake Docker, including interrupted publication, post-record-commit retry, project backups, source/destination conflict checks, raw-value redaction, prepared-directory identity mismatch, archive traversal rejection, and pending-input reapproval. No personal installation or Docker resources are test targets.
- Nested source layouts now use one read-only mapping for flat/canonical stores and generated config, including exact retained aliases and partially moved directories. Both discovery/snapshots and OpenCode capture use it. Nested attached leases are checked even for excluded sessions. Unknown internal entries, two real copies, foreign aliases, missing targets, and alias changes after snapshot remain blockers. Tests cover payload-free discovery, copy-once behavior, capture, and interrupted/resumed mixed-layout imports into existing Neo state. No staging-reset feature was added.
- Retained Claude/Codex/Copilot stores now emit explicit nonblocking warnings for Pi/OpenCode sessions in both layouts. They remain untouched and unscanned in the source. Warnings are shown before staging approval, during merge review, and in saved reports/journals. Unsupported recorded harnesses, unknown entries, unsafe paths, and ambiguous layouts still block. Tests cover all three retained harnesses in both layouts, no payload reads, preserved supported history, report redaction, and warning visibility before confirmation.
- Compatibility-pass validation: rewrite `make test`, migration/CLI race tests, vet, `make build-migrate`, and `git diff --check` pass.
- Real-Docker migration, provider auth/conversation continuation, process-kill/power-loss acceptance, and manual terminal UX remain unpassed. Fake-backed tests do not establish readiness for a real cutover.

## Foreground SSH sharing — implemented, live-Docker acceptance pending

- Added `ssh <target> <destination>` with a container-side master by default and invocation-only `--host-master`. The user authenticates in a foreground host terminal; generated in-container SSH config reuses the connection without fallback login. Host mode prints the agreed host/network-access warning before authentication. Normal SSH configuration, including ProxyJump, agent and X11 forwarding, is honored; no keys/config are copied and there is no identity/detach flag.
- Shared startup and attached-command lease ownership cover both modes. A per-invocation flock supervisor terminates the actual master after controller loss, including during authentication; a lifetime lock makes cleanup wait before releasing the environment lease. Host masters also end on forced container stop/removal or unavailable Docker. Runtime sockets/config live in a private `/devbox/ssh` mount, are excluded from transfers, and are excluded from runtime asset permission changes. Existing environments need explicit recreation to acquire the mount.
- Added unit/process tests for validation, duplicate/concurrent connections, unavailable-socket failure, actual master teardown, long home paths, startup/lease/on-exit behavior, host authentication failure, forced stop, and CLI warning/terminal behavior. Added an isolated real-Docker test for both modes through ProxyJump with generated fixture credentials and strict known-host verification. The Docker CLI is unavailable in the assistant container: the new Docker test has compiled, not run. The user subsequently demonstrated successful container-mode password authentication. MFA, graphical forwarding, and the corrected terminal/disconnect UX still need manual acceptance.
- The user's host run exposed staircase SSH status output, Ctrl-C reported as Docker exit 130, and escaped multiline Cobra suggestions. SSH now uses raw-terminal-aware status output, restores terminal settings, and treats expected foreground interruption as a normal disconnect without hiding cleanup failures. Unknown group commands use structured suggestion steps. Added pseudo-terminal, cancellation/cleanup, and command-validation regressions; host rerun of these fixes remains pending.
- Validation: rewrite `make check` (format, unit tests, race tests, build), parent `make test`, `go vet ./...`, and integration-tag compilation pass. The new live-Docker and manual SSH checks above remain unpassed here.
- The user reported `make test` and `make test-integration` passing on the host for the preceding environment/startup/cleanup commit `21e86ac`. That is not a live-Docker acceptance claim for the new SSH implementation; earlier checkpoint notes below are historical.

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
- Explicit `create` prepares a new environment and leaves it stopped without launching its harness. `open`/`start` require an existing session and never create a new one. Container-setting flags are limited to `create`/`recreate`; `open` retains launch settings, continuation, and harness arguments. Both retain recorded missing-container recovery. Unit/fake-backed tests cover missing-session rejection, creation/reopen, concurrency, refusal of existing/invalid state, recovery, and final-stop failure. `make test` and focused app/CLI/resource race tests pass. The real-Docker lifecycle now uses standalone create followed by plain open; it compiles with integration tags, but this updated path has not been executed here.
- Non-blocking creation drift, stopped-only config synchronization, running-container deferral, and explicit recreation with durable state preserved.
- No-cache forced rebuilds; explicit recreation builds a missing image, whereas recorded recovery refuses it.
- Per-container setup and every-open entrypoint scripts. Setup inputs belong to creation fingerprints because synchronization cannot claim a changed setup script already ran.
- Config-independent running-container start/shell/exec and recorded recovery for the currently supported inputs. The startup-consolidation follow-up now resolves and synchronizes valid runtime config for stopped-container access. Definition-sourced env values are not stored in session JSON. Terminal display variables are forwarded at creation/recovery and refreshed for attached commands without entering session records or fingerprints; unit/fake-backed coverage passes, while the added live-Docker forwarding check and manual Bash prompt rerun remain pending.
- Failure propagation, failed-commit cleanup, cancellation cleanup, concurrent-lease safety, and preserved foreground exit/signal status.
- A custom fixture already exercises the same generic engine; this does not complete the full Phase 2 schema/acceptance gate.

The user reported the original Pi real-Docker lifecycle test passing on their Linux host. That test exercised non-interactive version launch and state preservation, not provider login or conversation continuation. Those manual checks remain unverified. The user explicitly approved continuing implementation rather than blocking on them.

## Phase 2 — candidate implemented, expanded Docker acceptance pending

- Added the OpenCode definition, separate config/data stores, shared cache, managed auth, and continuation/transfer declarations.
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
- List/status/logs/delete, bulk recreation, and network inspect/env/connect/disconnect are wired into the CLI. Inventory batches Docker inspection; status reports broken desired config separately from live state. The later top-level environment consolidation makes list/status session-based and retains optional exact activity/creation times and last action. Stopped/missing rows are dimmed after alignment while running rows and separate diagnostics stay undimmed; terminal capability/opt-out checks preserve plain output.
- Exact `open` targets keep their recorded slots, and explicit-profile access avoids unrelated corrupt session records.
- Container deletion preserves durable state and image tags; complete selection locks and preflight precede bulk mutations. Fully labelled recordless owned containers can be deleted without adoption.
- Saved-environment list/status and deletion are wired. Single-target status combines the saved contract and live leases with container/configuration checks; the separate show command is removed. Config errors retain saved details, and exact pending-transfer endpoints remain inspectable without records. Bulk status stays compact. The startup/cleanup consolidation removes reset and folds prune's filters into delete. Dry runs do not reap leases or change files.
- Saved-state deletion requires container absence, verifies image-tag association, and keeps external locks stable. Filtered delete requires explicit scope outside interactive mode and rechecks age under lock.
- These additions pass unit/fake-backed checks; real-Docker acceptance for the new image/network/session cases remains outstanding.

## Configuration and environment — implemented

- User-approved sensitivity rule: env/auth values are sensitive; ordinary configuration names, paths, networks, argv, and Docker arguments are public fields, including their substitutions.
- `${env:NAME}` expands decoded strings from a captured host snapshot, not keys/raw JSON. Unset references fail; empty values are present; replacement text is not expanded recursively.
- Global passthrough and layer/CLI env are supported. Config env recovery uses verified source entries; CLI-only values remain unavailable for exact missing-container recovery. Session records never contain env values.
- Extra bind/volume mounts, published ports, protected raw Docker options, IDE metadata, and root/recreate creation flags are implemented. Host networking rejects published ports and managed targets/labels/env remain protected.
- Scoped global/profile/project config `--show` and JSON output use the shared resolver, expose origins/exclusions/references, and redact env. Numbered settings menus support scalar/list edits and reset-to-inherited without new dependencies. Each valid operation saves immediately; Back only navigates, with no draft or confirmation stage. Source edits use owner locks and same-field conflict checks, retain expressions and unrelated concurrent edits, and never save redacted display values. Resolution errors remain visible while local editing stays available. Config submenus, profile/project init, and profile selection share bold headings, aligned choices, wrapped text, and dim secondary instructions without changing input rules. Automated coverage includes Linux pseudo-terminal config/init commands and styled/plain list/menu checks; manual terminal usability acceptance remains pending.

## Runtime documentation and network files — implemented

- Optional browser docs use direct Zensical Docker Make targets. The actual Docker build/serve smoke check remains unrun here; Docker is unavailable.

- Embedded human docs, linked development notes, and `/devbox/AGENTS.md` are copied into verified running containers with root ownership and read-only access for the container user.
- `/devbox/network/env` and `/devbox/network/inspect.json` use fresh Docker facts, not persisted session authority. Preparation/access and managed network changes refresh them.
- Pi/OpenCode defaults include the generic `devbox` skill pointing to the runtime docs. Init leaves it inherited rather than copying it into profile/project config; explicit user overrides still take precedence. Init remains non-overwriting.
- Unit tests cover bundle links, network contents/refresh, staging cleanup, failed-copy launch prevention, and updated seed counts. The real-Docker suite now checks runtime file readability/permissions, but has only been compiled here.

## Phase 5 — transfers implemented, broader acceptance pending

- `copy` and `copy --move` share one explicit state machine, sorted endpoint locks, and a single external journal reserving both names. Copy allocates a new ID and requires a stopped/absent source; move preserves ID and restores running intent. The old command names are removed without aliases; internal journal/JSON modes and harness capability fields remain `clone`/`relocate`.
- Copies declared environment stores and managed-config ownership manifests. Auth overlays, shared caches, leases, workspace files, and container-layer data are excluded. Opaque links are preserved without traversal; unsupported special files fail clearly.
- Destination creation resolves normal destination configuration. Same-folder `--from`/`--to` slots map exactly to profiles or `.project`; cross-folder copy without `--move` may override the destination profile. Dry run does not create session state or journals.
- Preparation failures attempt bounded destination cleanup and source restart. Pending retries preserve the allocated identity and recopy the authoritative source with unchanged destination inputs. Committed retries use recorded destination recovery and finish source cleanup without recopying or consulting desired config.
- Pending state appears in inventory/status and blocks ordinary mutations, including forced deletion. External journals remain discoverable after source-directory deletion; completion removes the journal without permanent lineage.
- Fake-backed tests cover running/stopped rules, IDs/state, same-folder slots, dry run, ownership/lease rejection, portability policy, auth/cache exclusions, cancellation, incomplete preparation, retained prepared destinations, committed cleanup interruption, and recovery after source deletion/container loss.
- Real-Docker transfer acceptance and process-kill/power-loss testing remain unrun; fake-backed failures are not evidence of those gates passing.

## Mount-parent permissions — regression fix awaiting Docker rerun

- The user's expanded Linux Docker run failed in OpenCode 1.18.29: `EACCES` creating `/home/devuser/.local/state` after a successful image build. The generated image had not prepared `.local/share`, the parent of its data mount.
- Generic mount-parent planning now separates image-owned ancestors from ancestors inside other managed mounts. The runtime layer creates/checks image parents as `devuser`; recorded create/start prepares nested parents in host store/auth/cache sources. No recursive chown, new mounts, or persistence mapping changes were added.
- Unit tests cover parent ownership classification, literal path arguments, both image build modes, nested auth/cache parents, restoration of missing nested parents before startup, and rejection of missing roots/symlinks. The Docker gate probes sibling-directory creation for all three harnesses before and after recreation/start; the custom fixture now uses nested state/auth targets.
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

- `status --all [--profile NAME] [--json]` shares inventory ownership/instance checks and single-target desired comparison. The later environment consolidation includes missing-container sessions and reports unmatched containers separately. Ordinary listing remains free of desired resolution.
- Container/image drift recommends ordinary recreation; runtime changes do not imply rebuilding. `environment.Inputs` now supplies both fingerprints and detailed reasons shared by open and single/bulk status, including safe setting values, changed files/permissions, and env variable names without values. Upstream-version discovery and general doctor checks remain out of scope.
- Session schema 2 requires a complete applied-input snapshot and validates it against committed fingerprints. Creation/recreation commits all inputs; runtime application advances its snapshot and hash together, including recovery. Transfers commit destination inputs. Older development records require a clean reset; no compatibility, migration, or guessed baselines were added.
- Open reports creation drift before resolution warnings, recovery, synchronization, startup, and entrypoint/harness output, then continues immediately without an artificial delay.
- Fake-backed tests cover drift classifications, broken records/config, pending transfers, profile selection, Docker inventory failure, JSON/table output, warning ordering, immediate continuation, cancellation, detailed reason coverage/redaction, strict record validation, baseline commit/failure/defer behavior, and transfer destination baselines. Rewrite `make test` and `make check`, plus parent `make test`, pass. Live-Docker acceptance has not been run because this workspace has no Docker CLI.

## Shared command errors and Pi fullscreen — implemented

- Resource and lifecycle failures share `commanderror.Error`/`Step`; the process boundary prints errors once. Existing JSON commands emit structured failures on stdout, while streaming commands keep human stderr and unchanged child output. Codes/targets/next steps come from owners, not message matching; causes and foreground exit statuses are retained. Unknown errors remain `command_failed` rather than guessed categories.
- Human errors now use short `Error:` messages and separate target context, without internal code/operation banners. Existing step reasons label copyable commands, with `Then`/`Or` distinguishing sequences and alternatives; generic creation guidance shows only `create`. Missing-environment and project-owner hints preserve entered folder paths while filesystem identity remains canonical. These hints omit profile flags and use normal configuration selection; profile-default setup is labeled optional. Resource success guidance shares the labeled renderer. JSON fields, error codes, private causes, and exit statuses remain intact. Exact-output, action-label escaping, and existing JSON/privacy tests pass, along with `make test`, affected-package race tests, vet, build, and integration-test compilation. A temporary-home CLI smoke check confirms the missing-environment output and nonzero exit; live Docker remains unrun.
- Added actionable handling for configuration/harness selection, missing/ambiguous targets, invalid records, missing containers, active leases, ownership/instance failures, managed-file conflicts, recovery, pending transfers, and Docker failures. Retry argv uses journal endpoints and retains explicit home selection. No automatic repair or force-next-step behavior was added.
- Pi defaults to `--tui-mode fullscreen` through its built-in launch definition; profile/project harness arguments or one-off `--tui-mode regular` override it. Existing recorded environments require recreation to adopt the definition. Fullscreen is experimental upstream; live Pi acceptance remains unrun here.
- Unit tests cover JSON/human routing, private causes, next-step scoping, cleanup diagnostics, exit codes, absence/corruption boundaries, transfer retry commands, and Pi launch/override ordering. Rewrite `make test`/`make check`, vet, integration-test compilation, and parent `make test` pass. Live Docker and fullscreen UI checks remain unrun here.
- The executable remains `devbox-neo`. The obsolete rename task and deferred doctor scope were removed.

## Top-level environments and explicit deletion — implemented

- Moved saved-environment commands to the root and removed the `session` group. The later consolidation removes reset and folds prune into delete. List and status now describe saved environments, including missing containers. There are no command aliases or container-only list/status views.
- List and bulk status warn separately about installation-owned containers with no session record. Corrupt records remain session diagnostics. JSON has `sessions` and `unmatched_containers` arrays; single status remains one object. List keeps activity sorting and optional wide details; status reuses existing drift checks without a doctor or upstream-version discovery.
- Delete uses one complete operation-lock set across container removal and optional saved-data deletion. Interactive prompts default to no. The later cleanup consolidation replaces the initial include/yes flags with explicit `--container`/`--session` scopes; `--force` only permits interrupting attached container commands. Saved-state deletion still requires idle sessions. Explicit saved-state scope is preflighted before container removal and container absence is checked afterward. Dry runs never prompt or delete.
- Completion, actionable next steps, help, runtime guidance, and guide/reference/architecture docs use the top-level commands. No saved-record or config schema migration is involved.
- Rewrite `make check` (unit tests, race tests, build), parent `make test`, vet, and integration-test compilation pass. Fake-backed regression tests cover missing-container drift/config errors, unmatched versus corrupt records, profile selection, explicit/default deletion scope, full-set preflight, cancellation, prompt locking, image-tag reassociation, and container reappearance. CLI tests exercise the two-stage and combined confirmations through Linux pseudo-terminals, top-level completion, JSON diagnostics, and removal of the old command group. No live-Docker execution of the changed deletion flow was performed here.

## Authoritative startup configuration and consolidated cleanup — implemented

- Ordinary managed files now follow source bytes/modes even after local edits; obsolete managed files are removed. Unmanaged files/history remain untouched. Pi's declared-key JSON merge is unchanged; malformed live JSON remains a blocking conflict.
- Shared `startAccess` preparation synchronizes before stopped-container open/start/shell/exec, including compatible current runtime config during ordinary recorded recovery. Running attachments do not sync. Invalid participating config blocks startup; creation inputs and incompatible harness layouts still require recreation. Transaction rollback and committed-transfer recovery retain their recorded contracts.
- Removed reset and its `reset_preserve` harness field. Custom definitions must remove that field; existing environments require recreation to adopt changed built-ins. No automatic migration or schema fallback was added.
- Delete has mutually exclusive, non-prompting `--container` and `--session` scopes. Unscoped interactive calls retain two choices; scripts/JSON/dry runs require scope. Removed `--yes`, `--include-session`, and prune. Delete owns intersecting all/stopped/orphaned/age filters, rechecked under lock before its own activity update. Force remains separate from saved-state idle safety.
- Rewrite `make check` (unit tests, race tests, build), parent `make test`, vet, and integration-test compilation pass. Tests cover authoritative ordinary-file content/mode/removal, Pi-key preservation, each access command's stopped/running boundary, invalid startup config/shared JSON, runtime baseline commits, intersecting deletion filters, stale activity/orphan rechecks, explicit scope, and removed commands/flags. Live-Docker execution of this follow-up was not performed here.

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
- Phase 5: real-Docker transfer/kill-point acceptance and additional acceptance coverage for filtered deletion.
- Phase 6: remaining guided artifact workflow audit, menu usability acceptance, and documentation coverage; shared error/next-step handling is implemented and a doctor command is not planned.
- Phase 7: release hardening, performance/secret audits, and remaining acceptance tests.
- Separate migration utility: development candidate implemented; real-Docker, provider auth/continuation, and process-kill/power-loss acceptance remain pending.

Container creation env uses a private temporary env file, keeping configured sensitive values out of process arguments and avoiding container-env overrides of the host Docker client's environment. Attached commands forward only the captured terminal display allowlist through Docker exec env arguments. Multiline/NUL env values are rejected before Docker work.

Unsupported inputs are rejected instead of being silently ignored. Initial-creation failures can leave uncommitted host state when it cannot be safely classified; retries refuse to adopt it. This is not a production-ready cutover build.
