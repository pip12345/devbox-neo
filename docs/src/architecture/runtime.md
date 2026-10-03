# Runtime architecture

Devbox separates **desired configuration** from the **recorded environment** it created. The application engine owns transitions between them. Linux locks, process identity, terminal APIs, and exit status are used directly.

## Component layout

All paths below are under `internal/`.

| Package | Owns |
|---|---|
| `cli`, `cliui` | Commands, synchronous workflows, rendering, terminal ownership |
| `app` | Lifecycle ordering, validation, locks, and external effects |
| `resource` | Config-directory creation, edits, optional files, deletion |
| `config` | Schemas, substitution, validation, merges |
| `artifact` | Ordered composition, provenance, captured build contexts |
| `environment` | Identity, desired specification, image plan, input comparison |
| `harness` | Definitions, defaults, declared mounts and capabilities |
| `store` | Records, external locks, leases, transfer journals |
| `filesync` | Managed file and shared-JSON reconciliation |
| `docker` | Typed Docker requests and ownership checks |
| `sshshare` | SSH master processes, publication, revocation |
| `assets` | Installed agent guide, documentation, network-data bundle |
| `commanderror` | Typed failures and suggested next steps |

Commands translate requests; they do not implement lifecycle policy. Docker consumes captured plans rather than reloading config. Harness-specific behavior belongs in declarations, not engine branches.

## Core invariants

- Names locate resources; labels authorize mutations.
- Container/image absence does not erase saved session identity, history or its comparison baseline; only explicit recreation rebuilds an existing session from current selected config.
- Inspection never advances applied state.
- Ordinary startup has one synchronization boundary.
- Long commands use leases rather than holding operation locks throughout execution.
- Records and diagnostics do not contain env/auth values.
- A committed transfer destination must not be overwritten by recopying its source.

## Follow a subsystem

| Topic | Details |
|---|---|
| Create, start, attach, recover | [Lifecycle and applied state](lifecycle.md) |
| Config resolution, images, managed files | [Configuration and images](configuration.md) |
| Identity, locking, delete, transfer | [State and transactions](state.md) |
| Menus, terminal handoff, completion | [Interactive frontend](interactive.md) |
| Authentication and connection lifetime | [SSH sharing](ssh.md) |

## Error ownership and rendering

Owners return `commanderror.Error` with a code, safe message, target, cause, and optional `Step` values. Preserve filesystem absence through wrapping until the owning layer can distinguish it from corrupt state. Use `errors.Is`; never classify failures by display text.

`cli.Execute` renders errors. Human failures go to stderr; JSON-capable commands emit a structured object on stdout. Child streams pass through, and a child/Docker exit status stays authoritative when cleanup also fails. CLI-owned partial-result errors retain confirmed deletions or incomplete usage scans alongside the cause. JSON includes `partial_result` in the error document.

`Step.Reason` labels a shell-quoted command. `Then` expresses a sequence and `Or` an alternative. Suggestions are data, never automatic recovery actions or permission to add Force.

Two target forms must survive presentation:

- Missing-session/config hints retain the spelling entered by the user.
- Known sessions and transfer retries retain exact identities/endpoints.

Carry an explicit home into suggested commands. Display metadata must not become an alternative source of configuration or identity.

## Validation

Unit tests use temporary homes and a command-boundary Docker fake to verify ordering, ownership, redaction, locks, recovery, and partial failures. `make test-fast` runs the suite; `make check` also formats, race-tests, and builds.

Real installer execution, provider login, Docker integration, and host-terminal acceptance are separate checks. Record unpassed gates in `docs/dev/progress.md`, not in user onboarding pages.
