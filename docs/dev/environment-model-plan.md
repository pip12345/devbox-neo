# Environment identity and configuration simplification

Status: implemented; final validation and review tracked in `progress.md`. This document records the agreed model and acceptance coverage. Existing development sessions require a clean reset; no automatic migration or compatibility readers are provided.

## Overview

An environment belongs to one canonical workspace and one selected combination of profile and project configuration. That combination determines its identity. Each combination has its own container and durable session state and requires an explicit `create`.

All environment commands must target the selected combination consistently. If it has no saved environment, the command fails. It must never substitute another profile's environment merely because one exists.

Configuration files are the lasting source of settings:

- **Profile:** reusable defaults.
- **Project `.devbox`:** persistent project-specific settings layered above the selected profile.
- **Global configuration:** defaults and settings shared across environments.

Remove configuration overrides from `create` and `recreate`. Configure first, then create; edit configuration, then recreate. Recreation preserves the environment's identity and saved harness state while applying the current contents of its selected configuration sources.

Keep configs sparse, preserve straightforward merge rules, and use the existing configuration editors. Do not introduce a saved per-environment override layer.

## Settled decisions

### 1. Profile selection and project participation are separate

- `--profile NAME` selects the base profile instead of the configured default. It does not implicitly exclude project settings or artifacts.
- Add `--ignore-project` to explicitly exclude all project settings and artifacts.
- Without that exclusion, a participating `.devbox` layer applies above the selected profile.
- Preserve standalone projects through generic `inherit: false`, which discards preceding sources before merging.
- A project's persistent additions apply across its participating profiles. Do not add another configuration layer for individual profile/project combinations.
- Global project-exclusion configuration and the invocation flag must use the same participation mechanism, not separate artifact-specific rules.

A participating project's `inherit: false` excludes even an explicitly selected profile. `--ignore-project` excludes the project and its inheritance setting, allowing profile-only operation. Config sources share one schema without a `name` field; the selection frontend determines identity. See `config-directory-proposal.md` for the source, image, and hook contracts.

### 2. Identity includes the participating combination

Use these suffixes:

| Participating configuration | Suffix |
|---|---|
| Named profile only | `.profile-<name>` |
| Named profile plus project | `.profile-<name>.project` |
| Standalone project | `.project` |

Retain the rewrite's readable workspace prefix and canonical-path hashing convention. Include the complete profile/project combination in the hash, saved identity, and ownership metadata. Do not implement this as a cosmetic name change over the current single project slot.

For a project that inherits profiles:

```sh
devbox-neo create . --profile blah
devbox-neo create . --profile bleep
```

These create separate environments ending in `.profile-blah.project` and `.profile-bleep.project`. Both use the project's overrides. Each must be created separately.

```sh
devbox-neo open . --profile bleep
```

This fails if the `bleep` combination has not been created, even when the `blah` environment exists. There is no automatic creation, adoption, or fallback.

Harness data is already namespaced under each session's `harnesses/<name>/`. Preserve that separation. The identity correction is not a workaround for Pi and OpenCode sharing the same history directory; they already have separate stores.

### 3. Every folder-targeted command uses the same selection rule

For `open`, `start`, `stop`, `shell`, `exec`, `ssh`, `status`, `logs`, `recreate`, `delete`, and network operations that take an environment target:

1. Canonicalize the workspace.
2. Determine the selected profile and project participation.
3. Calculate the exact environment identity.
4. Look up that environment directly.
5. Fail when that target is unavailable; do not select another saved environment.

For example, if `work` is selected but only `basic` exists, both `open .` and `start .` fail. The sole existing environment is not an implicit fallback.

Target selection and desired-runtime resolution remain separate responsibilities. Determining which environment was requested must not unnecessarily validate build inputs, load the harness registry, or parse unrelated configuration. Preserve read-only diagnostic and destructive-command access through exact targets where current desired configuration is broken.

#### Session terminology and command arguments

