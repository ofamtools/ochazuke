# UX rules: the interaction checklist

What a reviewer checks an interaction change against, and what an author
checks before asking for review. Three tiers. Each finding in a review cites
one entry here by id, or is labelled a judgment.

| Tier | Id prefix | What it is | Highest severity | Blocks a round |
|---|---|---|---|---|
| Rule | `R` | A condition that is true or false, with a story or test that decides | blocker | yes |
| Principle | `P` | A heuristic stated as a sentence with examples; applying it takes reasoning | major | no |
| Judgment | `J` | The reviewer thinks it is worse and no entry covers it | suggestion | no |

Severities: `blocker`, `major`, `minor`, `suggestion`. A rule finding is a
blocker when the story fails, minor when the story passes but the code reaches
the result the wrong way (a literal where a row should be). A principle
finding is major at most and carries its reasoning in one or two sentences. A
judgment never blocks; the applier may skip it with a one-word reason, and
disagreements go to the owner, not back into the loop.

A story path under `cmd/o-story/stories/` is the executable form of a rule.
`needed` means no story covers it yet; a finding under such a rule must add
the story (`"skip": "gap: …"` if it stays red). The source of the long form
is `AGENTS.md`'s invariants; this file restates them as checks and adds the
tiers above them. When the two disagree, fix one in the same commit.

---

## Part A. Rules

### R1. Keys are data

- **R1.1** Every key the user presses dispatches a registered `noun.verb`
  through one stack walk. No function compares a key name outside the
  classifier and `keyTexts` in `noun.go`.
  Check: `TestBindingRowsAreAtomic`, which parses `resolve.go`.
- **R1.2** A binding row is a `Request`, `Enter`/`Leave`, or `null`. No Go
  code in a row. Check: `ValidateBindings` on every table.
- **R1.3** Composite behaviour is an operation calling operations through
  `Execute`, never a branch on the raw key or mode name.
  Check: read the diff; no story can see this.
- **R1.4** Escape cancelling a key-sequence prefix is a generated
  `"<prefix> esc"` → `mode.leave` row. Story: needed
  (`keys/esc-cancels-prefix`).
- **R1.5** Function keys F1 to F12 are never bound anywhere, demos and
  stories included. Check: grep the binding JSON and stories for `"f[0-9]`.
- **R1.6** Resolution order at a level: instance rows above noun rows; mode
  table before any-mode table; exact key, then key class, then `any`.
  Story: needed (`keys/instance-over-noun`, `keys/exact-over-class`).
- **R1.7** A key sequence waits `DefaultBindingTimeout` when a longer
  sequence extends it at any level, and the innermost exact match runs on
  timeout or mismatch. Story: needed (`keys/sequence-timeout-fallback`).
- **R1.8** A row whose verb guard is false at the focus does not answer and
  does not appear in `bindings.list`. Story: needed (`keys/guard-hides-row`).
- **R1.9** A paste reaches the walk as one `MacroKey` with `Paste` set and
  matches only `paste` rows. Story: needed (`inputs/paste-is-one-key`).

### R2. Modes

- **R2.1** The mode is one global value, `Runtime.Mode {Name, Payload}`.
  Instances hold no mode. Check: `TestRuntimeHoldsNoComponentFields` and the
  diff.
- **R2.2** `Input.Mode` changes only through `Input.SetMode`. Check: grep for
  `.Mode =` outside `input.go`.
- **R2.3** A pending mode is exclusive: only its table answers, no any-mode
  rows. Story: needed (`modes/pending-is-exclusive`).
- **R2.4** A one-shot pending mode is consumed by the answering key before
  its request runs, except when the request is `mode.enter`, `count.append`,
  `mode.leave` or `input.cancel`. Story: needed
  (`modes/one-shot-consumed-first`).
- **R2.5** Leaving a pending mode returns to its base (`Payload.Return`),
  never to a hard-coded `normal`. Story: needed (`modes/leave-returns-to-base`).
- **R2.6** Unknown mode names are errors, not treated as normal or insert.
  Check: unit test on `SetMode` with a garbage name.
