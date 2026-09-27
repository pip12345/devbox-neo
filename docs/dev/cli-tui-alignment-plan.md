# CLI/TUI alignment and editing UX

Status: implemented. See [progress](progress.md) for validation and outstanding acceptance checks.

Related: [interactive frontend](interactive-frontend-plan.md). Architecture owners are documented in `docs/src/architecture/interactive.md`, `configuration.md`, and `runtime.md` (installed under `/devbox/docs/architecture/`).

## Goal and scope

Make session configuration selection and inspection available without menu navigation, improve text editing, and keep command/menu outcomes consistent. Preserve the existing lifecycle services and synchronous frontend architecture.

Parity means users can accomplish the task through ordinary command-line tools. It does not require a Devbox command for every file edit: configs are user-editable files, and editors such as `vi` already serve that purpose.

Included:

- Direct replacement of an existing session's ordered config selection.
- A nonempty saved-selection rule shared by CLI and TUI.
- Standalone config inspection and usage queries.
- A redesigned shared text-input contract.
- Consistent partial deletion reporting and home-aware guidance.
- Small shared validation/rendering extractions and parity tests.

Excluded:

- `config set`, `config unset`, or other config-field mutation commands. Existing harness/artifact setup flags remain.
- `--clear-configs`, append/remove/reorder flags, or an empty-string sentinel for clearing a saved selection.
- Changes to deliberate full inventory reloads, caching, polling, or Docker-unavailable behavior. Do not expand offline operation.
- A generated CLI/TUI command registry, new router/event bus, or replacement lifecycle architecture.
- Migrations, compatibility shims, command removals, or new runtime fallback behavior.

## Architecture decision

Keep `app` and `resource` as the owners of validation, locking, persistence, and lifecycle policy. Both frontends already call these services; duplicating them would make alignment worse.

A targeted redesign is necessary in two places:

1. **Text input:** its shared contract must represent an initial/pending value and preserve it on validation failure. Per-caller redraw workarounds are not sufficient.
2. **Result presentation:** a command can return both completed work and an error. The CLI boundary must preserve both rather than discarding the result.

Keep synchronous workflows, immutable display snapshots, a single terminal reader, foreground handoff, and intentional full reloads. Share operation meaning, not necessarily navigation or confirmation mechanics.

## 1. Replace a session's selected configs directly

Extend `edit`:

```sh
devbox-neo edit . --name work --config base --config ./project-config
devbox-neo edit <full-session-name> --config base --json
```

### Command contract

- One or more `--config` values replace the **entire ordered selection**. They never append to the saved list.
- Preserve flag occurrence order and exact reference values; do not introduce comma splitting.
- No `--config` leaves the existing command behavior intact. It does not request an empty replacement; an ordinary edit invocation still opens the editor.
- Require `--name` or an exact full session name for direct replacement, matching the existing explicit-edit targeting convention. Do not silently select a folder default for this mutation.
- Reject combinations with `--show`, `--default`, or `--clear-default`.
- `--json` is supported for direct replacement as well as the existing inspection operation, and never prompts.
- An empty reference is invalid. There is no clearing sentinel or separate clearing flag.
- Use the existing name/path capture rules. Relative arguments are interpreted from the invoking directory and saved with the existing workspace-relative intent.
- Save desired references only. Do not create configs, start/recreate containers, or change folder defaults.
- Human help and receipts explicitly say that the selected configs are replaced. Report the resulting ordered selection and point to exact-session Status, preserving an explicit home.
- JSON reports the target and resulting selection without serializing the whole internal session record.

### Shared owner

Use `app.UpdateSources` for persistence. Share the existing reference-capture logic rather than reimplementing path interpretation in the command handler.

Retain identity and source-snapshot comparison under the operation lock. Same-selection conflicts fail without automatic retry; unrelated activity/applied-state fields remain intact. Make conflict guidance suitable for both command and menu callers rather than assuming a menu was open.

### Nonempty saved-selection rule

A successful saved selection must contain at least one config, in both interfaces.

