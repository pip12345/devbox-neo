# Lifecycle and applied state

`app.Engine` owns environment transitions. It resolves desired inputs before executing them, validates the recorded environment under an operation lock, and commits state only at defined application points. Docker execution consumes the captured plan rather than rereading source files.

The lifecycle code stays in one `app` package, with files organized by responsibility:

| File | Ownership |
|---|---|
| `engine.go` | Dependencies, request/result types, resolution, and ownership checks |
| `open.go` | Open sequencing and harness launch |
| `create.go`, `creation_record.go` | Creation/recreation, record assembly, materialization, and commit cleanup |
| `mount_plan.go`, `mounts.go` | Store/auth mount planning, managed-config synchronization, and recorded mount-parent preparation |
| `startup.go` | Config-independent access to recorded runtime |
| `recreate.go`, `open_hooks.go` | Explicit application planning and container-owned hook copies |
| `durable_stores.go` | Previously committed harness-store validation |
| `attach.go` | Attachment leases and last-command cleanup |

Command-specific orchestration remains explicit; sharing preparation does not make creation, ordinary access, recovery, and transfer rollback interchangeable.

## Desired specification versus recorded contract

`environment.Spec` is the captured desired environment for an operation. It includes resolved configuration, harness definition, source files, image plan, and `environment.Inputs`.

The saved `store.Record` records identity, actual image/container association, current mounts/launch settings, and a compact applied comparison baseline. It is not a historical reconstruction recipe. Existing containers keep creation settings until explicit recreation commits.

```mermaid
flowchart TD
    SRC[Config and source files] --> SPEC[Captured desired spec]
    SPEC --> CMP[CompareInputs]
    REC[Saved applied inputs] --> CMP
    CMP --> REPORT[Status and drift reasons]
    SPEC --> APPLY[Create or synchronize]
    APPLY -->|success| REC
```

### One input model

`environment.Inputs` has committed absolute source directories plus image, container, and runtime snapshots. Aggregate fingerprints and detailed changes both derive from it. There is no second settings registry or diagnostic file scan that can disagree with lifecycle decisions.

| Scope | Representative inputs | Baseline advances |
|---|---|---|
| Image | Base image, ordered Dockerfiles/contexts/ignore rules, build args, generated preparation/boundary/finalization layers, harness definition | Creation/recreation commit |
| Container | Image dependency, mounts, network, env verification, ordered setup inputs | Creation/recreation commit |
| Runtime | Managed files, launch settings, ordered before-open inputs, runtime assets | Successful explicit application through `Record.ApplyRuntime` |

Schema `7` validates public settings and aggregate content hashes, never file contents or env values. Managed trees and build contexts contribute one digest each rather than a persisted per-file inventory. Applied source directories retain provenance for config-usage reporting, not authority to reconstruct historical inputs.

`CompareInputs` emits public-setting and category changes in stable order. Action priority is image over container over runtime. The image-to-container hash dependency does not become a duplicate user-facing reason. Dockerfiles/scripts retain specific reasons; build-context and managed-tree reasons deliberately do not enumerate changed filenames.

Source paths explain changes but do not themselves change fingerprints when effective input bytes are identical. Relative context/config paths remain semantic inputs. Env diagnostics expose names, not values or hashes. The applied container fingerprint includes the actual image ID rather than only a mutable tag.

## New-session creation

`Engine.Create` checks workspace/name uniqueness under the namespace lock, allocates a session ID and storage directory, acquires its operation lock, and resolves the explicit sources. Container creation allocates a separate Docker name. It then enters the shared creation pipeline. `Request.MakeDefault` is an explicit creation-only option; neither the first session nor a transfer destination becomes a default implicitly.

```mermaid
flowchart TD
    I[Identify named target] --> L[Lock and reject existing state]
    L --> R[Resolve and capture inputs]
    R --> NEW[Verify unused runtime]
    NEW --> B[Build or select image]
    B --> P[Prepare stores and managed config]
    P --> C[Create and start container]
    C --> S[Install runtime and run preparation]
    S --> SETUP[Run setup and verify harness binary]
    SETUP --> COMMIT[Commit session record]
    COMMIT --> STOP[Stop prepared container]
```

The ordered `setup.sh` chain belongs to the per-container contract. Before-open scripts and harness attachment are not part of standalone `create`. Successful creation leaves the environment stopped.