- **R2.7** `prompt` pins insert and `terminal` pins terminal; focusing an
  instance of such a noun enters the pinned mode. Story:
  `inputs/lone-input.json` covers insert and escape for a plain input;
  needed for prompt and terminal pins.
- **R2.8** A passthrough mode has no capture pass: while `terminal` is the
  base mode, the app's captured `ctrl+c` does not fire. Story:
  `terminal/ctrl-c-reaches-pane.json`.
- **R2.9** Editor rows answer only when an input is on the focus path; a list
  row or page focus ends at `app`. Counts, registers and macros are `app`
  rows and still answer. Story: needed (`modes/editor-rows-need-input`).

### R3. Focus and motion

- **R3.1** Focus moves only through the runtime: `SetFocus`, `focus.*`,
  `Push`/`Pop`, `OpenPage`, `RemoveInstance`, `RetargetFocus`. No component
  assigns `Runtime.Focus`. Check: grep for `.Focus =` outside `focus.go`.
- **R3.2** A focus change that leaves the current stop resets the mode to
  `normal` with an empty payload, or to the new noun's pinned mode. A move
  inside one stop keeps it. `Pop` and `page.open` restore the mode they left.
  Story: needed (`focus/leaving-stop-resets-mode`,
  `focus/inside-stop-keeps-mode`).
- **R3.3** Stop order is the page's recorded drawing order, never
  registration order. Story: `motions/drawing-order.json`,
  `pages/tab-order.json`.
- **R3.4** `j`/`k` past the edge of a focused page list spills to the
  neighbouring stop (next stop's first row, previous stop's last row).
  Story: `motions/spill-down.json`, `motions/spill-up.json`.
- **R3.5** Spill never wraps at the page's first or last stop. Story:
  `motions/no-wrap.json`.
- **R3.6** A `NoSpill` list and any list inside a modal clamp instead of
  spilling. Story: `motions/nospill.json`; needed for the modal case.
- **R3.7** Spilling into an empty list lands on the list itself. Story:
  `motions/empty-list.json`.
- **R3.8** `}`/`{`/`tab`/`shift+tab` jump stops, `)`/`(` jump regions, on the
  `app` noun in normal mode only. Story: `pages/tab-order.json`,
  `pages/ctrl-j-k.json`; needed for the regions case.
- **R3.9** A jump lands on the stop's remembered child, not its first.
  Story: needed (`focus/jump-lands-on-remembered`).
- **R3.10** The page scrolls to keep the focus visible with scrolloff 2, and
  a self-windowing list keeps the same margin inside its window. Story:
  `scroll/follow-scrolloff.json`, `scroll/list-window-scrolloff.json`,
  `scroll/tall-list.json`.
- **R3.11** `pgup`/`pgdn` move the surface only; the next `j` moves from
  where the focus was, not from the new viewport. Story:
  `scroll/pgdown-then-j.json`.
- **R3.12** Removing the focused instance sends the focus to `FocusOnClose`'s
  target when it survives, else the parent, else Pop's return. Story:
  `terminal/detach-focus-returns.json`; needed for the plain remove case.

### R4. Modals

- **R4.1** While a modal is open, `Runtime.Focus` is the top modal or a
  descendant. `focus.set` outside it is refused with an error result.
  Check: focus tests assert `focusInTopLevel`.
- **R4.2** The top modal is a boundary: the walk stops at the modal instance,
  then reads `modal` and `app`. Rows beneath never answer. Story: needed
  (`modals/keys-do-not-reach-page`).
- **R4.3** Closing a modal restores the focus and mode it opened over; when
  that focus is gone, the nearest surviving ancestor. Story: needed
  (`modals/pop-restores-focus-and-mode`).
- **R4.4** A second modal opens only from inside the top one; otherwise the
  open is refused with an ordinary error, first wins, no queue. Story:
  needed (`modals/refuse-from-beneath`).
- **R4.5** A confirm modal: `y` runs the stored request, `n`/`esc` pop, every
  other key is swallowed, `{{label}}` reads the argument before the target's
  label. Story: `modals/confirm-yes.json`, `modals/confirm-label.json`;
  needed for the swallow case.