- CLI replacement requires at least one reference.
- In the saved-session TUI editor, block removal of the final config with guidance to replace it instead.
- Keep Replace available: switching configurations does not need an intermediate empty saved state.
- Creation drafts may start empty and may become empty while being edited. Submission still requires at least one config.
- Enforce nonemptiness in the saved-selection operation, not globally in the structural reference validator used by drafts.

This deliberately removes the existing ability to save an empty selection. An empty selection cannot produce runnable configuration, and whole-list replacement covers switching without adding a clearing mode.

Keep the restriction narrow: **nonempty is not the same as runnable**. Retain structural/duplicate checks and the existing repair contract. Do not require full runtime resolution or reject saved-record reads because their current selection is empty or broken. Adding a valid selection must remain possible without changing the reader/schema or introducing migration behavior.

## 2. Inspect a config and its users

Add:

```sh
devbox-neo config show <name|path> [--json]
devbox-neo config users <name|path> [--json]
```

### `config show`

- Show one config over built-in defaults, with resolver-provided provenance and existing redaction.
- Do not substitute the combined configuration of an arbitrary session using that config.
- Reuse `resource.ShowOwner`, `ConfigView`, and the existing config-view renderer.
- Leave combined session inspection under `edit <target> --show`.

### `config users`

- Report saved sessions using the directory through the existing desired/committed-source usage rules.
- Accept the same named/path references as other inspection operations.
- Reuse `resource.ConfigUsers`; do not add a second usage scan or resolver.
- Preserve known users when a scan is incomplete, report the incompleteness, and exit nonzero. An incomplete scan must not look like an authoritative empty result.
- Follow the partial-outcome presentation rule below when returning known data alongside an error.

Update help and read-only completion for both commands. Preserve existing completion rules: no initialization, locks, or mutations solely to supply suggestions.

## 3. Redesign shared text entry

Current evidence: `cliui.Text` and `terminalText` have no initial-value parameter, and `terminalModel.load` creates a fresh text input. A validation retry therefore loses the rejected text. Existing form-draft retention does not solve this smaller interaction problem.

### Shared contract

Replace the input contract with a request that carries:

- Prompt and relevant field context.
- Initial value.
- Validation.
- Sensitive-input behavior.

The workflow owns the pending value and any persistence attempt. The UI owns text editing and cursor behavior. Do not move resource mutations into the rendering goroutine.

### Interaction rules

| Event | Behavior |
|---|---|
| Open an existing text value | Prefill the actual source value, not its effective/redacted display value |
| Invalid submission | Keep the entered text and show the validation error |
| Esc/cancel | Discard this pending edit; leave saved state unchanged |
| Successful config submission | Save immediately, as today |
| Same-field concurrency conflict | Report the conflict, reload current state, and require a fresh explicit submission |
| Write failure | Keep the pending value visibly unsaved; permit explicit retry or cancellation |
| Sensitive input | Mask terminal input and never expose saved values in diagnostics or receipts |

Apply the contract to config scalars/list entries, creation names and paths, transfer/SSH destinations, and invocation arguments where applicable.

Preserve exact argument semantics, including empty arguments; blank text is not an implicit instruction to keep the previous value. Initial values must come from raw source data so editing does not flatten host expressions.

Keep immediate config editing, creation drafts, and independently created configs as their existing distinct save models. This work does not add whole-editor Save/Discard screens or automatic retries.

Update all affected shared-runner callers while preserving their workflow and persistence behavior. Preserve plain-prompt cancellation/EOF rules, deliberately define initial-value presentation there, and prevent sensitive defaults or terminal echo from exposing saved values. Do not retain the old API through a compatibility wrapper.

Use typed validation/conflict distinctions where the workflow needs different retry behavior; never classify errors by their display text. No owner lock may remain held while waiting for corrected input.

## 4. Preserve partial outcomes at the command boundary

Current evidence: `app.Delete` returns a `DeleteResult` together with an error. The TUI prints that result, but the direct command returns the error before rendering it.

### Output contract