`createAs` prepares resources before passing explicit inputs to the side-effect-free `creationRecord` helper. The caller owns ID allocation and clock reads; the helper assembles fields and applies the existing activity/action/manual-start rules for new sessions, recreation, and prepared destinations. Materialization supplies the setup-container ID before publication.

The record commits only after startup, declared preparation, setup, and binary-availability checks succeed. Creation then stops the container and, if requested, selects the saved session as the folder default while retaining its operation lock. A stop failure does not prevent the requested default selection. A default-selection failure retains the created session and returns an exact `edit --default` repair step. Neither failure is presented as an absent session that can be created again. `CreationResult.Saved` confirms successful record publication independently of completion errors; the UI uses that outcome rather than classifying lifecycle error codes. An unconfirmed publication is not proof of absence.

Recreation selects saved identity and rereads it under the operation lock before resolving its current desired references. A later default change cannot retarget the invocation. Source edits do not rename the session; a replaced durable ID fails rather than being adopted.

Explicit application preserves the session ID and stores. Runtime-only changes use the existing container. Changed container inputs or missing runtime require replacement; changed image inputs require a build too. `--container` forces replacement and `--image` forces an uncached build plus replacement. `--force` permits interruption and always replaces the container. The creation owner retires captured attachment leases only after verified old-runtime removal (or confirmed absence); a failure before removal retains their protection. Interrupted automatic sessions finish stopped; manual-start intent is retained. An unchanged available owned image can be reused. Running/stopped intent is retained. Container-local state is lost only when the container is replaced.

## Ordinary access

`app.startAccess` is the shared boundary for `open`, `start`, `shell`, `exec`, and `ssh`. It validates ownership and durable stores, starts the recorded container if needed without requiring attachment idleness. It never resolves desired configs or synchronizes managed files. Pending workspace/config edits therefore do not block access to the applied runtime. Missing backing stores remain errors; missing containers require explicit recreation.

Open and Continue launch the recorded harness and arguments. Before-open scripts are installed under `/devbox/hooks/` during creation/application, keyed by their content hashes. The recorded runtime inputs already retain their order and hashes, so session schema 7 needs no conversion or script contents. Staging and atomic file replacement prevent failed publication from truncating previously applied scripts. These copies are disposable container files, not a historical reconstruction archive.

Open checks the complete applied hook chain before running it. Each hook is a separate process; failure blocks harness attachment. Shell/Exec never run before-open hooks or parse live harness JSON, so they remain usable for repair. An older container without installed hook copies gets an exact `recreate` step, never a fallback to current source files.

## Explicit configuration application

`planRecreate` captures current inputs under the operation lock and uses the existing image/container/runtime comparison. `applyRecreate` reports its chosen work before mutation. Runtime-only application stops an idle container, reconciles managed files, installs captured hooks and runtime assets, and commits launch settings plus the runtime baseline. It restores previous running/stopped intent using bounded cleanup even after cancellation. A failed reconciliation may have changed some files; the error reports partial application rather than promising rollback. Only successful application advances the saved baseline.

Container/image changes use the shared creation pipeline. A missing image alone does not replace a healthy container. Bulk application selects saved sessions regardless of container presence, then preflights and captures all selected plans before executing them. Corrupt records and pending transfers block the batch; incomplete allocations have no saved session and remain for explicit cleanup. Both interfaces use the same application owner.

Typed plan diagnostics are collected in `Result.Diagnostics` and delivered synchronously through `Engine.OnDiagnostic`. The CLI renders them on stderr before runtime mutation. Callbacks may run under the operation lock and must not reenter session operations. Source-reading warnings and child streams remain separate. Shared resolution returns captured warnings without printing. Creation/application report them explicitly before mutation; status and transfer previews return warning data for the CLI/menu presentation owner, outside JSON command data. Ordinary access does not resolve config merely to produce drift warnings; Status owns that inspection.

## Missing runtime and explicit recreation

Containers and images are disposable, but their absence does not authorize replacement. Open, Start, Shell, Exec and SSH return `container_missing` with an exact `dbx recreate <session-directory>` step. They leave saved identity, applied state and history unchanged. Logs/status never build runtime. A healthy container remains usable when its image has been pruned.