The session is the user-facing object. Its managed Docker container has the same name and is its replaceable runtime, not a separately selected object. A session remains identifiable when its container is running, stopped, or missing. Do not introduce separate session/container target namespaces.

Use explicit argument labels in help and documentation:

| Command kind | Argument notation | Meaning |
|---|---|---|
| Session operations such as `open`, `start`, `stop`, `shell`, and `recreate` | `<folder\|session>` | Select through the folder's profile/project rules, or use an exact saved session name |
| New-session creation | `create <folder>` | Create a session for the selected combination in this folder |
| Project configuration | `project config <folder>` | Edit the folder's `.devbox/config.json` |
| Profile configuration | `profile config <profile>` | Edit the named profile's configuration |

Apply the same explicit labels to other commands according to what they actually accept, retaining optional/repeated argument notation and distinct source/destination roles where needed. Replace vague `<target>` and profile `<name>` placeholders. Explain that `session` means the saved session name, not a Docker container ID or internal session UUID.

Configuration commands edit shared sources, not sessions. They do not require a session to exist, and several sessions can use the same profile/project files. Do not make `project config` or `profile config` accept session names just to make their syntax resemble session operations.

Keep help descriptions session-centric while naming container effects where useful: starting a session starts or restores its container; recreation replaces its container while retaining saved state. This wording does not expand which commands support missing-container recovery.

#### Exact names

An exact environment name selects its recorded identity, independent of later default-profile changes. Supplied selection flags must agree with that identity or produce an error. Never silently ignore a conflicting explicit selector.

`stop <folder> --profile work` is an ordinary valid request for the `work` combination. A conflict is specifically an exact name belonging to one combination accompanied by flags explicitly asking for another.

#### Corrupt and missing records

Direct target lookup must not read every other session record. A corrupt record for Banana must not block access to a healthy Apple target. Corruption of the requested record remains a hard error; do not treat it as absence or adopt its container.

Listing and bulk operations still inventory their complete selection and report corruption. Do not weaken ownership checks or diagnostic visibility to simplify targeted lookup.

A missing saved session is not the same as a missing Docker container. Preserve existing recorded-container recovery where supported. Never recover a different profile's environment to satisfy a request.

### 4. Recreation preserves identity, not old configuration contents

Recreating `.profile-blah.project`:

- Keeps the workspace, profile `blah`, and project participation.
- Reads the current contents of that profile and project configuration.
- Applies those settings through the existing recreation lifecycle.
- Preserves the durable session ID and saved harness stores.
- Does not switch to another profile because the global default changed.
- Does not drop project participation because invocation/global defaults changed.

Folder-based recreation first selects the exact requested environment by the common lookup rule. After selection, creation planning uses the saved combination. Explicit selection of a different combination targets that different environment; it does not rename or convert the first one.

Configuration edits that conflict with recorded participation must not silently move or relabel an environment. Define their failure behavior explicitly before implementation.

### 5. Remove creation-time configuration overrides

Remove these flags from `create` and `recreate`:

- `--harness`
- `--env` / `-e`
- `--volume` / `-v`
- `--port`
- `--network`
- `--docker-arg`
- `--on-exit`
- `--harness-arg`
- `--read-only`

Keep the corresponding supported settings in configuration, except workspace read-only mode and `on_exit`, which are removed entirely.

Keep flags that select the environment or control the operation, including `--profile`, `--ignore-project`, and applicable existing operational flags such as `--image` and `--all`.

Keep continuation and invocation-only harness arguments on `open`. Remove `--on-exit` from `open` as well as creation commands; manual `start` and `stop` determine keep-running behavior instead. Invocation-only arguments must not create a hidden persistent configuration layer.

#### Workspace read-only removal

Remove the workspace read-only flag, request/spec field, applied-input field, and workspace mount behavior. Update schema validation, fingerprints, fixtures, completion, help, docs, and tests as needed.

Do not remove generic Docker mount permission support, explicit read-only options on user mounts, or unrelated uses of "read-only" such as read-only inspection and shell completion.

