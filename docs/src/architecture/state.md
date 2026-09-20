# State, locking, and transfers

The saved session is the top-level environment model. Docker inventory supplies live runtime facts, but container absence does not erase identity, history, or the recorded recovery contract. `store` owns durable state; `app` combines it with verified Docker state before transitions.

## Identity and ownership

`environment.ContainerPrefix` defines the `devbox-` lookup convention independently of `docker.Namespace`, which defines `devbox-rewrite.*` labels and image tags.

Identity uses the canonical workspace path and retained profile/project selection. The selection frontend produces `.profile-<name>`, `.profile-<name>.project`, or `.project` suffixes. Session names also include a readable folder basename and a 12-hex workspace/slot hash. Saved source directories are recorded explicitly. The generic config merger does not name environments, and source files have no `name` field. Exact names use recorded sources even when defaults change.

Folder targeting uses the selected profile and project metadata. Saved project-directory overrides are matched by workspace/profile; differing matches require an exact target, and unreadable matching project slots prevent guessing. Unrelated profiles and profile-only slots do not participate in override lookup. `--ignore-project` selects profile-only configuration without consulting overrides. Inheritance changes cannot rename recorded state implicitly.

The readable basename is lowercased and bounded to 32 characters from `a-z0-9_.-`; invalid runs become `-`, edge punctuation is trimmed, and an empty result becomes `workspace`. Sanitization and truncation do not change the hash input. Symlink aliases therefore produce the same identity. Records validate against the naming rule.

Docker ownership checks use installation ID, ownership version, session ID, workspace, and slot labels. Application inspection additionally verifies the image and recorded container instance association. A matching name with different labels or instance identity fails rather than being adopted.

Images carry installation ownership and final session tags. Removing a tag requires verifying both ownership and its expected image association; a mutable name alone does not authorize deletion.

## Record structure

`sessions/<container>/session.json` holds:

- stable session ID, canonical environment identity, and `manual_start` intent;
- recorded image/container association and creation settings;
- launch settings, definition source verification, setup input, and environment source references;
- complete applied image/container/runtime inputs and fingerprints;
- creation time, last recorded activity, and last action.

Schema `3` requires complete applied snapshots and records manual keep-running intent separately from desired configuration. Public settings, source paths, modes, and hashes are durable; env/auth values and file contents are not. Raw env diagnostics are redacted. Records are atomically replaced with restrictive permissions, and invalid records stay errors rather than becoming absence.

Creation/recreation commits image and container baselines. `Record.ApplyRuntime` advances runtime inputs with their fingerprint at application commit points. Status and warning generation never alter either baseline.

## Locks and leases

Operation and record locks live under `state/locks/sessions/`, outside removable session directories. Otherwise deleting a session could unlink a lock pathname while another process still holds the old inode, letting a second process acquire a different lock for the same logical resource.

| Lock / record | Lifetime and job |
|---|---|
| Installation lock | Serialize initial home/installation identity creation |
| Configuration-owner lock | Serialize publication or mutation of one source owner |
| Session operation lock | Serialize ownership checks and lifecycle transitions |
| Session record lock | Protect short record read/write/delete operations |
| Attached-command lease | Represent a foreground command while its operation lock is released |
| SSH owner/master flocks | Govern transient SSH process lifetime, independently of durable record locks |

Operations involving several environments acquire the complete lock set in sorted order. Bulk preflight and mutation retain that set so another command cannot replace a selected target between checks or prompts.

Leases use Linux process start ticks and boot identity, not PID alone. Inspection reads active state without reaping it. Mutations can reap provably stale leases; corrupt or unverifiable ones fail closed. Session deletion requires idleness even when container removal is forced.

SSH lifetime locks deliberately remain in transient connection directories: supervisors hold open inodes, so revocation still works if a path is removed. They are not substitutes for external environment-operation locks.

## Inventory and status

Inventory joins one installation-filtered Docker list and batched inspection with saved directories and pending-transfer endpoints. `app.InventoryReport` separates `sessions` from `unmatched_containers`.

| Observation | Representation |
|---|---|
| Valid session and container | Session row with live runtime facts |
| Session without container | Retained session row, marked missing |
| Managed container without record | Separate unmatched-container diagnostic |
| Corrupt/incomplete saved state | Session diagnostic, not fabricated valid state |
| Pending endpoint without record | Inspectable reserved endpoint |

Profile filtering uses valid recorded identity, or live slot labels when records are unavailable. Broken entries with unknown profiles remain visible in unfiltered inventory. Listing does not resolve desired configuration and never repairs or adopts resources.

Bulk status enriches the same inventory with the normal resolver and `environment.CompareInputs`. Runtime state remains independent of configuration health: a missing container can still have comparable inputs, while a running container can have invalid desired config.