- **R4.6** The backdrop is dimmed and the terminal cursor is hidden beneath a
  modal, but the page's highlights (selected row, grid cursor cell) remain
  visible. Story: needed (`modals/backdrop-keeps-highlights`).

### R5. Lists and selection

- **R5.1** Page lists draw no cursor marker; the focused list's selected row
  is reverse video alone; an unfocused list draws every row plain. Story:
  `lists/one-highlight.json`, `lists/unfocused-plain.json`,
  `lists/lone-list.json`.
- **R5.2** Marked rows take a `* ` gutter, and every row of that list gets
  the gutter while any mark exists. Story: `lists/marked-gutter.json`.
- **R5.3** Rows are instances of their noun, created and removed by
  `SetItems`; no noun per row; row-specific state goes in `ListItem.Data`.
  Check: the diff.
- **R5.4** Selection survives `SetItems` by stable id. Story: needed
  (`lists/setitems-keeps-selection`).
- **R5.5** `List.Move` (the API) always clamps; only the `list.move`
  operation spills. Check: unit test.

### R6. Chrome: footer, hints, help, ticker, notices

- **R6.1** Every hint line is `BindingHints`/`ListBindings` output for the
  focused instance and mode. No literal hint string exists in framework
  code. Check: grep `model.go`, `terminal.go`, `picker*.go` for quoted
  key names in view code.
- **R6.1a** The footer leaves out motions (`Operation.Motion`) and
  `Hidden` rows, orders hints innermost level outward, and shows one key per
  request; `?` still lists every row. Story: `chrome/footer-no-motions.json`.
- **R6.2** A row's `Hint` replaces the `noun.verb` text in the footer;
  descriptions appear only in help and `bindings.list`. Story: needed
  (`chrome/hint-replaces-operation`).
- **R6.3** `?` opens the help modal over the focus, grouped by the answering
  level's noun innermost first under a section label, app rows after the
  levels, motions last under `movement`, and only in normal mode.
  Story: `help/row-verbs-first.json`, `help/levels-innermost-first.json`;
  golden `testdata/help-80.golden`.
- **R6.4** `Status` shows the last key's error and clears on the next key;
  `Notices` persist until `notice.clear`. Story: `notices/persist.json`;
  needed for the status-clears case.
- **R6.5** The jobs ticker: at most two rows, running newest first on top,
  finished below, a finished row lingers four seconds, quiet verbs hidden,
  no jobs no rows. Story: the eight stories under `ticker/`.
- **R6.6** One chrome: everything renders through `Layout.Shell`; no second
  set of height or padding constants. Check: the diff.
- **R6.7** Help wraps a long cell inside its column rather than overflowing
  the modal width. Golden: `testdata/help-80.golden`.

### R7. Palette

- **R7.1** Nothing is highlighted and no `> ` marker is drawn until
  `ctrl+n`/`ctrl+p`. Story: `palette/no-highlight.json`.
- **R7.2** Enter runs the typed line unless a word is highlighted, in which
  case it appends the word. Story: `palette/enter-runs-line.json`.
- **R7.3** Words resolve left to right as unique prefixes; an incomplete
  line names its first missing word rather than guessing. Story: needed
  (`palette/names-missing-word`).
- **R7.4** An omitted noun resolves to the nearest instance on the origin's
  focus path that has the verb. Story: needed (`palette/noun-from-focus-path`).
- **R7.5** Up and down recall history; requests a line ran are recorded with
  source `palette`. Story: needed (`palette/history-recall`).

### R8. Terminal

- **R8.1** `any`/`paste` rows forward everything to the pane in terminal
  mode. Story: `terminal/ctrl-c-reaches-pane.json`.
- **R8.2** `ctrl+a` enters `leader_pending`; `d` detaches, `ctrl+a` sends
  itself, an unbound key sends leader and key in one `Send`, the timeout
  sends the leader alone. Story: needed (`terminal/leader-*`, one per case).