#### Why overrides are not saved

We first considered retaining creation flags as temporary container settings: ordinary start/recovery retained them, while recreation used current configuration plus newly supplied flags. That was coherent but easy to misunderstand.

The final direction removes that extra lifetime rule instead. No saved override layer, no repeated creation flags, and no silent write-back into profile/project files. Configuration is the lasting source of truth.

### 6. Keep additive lists and loud conflicts

Preserve the current merge behavior rather than adding a generic merge language:

- Omitted fields inherit.
- Scalar settings replace inherited values.
- Mounts, ports, and argument lists append, subject to their existing validation.
- Environment entries compose with later assignments winning for the same variable.
- Shell argv replaces as a complete command.
- Overlapping mount targets fail rather than silently replacing inherited mounts.
- An empty additive list adds nothing; it does not clear inherited entries.

The inability to selectively remove an inherited mount is a deliberate limitation, not a bug to fix in this work. Do not add deletion markers, deduplication, replacement operators, or hidden conflict resolution.

### 7. Fix harness-argument ownership

The current resolver replaces `harness` but always appends `harness_args`. This allows a project selecting OpenCode to inherit Pi-specific arguments from its profile.

Required outcome: arguments intended for one harness must not be passed to another merely because a higher layer changes the harness selection.

Keep the flat `harness_args` array. Configured arguments require a `harness` in the same layer. Resolve the final harness, then append arguments only from layers explicitly naming it; mismatching layers contribute neither arguments nor argument provenance. Do not introduce a map of harness-specific arguments. Invocation arguments go to the recorded harness actually launched and are not saved.

### 8. Manual start controls container lifetime

- Manual `start` keeps the session's container running until explicit `stop`, regardless of attached commands.
- Without a manual `start`, the container stops after the last attached command exits.
- `open` never changes that choice. Neither do other attached-command operations.
- Calling `start` while the container is already running establishes the same manual keep-running intent.
- Successful `stop` clears that intent. Keep existing active-command protection and explicit force requirements; a rejected stop must not clear it.

Remove `on_exit` entirely: CLI flags, configuration fields, editor controls, saved launch/lease policy fields, and policy-specific diagnostics and documentation. There is no per-profile, per-project, or per-invocation shutdown policy.

Record manual keep-running intent explicitly on the session and update/check it under the existing operation lock. Do not infer manual intent from the container merely being running, from the last activity label, or from which attached command exits last. A second `open` is not a manual `start`.

Manual keep-running intent survives host reboots and Docker daemon restarts. Once Docker starts again, it automatically restarts manually started containers until the user explicitly stops them. Successful `stop` leaves the container stopped across subsequent reboots. Containers started only for attached commands do not automatically restart after reboot.

Use Docker's restart-policy mechanism to enforce this intent without requiring a Devbox command at boot. The policy is owned by Devbox and derived from manual-start state, not a new user configuration option; raw Docker arguments must not override it. Preserve the intent and corresponding policy when replacing or recovering a container. Docker restarts the existing container runtime, not a harness process, terminal attachment, or SSH connection; reboot does not apply newly edited desired configuration.

Attached commands are the existing tracked Devbox `open`, `shell`, `exec`, and `ssh` operations. Arbitrary background processes inside the container do not count as attachments. This rule does not add process monitoring or change new-session creation's stopped end state.

## Configuration simplification

### Sparse files and existing editors

Do not materialize inherited defaults into project configuration. A project adding only mounts should contain only its schema version and mount entries. Profile changes should remain inherited unless the project actually overrides a value.

Continue using strict JSON and the existing interactive editors:

```sh
devbox-neo project config .
devbox-neo profile config work
```

The editors should keep locally owned entries distinct from effective inherited values. They edit source files, not a second per-environment settings store.

No new config-editing CLI is required for this plan. Explicit scripting commands can be considered later if the existing menus and direct JSON editing prove insufficient.