Single-target `app.Status` reads the record and live leases under the operation lock, inspects the linked container, and uses the same comparison. `StatusDetails` adds full `record` and `active` fields without bloating bulk rows. Pending transfers skip desired resolution because their transaction, not current configuration, governs recovery.

Last activity means recorded Devbox operations, not filesystem activity or only harness launches. Container creation time in listing comes from Docker, independently of durable session creation time.

## Deletion transaction

`app.Delete` owns both runtime and saved-data phases. Explicit `--container` or `--session` selects scope without prompts. Interactive deletion supplies two default-no callbacks; non-interactive, JSON, and dry-run calls must provide scope.

```mermaid
flowchart TD
    SELECT[Resolve intersecting selection] --> LOCK[Lock complete target set]
    LOCK --> CHECK[Preflight ownership and scope]
    CHECK --> CONFIRM[Confirm container phase if interactive]
    CONFIRM --> RECHECK[Recheck filters and active commands]
    RECHECK --> REMOVE[Remove selected containers]
    REMOVE --> SAVE{Delete saved state?}
    SAVE -->|yes| VERIFY[Require idle and verify absence/tag]
    VERIFY --> DELETE[Remove state and verified image tag]
    SAVE -->|no| KEEP[Retain saved environment]
```

Explicit saved-data deletion is preflighted before container removal and rechecked afterward. Container failures do not advance into state deletion. `--force` only relaxes attached-command protection for the runtime phase; it cannot expand scope or bypass pending transfers and saved-state idleness.

The complete operation-lock set spans confirmations and both phases. Record-directory removal also holds the short record lock. External lock files survive deletion. If cancellation or failure occurs after containers have been removed, remaining state is retained rather than pretending the whole operation rolled back.

Selection filters intersect. Age uses recorded activity, and unknown activity is not guessed to be old. Activity and orphan status are rechecked under lock, including after confirmation, before deletion records its own activity. Dry-run preflight examines leases without reaping them. This prevents a stale preview or prompt from selecting a newly active/recovered environment.

## Transfer state machine

`app/transfer.go` owns `copy` / `copy --move` orchestration; `store` owns journal persistence and portable store copying. A single external journal at `state/transfers/<source-container>.json` reserves both endpoint names.

Both endpoint operation locks are acquired in sorted order. Ordinary `Locked.Load` rejects pending work, while inventory and transfer operations can inspect it. Pending lookup scans unfinished journals; corrupt journals fail mutations closed because endpoint reservations cannot be trusted.

The journal records public endpoint identities, the original explicit `--to` selector (`requested_to`), session IDs, transfer mode/phase, intended running state, and destination input fingerprints. Retries accept the original selector or the effective destination suffix, even when an inheritance cutoff removed the requested profile; they do not reload config to reinterpret the selector. Internal modes remain `clone` for `copy` and `relocate` for `copy --move`; JSON transfer results and pending summaries use these mode values too. It is not a second creation record and contains no env/auth values. Destination creation accepts the journal's allocated identity so retry cannot allocate another session.

```mermaid
stateDiagram-v2
    [*] --> Prepare: reserve endpoints
    Prepare --> Prepare: rollback and retry
    Prepare --> Committed: destination ready
    Committed --> Committed: recover and clean up
    Committed --> [*]: remove journal
```

### Prepare: source is authoritative

The engine validates current portability declarations and requires the source's active definition to remain unchanged. `copy` requires a stopped or absent source; `copy --move` may stop a running source and remember its intent. Both require idle endpoints and an unused destination.

Only declared environment stores and ownership manifests are copied. Auth overlays, cache stores, records, leases, and SSH runtime data are excluded. Workspace files and container-layer tools are not part of the state tree. Symlinks are copied as opaque entries without traversal; special files are rejected.

Destination resolution uses the normal configuration pipeline. Image building, synchronization, setup, and runtime installation follow ordinary creation. `copy` leaves the destination stopped; `copy --move` restores the source's original running intent at the destination.

If preparation fails, bounded rollback cleans the destination, restores the source image tag after a relocation build, and restarts a previously running source. The journal remains pending. A preparation retry requires matching destination fingerprints and recopies the authoritative source because rollback may have restarted it and allowed its state to change.

### Committed: destination is authoritative

Publishing `committed` changes authority before source removal. Once publication is attempted, rollback cannot delete the destination: a directory sync error can occur after rename already succeeded.

A committed retry does not resolve new desired config or copy state again. It verifies the recorded destination, recovers a missing destination container when recorded inputs permit, and finishes source cleanup. Copying again here could overwrite newer destination history with stale source data.

The journal lives outside the source directory so deleting source state cannot lose the recovery plan or reservation. Only completed cleanup removes the journal and releases both names. `copy` creates a new session ID; `copy --move` preserves it. No permanent lineage record is needed after completion.
