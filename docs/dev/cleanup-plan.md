# Behavior-preserving cleanup plan

## Goal and scope

Clean up `rewrite/` by improving code organization and internal ownership, without changing behavior.

All current behavior must remain unchanged: commands, flags, defaults, output text and ordering, configuration resolution, saved formats, Docker operations, locking, recovery, transfers, and failure handling. This includes existing quirks and bugs; fixes belong in separate work.

This is a proposal, not an implementation record.

## 1. Separate responsibilities in `app/engine.go`

`internal/app/engine.go` currently combines resolution, opening, mount/auth preparation, creation, recovery, and attachment cleanup.

Move existing functions into focused files within the same `app` package:

| File or area | Responsibility |
|---|---|
| `engine.go` | Engine dependencies, shared types, resolution |
| `open.go` | Opening and harness launch sequencing |
| `create.go` | Creation, recreation, materialization, commit cleanup |
| `recovery.go` | Recovery from the recorded environment |
| `attach.go` | Attachment leases and last-command cleanup |
| Mount-preparation files | Store/auth mount planning and preparation |
| Existing `startup.go` | Shared stopped-to-running preparation |

Initially move functions unchanged. Preserve call order, deferred cleanup, lock lifetime, and context handling. Keep related helpers beside their owner; do not create a generic lifecycle framework.

Acceptance: lifecycle paths can be read without navigating unrelated responsibilities. File size alone is not an acceptance criterion.

## 2. Make creation-record construction readable

`createAs` currently constructs the durable record in one large literal.

Extract that construction into a small, side-effect-free helper with explicit inputs. Keep the following actions visible in the orchestration function:

1. Prepare resources.
2. Construct the record.
3. Materialize the container.
4. Save the committed record.
5. Perform existing failure cleanup.

Preserve every field, default, timestamp decision, and serialization detail. Pass the activity timestamp into the helper, evaluating it at the existing record-construction point. Keep ID allocation, creation-time selection, and resource operations outside the helper; do not introduce a clock abstraction. Preserve the distinct identity, activity, action, and manual-start rules for ordinary creation, recreation, and prepared destinations.

Do not introduce new validation, change mutation timing, or alter cleanup as part of the extraction.

Acceptance: assembling the record is clearly separate from Docker mutation and record publication, with unchanged saved state and execution behavior.

## 3. Separate diagnostic reporting from rendering

`Engine.diagnose` currently both collects diagnostics and formats terminal output.

- Keep diagnostic decisions and emission timing in `app`.
- Move rendering of typed `Diagnostic` values to `cli`. Leave `resolutionWarnings` unchanged; this pass does not move all application output.
- Use a small synchronous callback, not an event system. A missing callback means no rendering; diagnostic collection in `Result` still occurs. Do not retain an implicit rendering fallback in `app`.
- Inventory engine callers before changing delivery. Explicitly wire callers that currently display diagnostics, including callers outside the main CLI; preserve intentional silence for callers with nil or discarded error streams. Keep returned diagnostics unchanged.
- Keep child-process streams unchanged.

Preserve exact wording, whitespace, output destinations, command spelling, and output order. Do not bundle quoting improvements, output redesign, or delayed warnings into this change. In particular, warnings currently emitted before startup or harness output must remain immediate; rendering cannot wait for `Open` to return.

Acceptance: lifecycle code owns typed diagnostic facts and timing; presentation code owns their existing display. No caller loses, duplicates, or reorders output, including ordering relative to the unchanged resolution warnings.

## 4. Review the resulting boundaries

After the moves and extractions:

- Remove only helpers and imports made unused by this cleanup.
- Correct comments whose locations or ownership descriptions changed.
- Update architecture documentation to reflect the resulting ownership.
- Check that no new fallback, configuration read, state mutation, or extra Docker call was introduced.
- Keep command-specific contracts explicit. Similar-looking lifecycle paths must not be merged merely because their code resembles each other.

Acceptance: the cleanup improves navigation and ownership without introducing additional behavioral modes or changing existing ones.

## Validation

Use the existing test suite as the baseline. Before each extraction, add focused regression coverage for any unprotected behavior:

- Record construction: ordinary creation, recreation, and prepared destinations, including identity, timestamps, activity, action, manual-start intent, and saved-field parity.
- Diagnostics: exact wording, whitespace, command spelling, output destination, and ordering relative to resolution warnings and child-process output.

Retain existing warning-timing, cancellation, prepared-identity, and failed-commit coverage. When rendering moves, keep app-level tests for immediate callback delivery and returned diagnostics; add CLI tests for exact rendering and callback wiring. Formatting tests must not replace timing tests. Cover the missing-callback contract without weakening caller-output parity.

Do not change expected behavior to accommodate the refactor.

After each pass:

- Review the diff for unintended behavioral changes.
- Run focused tests for affected contracts.
- Run `make test` from `rewrite/`.

Final checks:

- Run race tests, vet, and build.
- Compile integration-tagged tests.
- Review saved-record construction and diagnostic output for parity.
- Record any unrun checks honestly. Integration-test compilation is not live Docker acceptance; live Docker verification remains a separate acceptance gate.

## Explicit exclusions

- Raw-environment-argument bugfixes or any other behavior change.
- Configuration or schema changes.
- Compatibility paths or migrations.
- Request/API redesign.
- New transaction or lifecycle frameworks.
- Transfer-state-machine redesign.
- Package splitting merely to reduce imports.

If implementation exposes a bug or requires a behavior decision, stop that portion of the work and propose it separately. Do not fold it into cleanup.

## Deliverable

A small series of independently reviewable refactoring commits with unchanged observable behavior. Prefer a move-only pass before functional extractions so reviewers can distinguish relocation from logic changes.