- **R8.3** Frames reach the component only through `Deliver` with a current
  ticket; a frame for a closed terminal is dropped. Story:
  `terminal/output-ticks.json`.
- **R8.4** Every close path releases the stream exactly once. Check: unit
  test with a counting `TerminalReleaser`.

### R9. Inputs and the editor

- **R9.1** Readline and Vim rows are `editor` noun defaults from
  `editor_bindings.json`; an app overrides by binding on a noun or instance
  on the stack or in a binding file; it never edits the JSON. Check: the
  diff.
- **R9.2** Offsets count runes; rendering accounts for cell width. Story:
  needed (`inputs/wide-runes`), with a CJK or emoji string.
- **R9.3** Undo and redo round-trip every editing operation: for each
  `input.*` verb that changes text, `undo` restores the prior text and
  cursor. Check: table-driven unit test.
- **R9.4** A count applies to every motion and operator that Vim gives one
  to, and an out-of-range count clamps silently. Story: needed
  (`editor/count-clamps`).
- **R9.5** Dot-repeat replays the last `Repeatable` operation with its count
  and register. Story: needed (`editor/dot-repeat`).

### R10. Delivery and jobs as the user sees them

- **R10.1** Application work never blocks the loop; a `Pending` result's
  completion lands on its owner or is dropped if the owner closed. Check:
  the diff and the delivery tests.
- **R10.2** A newer job in the same slot cancels the older; the stale
  completion never paints. Story: needed (`jobs/slot-replaces`).
- **R10.3** Confirmation is a property of the verb, opened at the focus, and
  identical from a key, the palette and the CLI. Story:
  `modals/confirm-yes.json`; needed for the palette and CLI paths.

### R11. Trees and outlines

- **R11.1** A tree's rows are a list: j/k move and spill in drawing order,
  one highlighted row; `h`/`l` collapse and expand. Story:
  `tree/expand-collapse.json`, `tree/spill-to-list.json`.
- **R11.2** Every structural edit of an outline is written before the next
  key; typing is written after the pause and when the line is left; esc
  never discards. Story: `tree/outline-add-nest.json`, `tree/debounce.json`.
- **R11.3** Undo and redo are written changes. Story: `tree/undo-redo.json`.
- **R11.4** Pasted indented text nests. Story: `tree/paste-nests.json`.
- **R11.5** A failed write keeps the text, marks the row and retries on the
  next change. Story: `tree/failed-write.json`.

---

## Part B. Principles

Each principle is a sentence, what it is protecting, and examples of a
violation. A principle finding names the principle, gives a keystroke walk,
and states the reasoning in one or two sentences. Where it helps, it names
the reference behaviour (Vim, Helix, fzf, lazygit) it is comparing to.

- **P1. Motions are visual.** `j`, `k`, `}`, `{`, `tab` move across the
  screen as drawn; only commands jump. Protects the user's spatial model.
  Violation: a motion that lands somewhere the eye did not go, such as a
  list that is drawn second receiving focus first, or a motion that scrolls
  the viewport without moving the focus.
- **P2. Pending state is visible.** Whenever the next key will be read
  differently from a plain normal-mode key, the screen says so. Protects
  against silent misfires. Violation: `operator_pending` with a count and an
  unchanged footer; a key-sequence prefix waiting on the timer with no sign;
  macro recording with no indicator. Reference: Vim's showcmd and recording
  line, Helix's mode name.
- **P3. The screen predicts the key.** A user looking at the footer and the
  highlight can predict what the next key does. Protects discoverability.
  Violation: a verb reachable only from `?`; a hint that names an operation
  the guard currently hides; two stops that look the same but answer
  different nouns.
- **P4. Escape always goes up one level and never destroys.** From any
  pending mode, modal, prefix or insert, `esc` returns to the enclosing
  state and loses only the pending part. Protects panic recovery.
  Violation: `esc` in a prompt discarding the typed text when a second
  `esc` would have been the natural discard; `esc` closing two modals; `esc`
  doing nothing in a state that is clearly nested.