Only explicit recreation replaces an existing session's runtime. When replacement is needed, it resolves current selected sources under the operation lock and calls the creation pipeline with the previous record. It preserves session ID, directory, creation time, history, defaults and manual-start intent. It reuses a matching available installation-owned image or builds a new one; it does not reconstruct historical configuration or adopt unowned images.

The creation path checks previously committed harness-store roots before creating directories; missing durable stores are errors, not empty replacements. Still-requested previously mounted named volumes must exist. A new runtime association commits only after materialization succeeds.

Committed transfer retries finish source cleanup even when destination runtime is missing. They verify any existing association but do not rebuild, resolve destination config or recopy committed stores. Once cleanup releases the endpoints, the user can explicitly recreate the destination.

## Attached-command leases

The operation lock covers loading, ownership/store validation, startup, and lease creation. The foreground command then runs without that lock.

```mermaid
sequenceDiagram
    participant C as CLI
    participant E as Engine
    participant S as Store
    participant D as Docker/runner
    C->>E: Access request
    E->>S: Lock, load, validate
    E->>D: Prepare and start if needed
    E->>S: Publish lease and release lock
    E->>D: Run foreground command
    D-->>E: Exit
    E->>S: Reacquire lock, remove lease
    E->>D: Stop if idle and not manually started
    E-->>C: Result plus cleanup errors
```

Cleanup uses an independent bounded context so cancellation of the foreground operation does not skip state cleanup. Under the operation lock, lease removal reports whether the attachment still owned it. A lease retired by forced replacement makes late cleanup a no-op, so it cannot update activity or stop the replacement. Otherwise cleanup reaps stale processes and reads current manual-start intent. The last attachment stops the container only when `manual_start` is false. A failed hook or lease setup stops a newly started automatic container when no other attachment exists.

Leases contain Linux process start ticks and boot identity to distinguish PID reuse. Corrupt or unverifiable leases fail closed rather than being assumed idle. Foreground SSH controllers use the same lease owner as Docker attachments.

### Manual start and Docker restart

`manual_start` belongs to the session, not to config fingerprints or individual leases:

| Operation | Effect on `manual_start` |
|---|---|
| Explicit `start` | Set, even when already running |
| Successful `stop` | Clear; a rejected stop leaves it unchanged |
| Attach or detach a command | No change |

The value survives recreation and reboot. Docker enforces it through `unless-stopped` when set and `no` otherwise; raw Docker options cannot override this policy.

`saveManual` updates Docker's policy and saves the record under the operation lock. If saving fails, it attempts to restore the previous policy so a failed command does not silently change reboot behavior.

At boot, Docker starts the existing container without CLI preparation. It does not apply changed config or restore harness processes, SSH connections, or terminal attachments.

## Invocation-local terminal metadata

The CLI captures the `app.TerminalEnv` allowlist once into the engine. During materialization it prepends those values to a temporary env plan, before harness and configured values. Terminal values do not participate in the saved comparison baseline, including during rebuilding.

Attached harness, shell, and exec commands receive current terminal variables through Docker exec overrides, with or without TTY allocation. Present empty host values are forwarded; absent keys do not clear container values. Internal hooks/preparation inherit creation env rather than attachment overrides.

This separates display capabilities from environment configuration: changing terminals does not cause drift or require recreation, and no host dotfiles need importing.

## Runtime documentation and network facts

`assets` embeds the human docs, development notes, and container agent guidance. The engine stages that bundle with inspected network facts in a private `.runtime-*` directory, then copies it into a verified running container's `/devbox` directory.

The adapter applies root-owned read-only permissions for `devuser`, excluding the live `/devbox/ssh` mount from recursive ownership/permission changes. Host staging is removed on success or failure. Creation/application installs the documentation bundle; ordinary access and network changes refresh only live network facts. Neither operation resolves desired config to refresh those facts.

Network commands inspect actual attachments under the operation lock. Secondary-network changes cannot detach the configured primary and do not change creation fingerprints. Managed changes refresh in-container facts while running; external Docker changes appear on the next refresh.

Pi/OpenCode inherit the `devbox` skill through harness defaults. Artifact setup leaves it inherited, while explicit config-directory overrides use normal tree resolution. Claude inherits a `CLAUDE.md` that imports `/devbox/AGENTS.md`; its optional harness-file setup includes that import. The asset content hash is a runtime input, not an image input, so updated guidance does not itself require an image rebuild.