### Field-name cleanup

These names reduce noise without changing merge behavior:

| Current | Proposed |
|---|---|
| `extra_mounts` | `mounts` |
| `extra_ports` | `ports` |
| `extra_env` | `env` |
| `default_shell` | `shell` |
| `ignore_project_overrides` | `ignore_project` |

Schema validation, config editors, source-entry references, help, examples, tests, and documentation use the new names. The global `global_env` contract remains separate. Old-name aliases and dual readers are not supported; only the existing explicit importer reads the old source schema.

## Implementation sequence

1. **Remove workspace read-only support.** Keep generic mount permission handling intact.
2. **Define selection and identity once.** Separate the selected profile, project participation, and exact saved target; update naming, hashing, records, and ownership checks coherently.
3. **Separate `--profile` from project exclusion.** Route profile/default/project participation through the shared owner of those rules.
4. **Unify targeted lookup.** Replace inventory-based folder fallback with exact selection across all environment commands. Keep full desired validation separate from selection.
5. **Pin saved participation for existing targets.** Exact-name access, desired-state inspection, startup, and recreation must not reinterpret identity through changed defaults.
6. **Update dependent surfaces.** Profile filters must recognize profiles participating with projects. Update list/status display, completion, clone/relocate selection and journals, diagnostics, and command guidance. Transfers must express compound identities without ambiguous slot shorthand. Standardize help/docs on session targeting through `<folder|session>`, with `<folder>` and `<profile>` for their respective configuration owners. Completion must match those accepted argument kinds.
7. **Remove creation configuration flags.** Keep operational/selection flags and direct users to existing config editors. Preserve internal prepared-creation APIs used by transfer/import; removing public flags does not mean removing required internal creation capabilities.
8. **Filter harness arguments by their layer's explicit harness.** Require local harness ownership for configured arguments; retain flat arrays.
9. **Replace `on_exit` with manual-start intent.** Explicit `start` keeps the container running until `stop`, including automatic restart after host/Docker restart; otherwise the last attached command triggers shutdown and there is no reboot restart. Derive the Docker restart policy from saved manual intent, preserve it through container replacement/recovery, and protect it from raw Docker overrides. Remove configuration and invocation policy overrides, and preserve operation-lock ordering for concurrent start/stop/attachment cleanup.
10. **Apply config naming cleanup.** Keep sparse files and existing merge rules.
11. **Synchronize documentation and validation.** Update runtime-contract notes, contributor guidance, seeded/embedded guidance, CLI help, and the guide/reference/architecture tiers. Update progress honestly.

This is a development identity/schema change. Plan for a clean reset of affected development sessions. Do not add automatic migration, adoption, compatibility readers, or legacy naming aliases. Do not delete any user sessions or state as part of implementation without explicit authorization. Audit the separate migration utility's dependencies; changes to its conversion behavior need explicit scope approval rather than hidden runtime fallbacks.

## Acceptance tests

### Selection and identity

- Create the same workspace with profiles `blah` and `bleep` plus project overrides; assert distinct names, hashes, ownership identities, session IDs, and stores.
- Require separate creation for each combination.
- An explicit profile still includes project mounts unless project participation is excluded.
- Profile-only, profile-plus-project, and standalone-project identities remain distinct.
- Symlink aliases resolve to the same canonical workspace identity.
- `--ignore-project` excludes every project artifact, not just config fields.

### Lookup

- With only `basic` saved and `work` selected, all folder-targeted commands fail for the missing `work` target; none touches `basic`.
- An exact name continues targeting its recorded combination after defaults change.
- Conflicting exact-name selectors fail consistently before mutation.
- An unrelated corrupt record does not block targeted access; a corrupt target does fail.
- Missing session and missing container errors/recovery remain distinct.
- Bulk/list operations retain diagnostics for corrupt records and unmatched owned containers.
- Help and completion distinguish `<folder|session>` session operations, `create <folder>`, `project config <folder>`, and `profile config <profile>`; optional/repeated forms retain their existing arity.
- Configuration editing works without an existing session and does not interpret session names as configuration-owner names.
- A saved session uses the same exact name regardless of whether its managed container is running, stopped, or missing; no separate container selector is introduced.