- **P5. One thing stands out.** On any frame there is exactly one place the
  eye is drawn to as "where I am": the focused list's row, the input's
  cursor, the grid's cell, the top modal. Protects orientation. Violation:
  two highlighted rows; a highlight under a modal brighter than the modal;
  a focused empty list with no visible sign of focus.
- **P6. Errors stay in the footer and stay short.** A failed key puts one
  line in `Status`; it never opens a modal, never shifts the body, and is
  gone on the next key. Persistent facts go to `Notices`. Protects flow.
  Violation: an error that reflows the page; a stale status that survives a
  successful key; a notice used for a one-off failure.
- **P7. Counts, registers and dot behave as Vim users expect.** Where a
  Vim user would type a count or a register prefix, it works, and where Vim
  would clamp or ignore, so does this. Protects muscle memory. Violation:
  `3j` at the bottom of a list beeping and keeping the count; `"ayy` in a
  list row doing nothing while `yy` works; `.` repeating a motion.
- **P8. Edges are quiet.** At the first or last row, the start or end of a
  line, the top or bottom of a page, a motion that cannot move does
  nothing visible and leaves no error. Protects rhythm. Violation: a status
  line reading "already at end"; a flash; a focus bounce.
- **P9. Modals ask one question.** A modal has one input or one list and one
  way to say yes and one to say no. Protects against form-like dialogs in a
  keyboard UI. Violation: a confirm with three choices; a prompt that also
  needs a pick list; a modal that opens another modal on enter by default.
- **P10. The same verb reads the same from every surface.** A key, the
  palette line, `?`, `bindings list` and the CLI all show `noun.verb`
  spelled the same way and run the same handler with the same confirm.
  Protects transfer between surfaces. Violation: a palette-only verb; a CLI
  flag that a key cannot express; a hint that abbreviates the verb.
- **P11. Prefer rows over code even when it is longer.** A feature that
  could be a branch in Go and could be a row with a guard is a row.
  Protects the property that `bindings list` is the truth. Violation: a
  verb that checks the mode name inside its handler to do two things.
- **P12. Latency is part of the design.** Anything over about 50 ms between
  key and frame is a finding; anything the user must wait for shows as a
  running job on the ticker. Protects the feel of a native editor.
  Violation: a `list.move` that reloads rows synchronously; a palette
  completion that queries a daemon on the loop.

---

## Part C. Judgment

A judgment finding is allowed and expected. It is how the two parts above
grow. Rules for recording one:

- Label it `J` with a short slug, no number. Severity `suggestion`.
- Give the keystroke walk and what the reviewer saw, then one sentence on
  why it is worse, and the reference behaviour if there is one.
- Propose, in one line, the rule or principle it would become if the owner
  agrees. A judgment without a proposal is a remark, and goes in the review's
  "notes" section instead.
- The applier may skip it with one word (`skip: scope`, `skip: disagree`,
  `skip: later`). The reviewer does not reopen it in the next round.

Promotion: when the owner accepts a judgment, or the same judgment appears
in two reviews, it is added above as a principle (if it needs reasoning to
apply) or a rule (if a story can decide it), in the same commit as the fix.

---

## Appendix. Review format

```markdown
# Punch list: <change>   Reviewer: <agent>   Round: <n>   Rules: docs/UX-RULES.md <commit>

## F<n>  <severity>  <R|P>x.y <name>        (or: J <slug>)
Walk:      <keys and what the screen showed, one line per key where it matters>
Where:     <file:line>
Expected:  <one line>
Observed:  <one line>
Story:     <path>  (passes | fails | new, fails | needed: <path>)
Reasoning: <principle and judgment findings only, one or two sentences>
Proposed:  <judgment findings only: the rule it would become>
Fix hint:  <optional, one line>

## Not findings (checked, fine)
- <rule id>: <one line>

## Notes
- <remarks that are not findings>
```

The applier replies with the same ids, one line each: `fixed`, `declined
<reason>`, or `skip: <word>`, followed by the check output (`go test -race
./...`, `o-story run`, `gofmt -l .`) and the list of files touched. A round
closes when every blocker is `fixed` or overruled by the owner. Three rounds
at most; what remains open is reported, not looped on.
