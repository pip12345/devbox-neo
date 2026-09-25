# Runtime architecture

Devbox separates desired configuration from the recorded environment it actually created. Configuration describes what the user wants now; the session record describes what can safely be started, inspected, recovered, or removed. The application engine owns transitions between those states.

The implementation targets Linux directly. File locks, process start identity, boot identity, terminal ioctls, and process exit status use Linux APIs.

## Component layout

```mermaid
flowchart TD
    CLI[cli: commands and presentation]
    CLI --> APP[app: lifecycle transitions]
    CLI --> RES[resource: source edits]
    APP --> ENV[environment: desired spec]
    RES --> ART[artifact: layer resolution]
    ENV --> ART
    ART --> CFG[config: schemas and merge]
    ENV --> HAR[harness: declarations]
    RES --> HAR
    APP --> STORE[store: records and locks]
    APP --> SYNC[filesync: managed files]
    APP --> DK[docker: typed CLI adapter]
    APP --> SSH[sshshare: connection lifetime]
    APP --> ASSET[assets: docs and network data]
```

All package paths below are under `internal/`.

| Package | Owns | Boundary |
|---|---|---|
| `cli` | Cobra commands, input, tables, menus, JSON/error rendering | Translates requests; does not implement lifecycle policy |
| `app` | Create, access, recreate, delete, inventory, transfer orchestration | Orders resolution, locks, validation, and external effects |
| `resource` | Config-directory publication, optional artifacts, setting edits | Mutates source files, not sessions or folder defaults |
| `config` | Strict schemas, host substitution, field validation and merges | Does not choose which artifacts participate |
| `artifact` | Ordered composition, provenance, artifact overlays, captured source trees | Consumes explicit absolute sources without discovery |
| `environment` | Identity, desired spec, image plan, input snapshots and comparison | Compiles resolved data before execution |
| `harness` | Definitions, registry, defaults, mount-parent derivation | Declares capabilities without owning transitions |
| `store` | Durable records, external locks, leases, transfer journals and copying | Distinguishes absent, corrupt, and reserved state |
| `filesync` | Managed file and shared-JSON ownership | Application must establish that synchronization is safe |
| `docker` | Typed Docker requests, validation, ownership checks and execution | Receives plans rather than reloading configuration |
| `sshshare` | Connection directories, supervisor and generated client config | Owns an individual SSH master's lifetime |
| `assets` | Embedded container guidance and docs bundle | Produces runtime content, not session authority |
| `commanderror` | Typed failures and actionable next steps | Carries causes separately from serialized diagnostics |

Pi, OpenCode, Claude Code, and custom definitions use the same engine. Installation, stores, auth overlays, merge keys, preparation commands, and continuation arguments are data in a harness definition.

## Core invariants

- **Names locate; labels authorize.** A deterministic name never makes a Docker resource safe to mutate.
- **Sessions outlive containers.** Losing a container does not erase the recorded environment.
- **Resolution is not application.** Inspecting changed configuration cannot advance the saved baseline or authorize recreation.
- **One ordinary startup boundary owns synchronization.** Access commands share stopped-to-running preparation.
- **Long commands do not hold operation locks.** They publish leases before releasing the lock; cleanup reacquires it.
- **Secret values are transient.** Recorded source references and keyed hashes support verification without storing env/auth values.
- **Transfer commitment changes authority.** A committed destination must never be overwritten by a retry from the source.

## Follow a subsystem

- [Lifecycle and applied state](lifecycle.md) — creation, startup, recovery, drift, attachment and runtime assets.
- [Configuration and images](configuration.md) — resolution, source edits, declarative harnesses, synchronization and image compilation.
- [State, locking, and transfers](state.md) — ownership, records, inventory, deletion and transactional movement.
- [SSH sharing](ssh.md) — authentication, publication, revocation and host/container masters.

## Error ownership and rendering

`commanderror.Error` carries a stable code, safe message, target, operation metadata, and optional `Step` values. Owners attach these at known failure predicates; the CLI does not infer classifications by matching prose. Causes remain available through `errors.Is` and `errors.As` but are not serialized.

File absence remains a filesystem error until the application owns enough context to turn it into a missing-environment result. Wrapped absence checks use `errors.Is`. This distinction is required for fresh creation and interrupted-transfer discovery: corrupt records must not enter an absence branch.

`cli.Execute` is the process rendering boundary. Human output uses a short error header and separate target context on stderr. JSON-capable commands emit one object on stdout. Child streams retain their normal shape, and child/Docker exit status remains authoritative when joined with cleanup errors.

`Step.Reason` supplies the label beside a shell-quoted command. Owners express sequences with `Then` and alternatives with `Or`; the renderer has no repair policy. The same renderer handles resource-success guidance. Suggested commands never run automatically and do not add force flags by default.

Guidance has two target contracts:

- Missing-session and config-owner hints retain the target spelling the user entered, such as `.` or `./devconfig`. Generic creation guidance uses normal selection rather than replaying unrelated flags.
- A recorded session or transfer retry retains its exact identity and endpoint selectors, independent of current defaults.

Project owners therefore keep the entered folder in `Name` while `Workspace` and `Root` remain canonical for filesystem operations. Explicit home selection is carried into next steps.

Command groups validate unknown commands before Cobra flattens suggestions into prose. Suggestions remain structured steps, user arguments stay quoted, and empty groups show help without opening a store.

## Presentation and completion