### Recreation and configuration

- Recreating a combined environment retains the recorded profile and project participation after defaults change.
- Current contents of the recorded configuration sources apply without changing the environment's session ID or losing harness state.
- Removed creation flags are rejected and absent from help/completion.
- Workspace read-only support is absent; user mount read-only options still work.
- Sparse projects inherit profile settings without seeded copies of defaults.
- Lists remain additive, empty lists do not clear inheritance, and overlapping mounts fail loudly.
- No profile/project combination accepts arguments belonging to a different harness under the agreed argument model.
- Profile filtering, completion, transfer planning/retry, and config inspection agree with normal selection and identity.
- Invocation-only launch arguments do not silently become persistent configuration.

### Manual start and automatic shutdown

- Manual `start` with no attachments stays running until `stop`.
- A manually started container automatically restarts when Docker starts after a host reboot or daemon restart, without a Devbox invocation.
- Explicitly stopped containers stay stopped across reboot; automatically started attachment-only containers do not restart at boot.
- Recreation/recovery retains manual intent and its Docker restart policy; ordinary opens do not change either.
- Raw Docker options cannot override the managed restart policy.
- Container restart does not resume prior harness processes, terminal attachments, or SSH connections, or apply changed desired configuration.
- An automatically started session remains running while any tracked attachment is active and stops after the last one exits.
- A manually started session remains running after all attachments exit, regardless of attachment type or exit order.
- Calling `start` during an active `open` keeps the container running after that attachment exits.
- A second `open` does not establish or clear manual keep-running intent.
- Successful `stop` clears manual intent; a later `open` without another manual `start` uses automatic shutdown.
- A rejected or failed stop does not prematurely clear manual intent.
- Separate CLI processes observe the same recorded intent, including concurrent start and final-attachment cleanup under the operation lock.
- `on_exit` is absent from configuration, CLI help/completion, editor controls, and saved launch/lease policy fields.

Run focused package tests during implementation and `make test` in `/workspace/rewrite` after changes. Run broader checks appropriate to changes in records, lifecycle, ownership, and transfers. Do not use live user sessions or Docker resources for tests.

## Resolved implementation details

- Explicit profile plus standalone project fails unless the project is excluded.
- Recorded participation is pinned. A missing recorded project or newly conflicting standalone setting fails instead of changing slots.
- Configured arguments remain flat arrays and require their own layer's harness; mismatches are ignored.
- Transfer slots use the same suffix notation: `.profile-NAME`, `.profile-NAME.project`, and `.project`. `--profile` selects the source; `--to` selects the destination combination. Cross-folder relocation retains its source combination.
- Continuation arguments precede invocation-only harness arguments. Neither `--harness-arg` nor trailing arguments modifies the saved launch arguments.
- Manual intent controls shutdown and Docker restart policy; clone starts automatic, while relocation and recreation preserve intent.
- Config field renames apply without runtime aliases. The existing explicit migration utility converts legacy source fields and reports removed settings.

## Alternatives considered, not selected

- **Saved per-environment overrides:** adds another layer that needs inspection and removal controls and can mask profile changes. Project configuration already covers the ordinary persistent customization need.
- **Snapshot-authoritative recreation:** preserves recorded settings but requires a separate operation for applying edited configuration. Keep the existing distinction between recorded recovery and configuration-driven recreation instead.
- **Temporary creation overrides:** coherent when clearly documented, but ultimately dropped to remove the surprising recreation lifetime rule.
- **Saved-environment fallback for folder commands:** rejected. Selecting a profile without an environment must fail, never access another profile.
- **Automatic list replacement or removal machinery:** not needed. Additive mounts and loud overlap errors are intentional.