- Human deletion output reports confirmed completed deletions and retained state, then reports the error.
- JSON emits one document, not a success document followed by an error document.
- Keep the existing error fields and add `partial_result` when a failure accompanies known completed work. Use the existing deletion-result shape within that field.
- A failed dry run must not report planned resources as completed deletions.
- Failed/incomplete commands retain a nonzero exit status and their underlying error causes.
- An incomplete `config users` query likewise retains its known result alongside its scan error; mark it incomplete rather than implying that any mutation occurred.

Keep the result/error carrier and output handling in `cli`. Services continue returning typed results and errors. `cliui` must not depend on deletion/session types, and `commanderror` must not become a persistence or transaction framework.

Reuse the deletion-result renderer in both entry points. Preserve intentional differences in confirmation flow, all scope/force semantics, locks, idle checks, and deletion safeguards. Do not change unrelated commands' output formats as part of this work.

## 5. Consolidate demonstrated drift

Make small extractions alongside the behavior they own:

- One log-tail validator used by flags and forms.
- One network-export renderer used by direct output and menu views.
- One structured config-save receipt builder.
- Existing `commanderror.Step`/scoped rendering for suggested commands, including an explicit `--home`.

Replace the browser's handwritten `Run devbox-neo status` receipt. Do not create a miscellaneous shared package or force different interactive workflows into a generic command runner.

## 6. Acceptance tests

Keep the capability matrix in `internal/cli/capabilities_test.go`, with outcome tests alongside it. Config-file editing remains an ordinary editor task, not a missing Devbox field command.

### Session selection

- Direct replacement produces exactly the requested ordered chain, not an appended chain.
- Existing command behavior is unchanged without replacement flags.
- Invalid combinations and empty references fail without mutation.
- CLI and TUI reject an empty saved replacement through the service owner.
- The TUI blocks removal of the final saved config but permits replacement.
- Empty creation drafts remain editable; empty submission remains blocked.
- Relative/fixed references, duplicate aliases, stale session identities, and concurrent selection changes retain their existing rules.
- Broken/empty current selections remain repairable; unrelated record fields are preserved.
- Saving does not apply container changes or select a default.

### Inspection and outcomes

- Config show matches the dashboard's source/default/provenance interpretation and redacts sensitive values.
- Config users preserves known users and reports scan failures without implying completeness.
- Inject a deletion failure after actual progress; verify both interfaces retain the completed-work summary.
- JSON is a single decodable document with correct partial data and nonzero failure status.
- Explicit-home receipts/suggestions point to the correct installation and exact session where applicable.

### Text input and terminal behavior

- Existing values are prefilled and editable.
- Validation failures preserve input; cancellation never saves it.
- Write retries are explicit; conflicts never replay an edit automatically.
- Expressions and exact argv values survive editing.
- Sensitive defaults/input do not leak into terminal captures, errors, or receipts.
- Plain prompts, EOF, terminal cancellation, and foreground reader handoff remain correct.

### Test structure

Prefer stable action identifiers or semantic selection for workflow tests over hardcoded menu positions. Keep separate presentation/PTY tests for actual labels, keyboard behavior, filtering, and layouts. Any action identifiers are UI test/dispatch metadata, not a registry generating the CLI and TUI.

Use equivalent operation fixtures to compare CLI/TUI saved state and failure outcomes. Continue testing service contracts independently of presentation.

## Delivery and documentation

1. Add direct session-config replacement and enforce the nonempty saved-selection rule.
2. Add config inspection/usage commands and shared partial-outcome reporting.
3. Consolidate guidance, log validation, and network-export rendering.
4. Redesign shared text input and update its callers without changing their save models.
5. Complete the capability matrix, regression tests, help, completion, and documentation.

Update relevant guides and command/configuration/output references, plus configuration, interactive, and runtime architecture pages. In particular, revise the current statement that rejected edits are discarded: validation failures retain pending text, while concurrency conflicts reload authoritative state without replay. Update the earlier frontend plan where its unchanged-edit-flags statement is superseded. Record implementation and acceptance status in `progress.md` when work is delivered, not as already complete in this plan.

Implementation validation: `make test-fast`, focused race tests for affected CLI/UI code, and the CLI build. Use PTY tests and manual host-terminal acceptance for editing/cancellation/output behavior. Report live Docker and manual acceptance separately from automated tests.

Record validation results in [progress](progress.md).