`internal/cliui` exposes synchronous workflows over a Bubble Tea v2/Lip Gloss frontend for the main CLI and separate migrator. One runner owns the terminal for a command. `terminal.go` passes immutable screen snapshots to one UI goroutine and waits for a selection; `tui.go` owns keyboard input, filtering, layout, scrolling, text controls and confirmation presentation. The workflow goroutine alone invokes action handlers and services. Collections contain object items with stable identity keys and open handlers; commands remain a separate action list. Structured fields carry detail values and status/warning semantics without parsing display text. Hidden-action filtering and filtered-list index mapping preserve the displayed snapshot's binding to its handler; labels never choose behavior. A nested workflow inherits a navigation snapshot so the selected object stays visible, while only its menu accepts input. Inventory refresh restores object selection by key, not row number. The UI goroutine is joined on exit, including cancellation and failures. Redirected/dumb-terminal prompting uses the plain line-oriented controls.

Workflows are ordinary functions with local state. A parent calls a child with the same runner and resumes after it returns; there is no router or global state store. Session creation and saved-session editing share config-chain actions but supply different update callbacks. Both nested config creation and `config create` call `cli.createConfig`. Its overview owns the pending destination, harness, and artifact choices; a supplied destination prefills the draft. Optional-files selection uses a copied slice so cancelling that editor cannot mutate accepted draft choices. Only the explicit Create config action invokes `resource.CreateConfig`. Services retain locking, validation, and publication ownership. Back does not undo committed operations; partial config creation is reported rather than retried or rolled back by the UI.

`folderSessionScreen` renders the shared header and rows for the folder editor and default picker; each caller binds its own selection behavior. Default changes appear in the persistent header/marker and the exit receipt, not transient notices that shift rows. Those controls remain in the direct `edit <folder>` editor. Browser session menus instead call SetDefault/ClearDefault directly for their selected target, refreshing both details and navigation markers. Folder browser menus retain clearing for unavailable default targets.

Bare root/config entry points use `cli/frontend*.go` to browse real inventory and collect operation arguments. Direct subcommands remain independent entry points into the same editors and services. Config browsing does not depend on Docker; session inventory failures remain visible without removing config navigation. Inventory refresh is synchronous, on screen entry or explicit refresh, rather than periodic background polling. Browser creation starts with an editable current/selected folder and no name or configs. It supplies a materialization callback to the shared creation overview so build errors retain the same draft. Service errors retain partial-resource recovery instructions; a committed creation whose stop failed enters that saved session's menu instead of retrying creation.

Bubble Tea owns raw input and the alternate screen while menus are active. `Pause` uses its blocking terminal handoff to release the renderer and input reader together before foreground application code runs. The workflow resumes the same UI at its next interaction. No UI reader competes with the harness, shell, exec, logs, SSH, or result acknowledgement. Foreground operations also capture and restore the pre-operation termios state before result output and acknowledgement: a cancelled Docker client can leave raw input behind, where Enter no longer supplies a newline. SSH uses the same restoration helper. Streamed results and warnings stay in the normal terminal until acknowledged. Confirmations default to No and also leave an audit line in that terminal. `Finish` restores the terminal without cancelling command work that may follow it, such as creation after a completed input form. UI cancellation reaches command-owned work through the menu's context; `SignalContext` routes foreground SIGINT to the operation context, while SIGTERM cancels the whole command. The terminal adapter translates command cancellation into graceful Bubble Tea Quit rather than its external-context force exit, which skips the input-reader join. The cancellation callback is also joined before terminal completion. Ultraviolet is pinned to include its upstream StreamEvents reader-join fix; a regression test requires that the event stream cannot return while its input goroutine remains active. Session source edits record saved sessions for a short exit receipt with exact `status` targets; saving sources does not apply container changes. The common formatter keeps scalars and shell argv inline, renders other lists below their field, and wraps within the terminal width. Source provenance comes from the resolver, not comparisons against displayed values. `terminalColors` centralizes TTY, `NO_COLOR`, and `TERM=dumb` handling.

Human list tables render `View.LocalName` as `NAME`, with folder context distinguishing equal local names. Wide output adds `View.Name` as `FULL NAME`. The full name remains the lookup identity in status, JSON, diagnostics, and sorting; this presentation choice does not change container names or saved state. Invalid records without local-name metadata remain identifiable by their full names.

Completion bypasses application/store initialization and locking record readers. Existing config/session directories provide lookup hints, including exact names whose records are corrupt. Local-name suggestions read identity metadata and are scoped to the supplied folder. Harness enumeration exposes only valid effective definitions. Live container completion uses a bounded installation-filtered inventory and quietly omits unavailable sources. Shell completion must not initialize a home, create locks, resolve a complete environment, or mutate Docker.

Shell generators register `devbox-neo` and an existing `dbx` shortcut against the same handlers. They do not define the shortcut. Invoking the typed shortcut preserves its own executable and flags; Zsh's autoload header advertises both command names.

## Validation strategy

Unit tests use temporary homes and a command-boundary Docker fake. This lets tests assert ordering, ownership checks, lock behavior, input redaction, missing-container recovery, and rollback at specific failure points without touching host environments.

`make test` runs unit tests. `make check` also formats, race-tests, and builds. `make test-integration` opts into the real-Docker suite, which exercises image installation, declared stores, managed auth, and harness lifecycle against a daemon. Fake-boundary tests do not establish that upstream installers or interactive provider login work; those require real runtime validation.
