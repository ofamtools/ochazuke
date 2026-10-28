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
  Its horizontal twin on tables is R5.11 (`h`/`l` scroll, then move the
  focus). Story: `motions/spill-down.json`, `motions/spill-up.json`.
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
  label, and a request addressing no instance of the verb's noun reads its
  arguments (`TestConfirmWithoutTargetReadsArguments`). Story: `modals/confirm-yes.json`, `modals/confirm-label.json`;
  needed for the swallow case.
- **R4.6** The backdrop is dimmed (`modal-dim`, default 35 percent toward the
  background plus faint) and the terminal cursor is hidden beneath a
  modal, but the page's highlights (selected row, grid cursor cell) remain
  visible. Story: needed (`modals/backdrop-keeps-highlights`).

### R5. Lists and selection

- **R5.1** Page lists draw no cursor marker; a list's selected row is reverse
  video alone. A page list without the focus keeps its selected row marked and
  is drawn faded as a whole (`dim-unfocused`, default 20 percent toward the
  background; faint in mono), so the focused list's row is the one highlight
  at full strength and the row an unfocused list (a tree beside a live
  terminal) is on stays visible; a tree whose line is being edited is not
  faded. A list in a modal stays plain until focused. A table's highlight
  spans its row; inside it a cell-mode table marks its current cell (R5.10).
  Story: `lists/one-highlight.json`, `lists/unfocused-dimmed.json`,
  `lists/beside-terminal-keeps-row.json`, `lists/lone-list.json`.
  Exception: a card timeline (`AddTimeline`, mtm's Turns) draws its
  selection as the card's border, never reverse video, so a selected
  card's wrapped prompt reads as text: the selected card of the focused
  timeline has a heavy border with heavy corners in the accent; the
  selected card of a timeline without the focus a heavy border with
  rounded corners, halfway from the accent to the border colour; every
  other card the light border, dimmed by `dim-unfocused`. In mono the line
  styles alone tell the three apart; the active card's `●` is a separate
  cue. Story: `timeline/border-focused-*.json`,
  `timeline/border-unfocused-*.json`.
- **R5.2** Marked rows take a `* ` gutter, and every row of that list gets
  the gutter while any mark exists. Story: `lists/marked-gutter.json`.
- **R5.3** Rows are instances of their noun, created and removed by
  `SetItems`; no noun per row; row-specific state goes in `ListItem.Data`.
  Check: the diff.
- **R5.4** Selection survives `SetItems` by stable id. Story: needed
  (`lists/setitems-keeps-selection`).
- **R5.5** `List.Move` (the API) always clamps; only the `list.move`
  operation spills. Check: unit test.
- **R5.6** `/` filters any focused list unless the app binds `/` on the
  stack: typing narrows the rows (label and cells, every word, ignoring
  case, list order), up/down and `ctrl+n`/`ctrl+p` move among the matches
  without spilling, enter keeps the filter in normal mode, esc clears it and
  goes back to the row `/` found, backspace on an empty query leaves the
  line. On a list added with `ClearFilterOnEnter` (one picks a row from,
  such as a pane list) enter with a match chooses it instead: the filter
  clears, the selection stays on the match and stays on screen, the mode is
  normal; with no match enter keeps the filter as on any list. Stories:
  `lists/filter-open.json`, `lists/filter-move.json`,
  `lists/filter-enter-keeps.json`, `lists/filter-enter-clears.json`,
  `lists/filter-esc-clears.json`,
  `lists/filter-backspace-empty-leaves.json`, `tables/filter-by-cell.json`.
- **R5.7** A filter hides rows and never removes them: `SetItems` while
  filtered filters the new rows, marks survive, and `list.apply` fans out
  to the marked rows shown. Story: `lists/filter-marks-survive.json`,
  `catalog/filter-setitems.json`; test `TestListFilterSetItemsMarksAndApply`.
- **R5.8** Every focused list answers `gg`/`G` (first/last row, a count is
  the row number), `ctrl+d`/`ctrl+u` (half a page, a count is rows) and
  `ctrl+f`/`ctrl+b` (a page, a count is pages) in normal mode; a page is the
  list's own window, else the page body; the window moves with the
  selection; they clamp at the list's edges and never spill; a row noun's
  own binding of the key answers first. Story: `lists/page-motions.json`;
  tests `TestListGotoEndsAndCounts`, `TestListPageMotionsClampWithoutSpilling`.
- **R5.9** A table that hides a column scrolls its columns: `z l`/`z h`
  move the view one column (a cell-mode table carries its current column
  along); on a row-select table (the default) `$` shows the last column and
  `^`/`0` (without a count) go back; the pinned columns stay (R5.13); no
  key scrolls without a visible change (an offset that draws what the
  start draws is the start); the keys do not answer (and `z` does not
  wait) on a row-select table that fits or a plain list. Story:
  `tables/column-motions.json`, `tables/row-select.json`; tests
  `TestColumnMotions*`.
- **R5.10** Cell mode is opt-in. A table selects rows unless it is added
  with `CellSelect()`, for a table one reads cell by cell (a JSON or CSV
  view); `h`/`l` stay free for row nouns everywhere else. A cell-mode
  table has a current column: `h`/`l`/arrows move by column (counts; at the
  first and last column they move the focus, R5.11), `j`/`k` keep it,
  `0`/`^`/home and `$`/end go to the ends, `N|` to column N, `g c` chooses
  one; every frame scrolls the view as little as shows it (a column dropped
  by `Priority` included). The selected row keeps the one highlight across
  the whole width and the current cell is reversed and underlined inside
  it; the header underlines the current column's title; an unfocused page
  table fades both (R5.1). `y` copies the cell into the registers;
  `z >`/`z <`/`z =` size the current column (they need one, so they answer
  on cell-mode tables only), and the widths and the column are remembered
  per table across a restart. On every table, cell-mode or not: `Y` copies
  the row's cells joined by tabs, and `V` is a visual row selection drawn
  into the marks (not on a `NoMarks` table): `j`/`k` extend it without
  spilling, a row's verb runs for every selected row with one confirm,
  `esc` ends it and restores the marks. A row-select table (a tree's list,
  where `h`/`l` collapse and expand; help; settings; a cli catalog table)
  and a plain list answer none of the other keys (`h`/`l` scroll a
  row-select table, R5.11); a row-select table remembers only its pins
  (R5.13). `RowSelect()` is a deprecated no-op. A row noun
  binding one of the keys answers first. The movement keys are motions
  (off the footer). Story: `tables/cell-motion.json`,
  `tables/column-count.json`, `tables/column-resize.json`,
  `tables/visual-apply.json` (a row-select table),
  `tables/unfocused-cell-dimmed.json`, `tables/row-select.json`,
  `catalog/groups-table-h.json`; tests `TestCell*`, `TestColumnWidths*`,
  `TestVisual*`.
- **R5.11** `h`/`l` scroll, then move the focus: the horizontal twin of
  R3.4. On a row-select table `l` scrolls the view one column right until
  the last column shows and `h` one column left until the start; on a
  cell-mode table they move the current column in drawing order. At that
  edge (a table that fits is at both) `l` moves the focus to the next stop
  and `h` to the previous one, landing on its remembered child as
  `focus.next`/`focus.previous` do. A count scrolls or moves that many and
  stops at the edge; it spills only from the edge. A `NoSpill` table, a
  table in a modal and the page's first or last stop clamp (R3.5, R3.6).
  A row noun's own `h` or `l` answers first, so the scroll and spill apply
  only where the row noun does not bind the key. A tree's list is never
  such a table: the tree's own `h`/`l` (R11.1) answer even though the
  list's level sits inside the tree's, and `z h`/`z l` scroll a tree's
  columns. Arrows are the same keys.
  Done with guarded rows (`table-column-*`, `table-scrolls-*`,
  `table-spills-*`) that fall through in order, no combined verb. Story:
  `tables/scroll-then-focus-right.json`,
  `tables/scroll-then-focus-left.json`, `tables/cell-edge-focus.json`,
  `tables/nospill-clamps.json`, `tables/modal-clamps.json`,
  `tables/row-noun-l-wins.json`, `tables/row-select.json`; tests
  `TestRowTableHLScrollThenMoveTheFocus`, `TestCellTableEdgeMovesTheFocus`,
  `TestTableEdgesClampWithoutAStop`, `TestRowNounKeyWinsOverTableScroll`.
- **R5.12** A table never hides a column silently: a table with a header
  whose columns do not fit draws `‹N` after its pinned columns and `N›` at
  its right edge, N the scrolling columns hidden on that side (pinned ones
  are never counted), each only when N > 0, so the user sees whether
  `h`/`l` scroll or move on (as R6.8 marks hidden tabs). The cells are
  reserved whenever the table hides a column, so scrolling never shifts the
  pinned columns; a table that fits reserves and draws nothing. Story:
  `tables/edge-markers.json`, `tables/edge-markers-start.json`,
  `tables/edge-markers-fit.json`, `tables/modal-clamps.json`; test
  `TestEdgeMarkersCountScrollingColumns`.
- **R5.13** Any table columns can be pinned: pinned columns draw first at
  the left in their declared order and never scroll; the rest scroll after
  them. The default pins are the columns an app declares `Column.Pinned`,
  else the first column. `list.pin-column`, `unpin-column`,
  `toggle-pin-column` (`z p` on a cell-mode table: the current column) and
  `unpin-columns` change them on any table (palette; a row-select table
  without a `column` chooses one from the column chooser); `z =`
  (`reset-columns`) puts the default back. A user's pins are remembered per
  table across a restart, the default is not written. When the pins leave
  no room for a scrolling column, the view (never the saved pins) drops
  pins from the right until one shows. Story: `tables/pin-cell.json`,
  `tables/pin-row-palette.json`, `tables/pin-declared.json`,
  `tables/pin-narrow.json`, `tables/unpin-columns.json`; tests
  `TestPinsDrawFirstPersistAndReset`, `TestPinsDroppedInTheViewOnly`.
- **R5.14** Colour in a cell is a named tone, never text: a row's
  `ListItem.Tones` (a tree node's `TreeNode.Tones`, a catalog row line's
  `tones`) give a whole cell or a run of runes inside it a tone such as
  `state.working` or `marker.star`, and the theme maps the tone to a colour
  and attributes (bold, dim). The cell's text is unchanged, so a tone never
  counts toward a width, a cut, the filter, a yank or `--format`. An unknown
  tone draws plain; mono and `NO_COLOR` draw a tone's attributes alone; a
  user overrides or adds tones in `themes.json` (configuration, not code).
  In a tree, cell 0's tones stay on the label, after the indent and the
  fold marker. A tone never carries meaning alone: the cell's text says the
  state, the tone only lets the eye find it.
  **Selection:** the selected row stays the one reverse-video bar (R5.1):
  inside it a tone draws its bold alone, never its colour and never its
  dim, so the bar reads the same in every theme and the row's text keeps
  full strength; a cell-mode table's current cell keeps its underline over
  a tone. The colour comes back as soon as the row is left. A page list
  without the focus fades its tones with it (`dim-unfocused`). Stories:
  `tables/tones-table.json`, `tables/tones-selected-row.json`,
  `tables/tones-mono.json`, `tree/tones-tree-row.json`,
  `chrome/tones-themes-override.json`, `chrome/tones-user-scheme.json`,
  `catalog/tones-rows.json`; tests `TestTonesNeverChangeTheText`,
  `TestSelectedRowDrawsToneAttributesOnly`, `TestUnfocusedListFadesTones`.

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
  `Notices` persist until `notice.clear`; the footer shows the newest at
  most half the width with `(+N more)`, so the hints always keep the rest.
  Story: `notices/persist.json`, `notices/footer-keeps-hints.json`;
  needed for the status-clears case.
- **R6.5** The jobs ticker rides the rule line above the footer: at most
  two entries joined by ` · `, the rest of the line still rule characters;
  it takes no row, so the body never shrinks, moves or loses a row while a
  job runs. Running newest first, finished after, a finished entry lingers
  four seconds, quiet verbs hidden, no jobs the plain rule. Story: the
  stories under `ticker/` (`ticker/keeps-body.json` pins every body row in
  place, the bottom one included).
- **R6.6** One chrome: everything renders through `Layout.Shell`; no second
  set of height or padding constants. Check: the diff.
- **R6.7** Help wraps a long cell inside its column rather than overflowing
  the modal width. Golden: `testdata/help-80.golden`.
- **R6.8** The tab row never drops a page silently: when the tabs do not
  fit, `‹N` at the left edge and `N›` at the right count the pages hidden on
  each side, the active tab is always on the row, and the shown tabs plus
  the counts are every page; with every tab fitting there are no markers.
  Story: `pages/tabs-{50,80,120}-{start,middle,end}.json`,
  `pages/tabs-fit.json`, `pages/tabs-50-step.json`; unit test
  `TestTabsMarkHiddenPagesOnEachSide`.

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
- **R7.6** A chooser is not the palette: its search input opens in insert,
  the top match is highlighted from the first frame and after every edit,
  enter takes the highlighted row, `ctrl+n`/`ctrl+p` move it clamped (up
  from no highlight stays), esc closes over the focus and mode it opened on.
  Every page is reachable from the page picker (`'`, `page.pick`): pages
  visited before, most recent first, then the never-visited in
  registration order, the current page last and marked `current`, so enter
  goes back to the page last left. Story: `picker/page-picker-filter-open.json`,
  `picker/page-picker-recent-first.json`,
  `picker/page-picker-esc.json`, `picker/page-picker-move.json`,
  `picker/page-picker-footer.json`; ranking in `chooser_test.go`. `'`
  answers only with no modal open (a page cannot open beneath one):
  `picker/page-picker-not-in-modal.json`.

### R8. Terminal

- **R8.1** `any`/`paste` rows forward everything to the pane in terminal
  mode. Story: `terminal/ctrl-c-reaches-pane.json`.
- **R8.2** `ctrl+a` enters `leader_pending`; `d` detaches, `ctrl+a` sends
  itself, an unbound key sends leader and key in one `Send`, the timeout
  sends the leader alone. The leader is the `terminal-leader` setting;
  its timeout is `terminal-leader-timeout` (default the key-sequence
  timeout), and `0` means the leader never expires: it waits for the next
  key, with the same footer hint. Other sequences keep their timeout.
  Stories: `terminal/detach-focus-returns`, `leader-twice-sends-one`,
  `leader-unbound-forwards-both`, `leader-timeout-sends-leader`,
  `leader-setting-footer`, `leader-setting-detach`,
  `leader-timeout-zero-waits`.
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
  one highlighted row; `h`/`l` collapse and expand. On a tree with columns
  too: `l` on an open node steps onto its first child, `right` on a leaf
  stays in the tree, and the table scroll and spill rows (R5.11) never
  answer for a tree's list; `z h`/`z l` scroll its columns. Story:
  `tree/expand-collapse.json`, `tree/spill-to-list.json`,
  `tree/columns-l-expands.json`, `tree/columns-hidden-l-expands.json`;
  test `TestTreeHLWinOverTableScroll`.
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
