# Readable sessions and disposable runtime

Status: approved first-phase implementation scope. This replaces the image-backed snapshot proposal for the current phase. Implementation progress and validation are recorded in [progress.md](progress.md).

## Contract

**Saved session identity and harness state are durable. Docker containers and images are disposable.**

Missing runtime is rebuilt only by explicit Recreate, from the session's current selected config sources. Access reports missing containers with recreate guidance and never rebuilds them. It need not reproduce the previous environment. Preserve session ID, backing stores/history, workspace defaults and manual-start intent. Never create a saved session implicitly. Broken configuration or unavailable build/bind inputs remain actionable errors, not permission to invent defaults or empty lost stores.

Healthy-container config application stays as it is: existing creation settings require explicit recreation; compatible runtime settings/managed content use the existing application boundaries. No complete configuration/snapshot refactor is included.

## First-phase changes

1. **Readable names.** Restore sanitized, bounded workspace hints in container and session-directory names. Use readable `dbx` image tags and `dbx.*` ownership labels. Allocation suffixes prevent collisions; IDs/ownership and recorded instances authorize mutations, never readable names. Metadata-only rename does not rename existing resources. Keep `~/.devbox-neo` unchanged.
2. **Readable inspection.** Human status identifies workspace and local name together. Completion describes exact session-ID candidates with workspace/name where the shell supports descriptions; insertion still uses the exact ID. Retain full IDs in JSON and wide/detail views.
3. **Disposable runtime, explicit replacement.** Only explicit Recreate rebuilds an existing saved session. Missing-container access returns guidance rather than creating runtime. Recreate reuses a matching available owned image or builds from current config. A healthy container remains usable if its image has been pruned. Inspection does not build anything. Recreation preserves durable state and commits a new runtime association only on success.
4. **Remove historical reconstruction.** Remove historical setup-script and definition-source recovery, env expression/value restoration references and verification, and the missing-image explicit-recreate restriction. Resolve current public and sensitive values through the normal creation resolver. Never persist env/auth values. Current-container ownership/instance checks remain mandatory.
5. **Keep compact change detection.** Retain public applied settings and aggregate content fingerprints so status explains changes and whether recreation/rebuilding is needed. Compare build contexts and managed config by category, without persisting a file entry for every captured file. Exact added/removed/changed filename diffs are not included. Preserve current runtime-update classifications and env redaction. A missing image/container is a runtime observation, not corrupt session state.
6. **Exclude repository metadata.** Exclude `.git` at every depth from managed defaults/config trees, including worktree `.git` files and corresponding seeding/copy paths. Do not change Docker-context ignore handling or harness installation inputs. Do not exclude required dependency/runtime trees such as `node_modules` or `dist`. Keep managed reconciliation into directory-backed stores, not per-file mounts or direct live-source mounts.
7. **Preserve defaults and safety.** Keep workspace default records, explicit selection/clearing, atomic switches and matching-ID cleanup. Retain managed-file/JSON-key ownership, locks, leases, idle checks, deletion scope and transfer protections. Missing durable backing roots must not silently become empty history.

## Runtime creation and transfers

`app` remains the lifecycle owner. Explicit recreation resolves current selected sources under the session operation lock, validates durable roots, and calls the shared creation pipeline. Creation commits only after preparation/setup succeeds. Failure leaves session identity/history available for retry; Docker absence is not missing saved state.

Committed transfer retries must not recopy source stores or change endpoint identity. If destination runtime is missing, finish cleanup without rebuilding it or resolving config. The user can explicitly recreate the destination after cleanup releases the endpoints. This deliberately drops historical-config reconstruction, not destination authority. Preparation fingerprint checks and source/cleanup identity checks remain.

Image tags are display/cleanup associations, not recovery promises. Exact image IDs still verify an existing container. Image pruning must not require restoring an old tag or historical config. Do not retain obsolete compatibility paths solely to support old resources after cutover.

## Blocking migration gate

Detect required migrations at the shared CLI/menu initialization boundary before using the selected home. One generic **Migration required** screen offers **Exit** (default) or **Migrate**. Agreement runs the migration and then continues the original action; decline, cancellation or failure blocks use. There is no separate migration command or management UI. Help/version and read-only shell completion remain available; noninteractive/JSON state-backed commands return a structured block without prompting or changing state.

`internal/migration` owns detection, explanations and version-specific conversion. The UI only presents that explanation and calls the service after agreement. Current conversion supports the immediately preceding schema/namespace only; normal session readers do not become compatibility readers.

- Explain installation-wide scope, preserved state and container-local data loss before the user selects Migrate. Use the selected existing home; do not initialize it merely to ask.
- Acquire existing namespace/session locks; refuse active commands and pending transfers.
- Verify old Docker ownership and exact recorded associations before removing containers or old managed image tags. Do not broadly prune Docker or force-delete shared image layers.
- Convert saved comparison/namespace metadata to the new compact record format atomically, preserving session IDs, directories, sources, activity, history and defaults.
- Retain bind-backed session data, configs, auth and caches. Container-local files/tools are lost; communicate that consequence before application.
- Support bounded, inspectable retry after partial progress without adopting unrelated resources or writing a second durable transaction store.
- Runtime is rebuilt through explicit Recreate, not by access or the gate. The original requested command runs only after migration succeeds.

Do not approve or execute migration against user state as part of implementation or tests. Unsupported formats, corrupt records, ambiguous ownership and cleanup failures remain errors.

## Acceptance

- Folder hints are sanitized/bounded; same basenames and reused names remain collision-safe.
- Status/completion identify duplicate local names without changing exact targets or mutating state.
- After deleting container/image resources, ordinary access fails with recreate guidance and no mutation. Explicit Recreate uses changed current config, preserving session ID/history/defaults/manual-start intent. Existing containers do not rebuild just because their image is absent.
- Current env is resolved ephemerally; no historical secret restoration references or values enter records/output.
- Failed builds/materialization do not erase durable session state or fabricate successful applied state.
- Missing backing roots fail without creating empty replacements.
- Large managed/build trees do not produce per-file growth in `session.json`; category fingerprints still detect changes and preserve image caching/transfer retry checks.
- Managed `.git` metadata is excluded while extension/runtime content and installation/build-context behavior remain intact.
- Transfer retry preserves exact endpoints, committed destination authority and no-recopy-after-commit.
- Gate tests cover default Exit, agreement/continuation, cancellation/failure, noninteractive/JSON blocks, unchanged state before agreement and help/version availability. Cutover tests retain preflight, active-work/ownership, partial-retry, history/default and installation-scoping coverage. Normal readers reject the preceding format.

After implementation and review, run `make test-fast` (or its local-toolchain equivalent if `make` is unavailable), fix failures and rerun affected tests, then build. Live Docker and manual shell/terminal acceptance are separate gates.

## Deferred

No image-backed applied bundle, internal image-reader container, durable snapshot store, exact historical recovery, config-update lifecycle replacement or `host-setup.sh` is part of this phase. Revisit those only against an explicit user requirement, not as prerequisites for disposable runtime or useful status.
