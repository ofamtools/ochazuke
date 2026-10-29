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
- **R1.6** A default key is written once, as data: the framework's in its
  embedded files (`bindings/*.json`, `markdown/`, `grid/`, `cli/
  bindings.json`), an app's in its own, loaded with `LoadDefaultBindings`.
  Go `Bind` is for rows computed at runtime (an instance's rows, a row
  built from a spec). Check: `TestFrameworkBindingFilesPassTheRuleCheck`,
  `TestFrameworkBindingFilesDoNotClash`, `CheckDefaultBindings` in the
  app's tests; `bindings defaults` shows each row's source.
- **R1.5** Function keys F1 to F12 are never bound anywhere, demos and
  stories included. Check: grep the binding JSON and stories for `"f[0-9]`.
- **R1.6** Resolution order at a level: instance rows above noun rows; mode
  table before any-mode table; exact key, then key class, then `any`.
  Story: needed (`keys/instance-over-noun`, `keys/exact-over-class`).
- **R1.7** A key sequence waits when a longer sequence extends it at any
  level. Every pending prefix (a multi-key row's prefix such as `g` of
  `g g`, and a row entering a pending mode with a prefix, such as the
  terminal leader) waits as long as one setting says, `sequence-timeout`:
  `0`, the default, waits until a key comes; a positive duration expires
  it and the innermost exact match runs. No mode has a timeout of its own.
  A key no row continues the sequence with is `sequence-unknown`'s:
  `pass` (default) runs the exact match and handles the key as if typed
  alone; `swallow` drops both with a notice naming the keys. A sequence
  a level only inherits (a table of the mode its mode extends, such as a
  row noun's normal `d d` read in `visual-line`) does not wait over a row
  the mode binds on its own tables, at any level, for the keys typed (the
  list's `visual-line` `d`): the mode's own key answers at once and the
  hints leave the shadowed sequence out (`sequence-inherit` `drop`, the
  default; `keep` lets it wait). Stories:
  `lists/sequence-waits-forever`, `lists/sequence-timeout-expires`,
  `terminal/sequence-unknown-pass`, `terminal/sequence-unknown-swallow`,
  `tables/visual-line-inherited-sequence-drop`,
  `tables/visual-line-inherited-sequence-keep`.
- **R1.7a** A row's key may name a setting (`{terminal-leader}`), as may
  its hint, help and string arguments: the row follows the setting's
  value, declared before or after it, in a defaults file, Go or the
  user's `bindings.json`. Nothing finds rows by what they do. Stories:
  `terminal/leader-setting-footer`, `terminal/leader-user-row-names-setting`.
- **R1.8** A row whose action is not available at the focus (its verb
  guard, `Runtime.Available`) does not answer and does not appear in
  `bindings.list`, the footer or `?` help; the next row on the key in line
  order is tried, and when none at a level is available the key goes on to
  the next level out. A key may be bound to several actions in one context
  for exactly this: the component says what it can do now, the rows carry
  no condition (a row's own `when` is a debt `bindings check` lists). A
  container that must keep a key at its edge binds it to the noop
  (`ochazuke.key.ignore`), which consumes it. Story: needed
  (`keys/guard-hides-row`); tests `TestResolverRunsTheFirstAvailableRow`,
  `TestResolverBubblesWhenNoneIsAvailable`, `TestResolverConsumedByNoop`.
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
- **R2.10** The current mode is always visible: the mode badge at the right
  of the header's title row is on screen in every state (normal, insert,
  visual, a pending or leader mode, under a modal, while the palette is
  open, in an input) and changes in the same frame as the mode. A pending
  mode shows the key sequence it holds while it waits (`NORMAL G`,
  `LEADER PENDING CTRL+A`), and a recording adds its register
  (`NORMAL · REC @a`). The held keys follow the `mode-badge.key-case`
  setting (`upper`, the default, or `as-typed`: `NORMAL g`, `NORMAL G`,
  `LEADER PENDING ctrl+a`); the mode's name is upper case either way. The badge names the mode for people: the mode's
  name with `_` read as a space, never a raw payload or an internal id; a
  name that reads badly is renamed in the data that defines the mode, not
  special-cased in the badge. No setting hides the badge; only a page
  drawn without chrome (`chrome: none` stories, an embedded page body) has
  no header. Reference: Vim's `showmode` and `showcmd` (P2). Story:
  `modes/badge-every-state.json`, `modes/badge-leader-prefix.json`,
  `modes/badge-recording.json`, `modes/badge-keys-as-typed.json` (the story `badge` expectation reads the
  title row).

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
  spilling. A card timeline first scrolls to its end (or top) by the
  half-page rule, then clamps or spills (R13.2). Story:
  `motions/nospill.json`, `timeline/card-end-clamp.json`; needed for the
  modal case.
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
- **R3.13** Focus never rests on an instance the last frame did not draw.
  Right after the Model draws the current page (before the footer), when
  the focus's stop was not placed by that frame (a tab switched away from
  it, a panel collapsed), the focus moves to its nearest ancestor below
  the page that the frame drew (a list nested under a row goes back to the
  row), else to the page's first stop in drawing order (its remembered
  child), else to the page, and the page is drawn again around it. Drawn
  means placed (`Runtime.Placed`/`PlacedAt`): a list row is drawn when its
  list is, a grid cell when its grid is, a card's body when its timeline
  is; a stop the frame did not place hides everything beneath it. The move
  is an ordinary focus change: the mode rule applies (a hidden prompt in
  insert leaves to normal) and the stop left remembers its child, so `}`
  goes back once it is drawn again. Not while a modal is open (the frame
  drew the level beneath it; the first frame after it closes applies the
  rule), not before the page's first frame (the CLI never draws one), not
  when the frame placed no stop at all, and never for a focus outside a
  stop (the page, a panel). An app does not move the focus off what it
  stopped drawing. Story: `focus/hidden-stop-moves-focus.json`,
  `focus/hidden-input-leaves-insert.json`,
  `focus/hidden-timeline-card.json`; tests `TestFocusHidden*`.
- **R3.14** The half-page rule: one motion key press never scrolls a view
  by more than half its height. A motion (`j`/`k`, a step, `}`/`{` between
  neighbouring items) whose target would scroll the view further scrolls
  half the view toward it instead, and the cursor moves only once the
  target is in view; until then it stays where it is, or, a line cursor
  that would leave the view, on the row nearest the target inside it.
  Explicit page and line keys keep their sizes (`ctrl+d`/`ctrl+u` half a
  page, `pgdown`/`pgup` and `ctrl+f`/`ctrl+b` a page, `J`/`K` a line), and
  `G`/`gg`, a count's `5G` and the searches (`]]`, `] h`, `/`) stay jumps.
  A count repeats the press. One helper serves every component:
  `MotionScroll(want, page)` is the scroll one press makes and
  `SpanInView` whether the target shows. Where it applies: card timelines,
  card to card and step to step and to a timeline's end (R13.2);
  `markdown.Doc`'s `}`/`{`. Single-line rows (lists, tables without
  wrapped cells, trees, the help tree) move one line per press and need
  nothing. Stories: `timeline/card-half-page.json`,
  `timeline/step-tall.json`, `timeline/card-end-clamp.json`,
  `markdown/motion-long-block.json`; test `TestTimelineHalfPageRule`.

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
- **R4.7** A verb check asks through the same confirm modal (R4.5): a
  check on a verb (`AddVerbCheck`; the quit hook `BeforeQuit` on
  `app.quit`) that answers `AskFirst` opens it instead of running the
  verb, `y` runs it with no second question, `n`/`esc` leave it unrun; a
  `Refuse` fails the request with its reason on the status line. The
  check runs from every path (a key, the capture row's `ctrl+c`, the
  palette, the CLI), and `force: true` skips it (`app.quit {force: true}`,
  vim's `:q!`). An app never wraps the framework's verb to ask. Stories:
  `quit/asks-before-quit.json`, `quit/clean-quits.json`,
  `quit/force-skips.json`, `quit/refuse.json`; tests in
  `verb_checks_test.go`.
- **R4.8** Running a command that opens a modal which is already open
  changes that modal's contents; it never errors with "already open" and
  never stacks a second copy (`:help settings` inside the docs viewer shows
  the settings page in the same viewer, from the top, over the focus it
  opened over). The opener says so with `ReplaceOpen` on `OpenModal`; a
  modal that holds a question (a confirm, a prompt, a chooser) keeps it and
  refuses the second open. Story: `docs/reopen-while-open.json`; tests
  `TestOpenModalReplaceOpen`, `TestDocsOpenWhileOpenShowsTheNewTopic`.
- **R4.9** A modal's size stays fixed while its content scrolls or its
  selection moves; it changes only when the content set changes or the
  screen resizes. A scrolling body sizes from its whole content
  (`Detail.Width`, the widest wrapped line), not the lines in view. Story:
  `docs/width-steady-scroll.json`; tests
  `TestDocsViewerKeepsItsWidthWhileScrolling`,
  `TestDetailWidthIsTheWholeTextsWidestLine`.

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
  into the marks on every list, table and tree (not on a `NoMarks` one),
  in the `visual-line` mode (R5.16): `j`/`k` extend it without
  spilling, a row's verb acts on every selected row (R5.15),
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
  pins from the right until one shows; a `Column.Group` pin drops with the
  pin after it (the help's prefix never shows without its key, test
  `TestGroupPinDropsWithThePinAfterIt`). Story: `tables/pin-cell.json`,
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
  **Backgrounds:** a tone may carry a background (`ToneStyle.Bg`,
  `themes.json` `bg`, a colour or a role); the built-in `error` tone is
  bold on an error background in every built-in theme (light and dark
  sides), bold alone in mono. A span's `Bg` lays a tone's background under
  another tone's foreground (a duplicate key keeps its key colour on the
  error background). The `chip` and `chip.focused` tones draw chips (a
  query's terms): a dim background, and the focused chip bright (surface on
  accent, bold; bold alone in mono). On the selected row the background is dropped like
  the colour (the setting `tones.selected-background`, default `drop`);
  `keep` draws that cell on its background, bold, out of the reverse bar,
  the rest of the row still reversed (the opt-out). Stories:
  `tables/tones-error-background.json`,
  `tables/tones-error-background-keep.json`; tests in `tone_bg_test.go`.
- **R5.15** An action over a visual selection acts on every selected row.
  While `V` is on, a row's own verb (an application verb of the row's
  noun, not a motion), run from its key, the palette or the API, has the
  selection as its subject, in display order: a verb that takes many
  (`NounBuilder.Many`, the catalog's `many`) runs once with every row in
  `Ctx.Subjects` (one chooser, one confirm, one notice: "Move 3 rows to",
  "Delete alpha and 2 more?"), and any other verb runs once per row
  (`Request.Subjects`; a verb's confirm still asks once, with the count).
  A verb that asks about its row (opens a chooser) without taking many
  runs on the first row only and says it left the rest; such a verb wants
  `Many`. One notice reports the run: info "VERB: done on N rows", or an
  error "VERB: D done, F failed (first failure)" when some failed; only
  when none was done does the action fail. Failures never stop the other
  rows. The selection ends when the verb runs (vim; `visual.after-action`
  `keep` keeps it over the rows still shown), motions extend it. The
  verb's availability is read while the selection is still on, so a verb
  available only over a selection answers.
  `Y`/`y` copy every selected row, one per line. Where the rows keep an
  undo stack (a tree), the run is one undo step: one `u` restores them
  all. Which verbs take many is data on the verb, never a list of names.
  Stories: `tables/visual-apply.json`, `tables/visual-many-subjects.json`,
  `tables/visual-partial-failure.json`, `tree/visual-undo-group.json`;
  tests `TestVisualSelection*`, `TestRequestSubjects`,
  `TestVisualYankCopiesEverySelectedRow`.

- **R5.16** A visual row selection is a mode. `V` enters `visual-line`,
  the line-shaped selection mode (the editor's `V` is the same mode; a
  mode's selection shape is data, `"modes": {"visual-line": {"selection":
  "line"}}`, read with `Runtime.SelectionShape`, never a mode name). The
  header's badge reads `VISUAL-LINE` and the footer shows the mode's keys.
  In it one key acts on the selection, then visual ends: `d` deletes
  (`list.apply {verb: delete}`, available only when a selected row's noun
  has `delete`, else the key goes on out), `y` yanks the rows, an
  application's own single keys bound in its row noun's `visual-line`
  table (`>` nest, `p` put) act on every row; `esc` or `V` leaves without
  acting; `:` and `?` still open the palette and help, and the palette's
  verbs act on the selection. `visual-line` extends `normal`
  (`ModeExtends`): a noun with no `visual-line` table of its own keeps its
  normal keys there (they act on the selection), and a noun with one reads
  only it; either way a `d d` in its normal table never makes the
  visual-line `d` wait (R1.7, `sequence-inherit`). A modal
  opened over the selection (a confirm, the palette, a chooser) holds it;
  its close resumes it. Leaving the list ends it. Opt out with
  `visual.list` `marks` (the older selection in normal mode). Stories:
  `tables/visual-line-delete-two.json`, `tables/visual-line-yank-two.json`,
  `tables/visual-line-motions-esc.json`, `tables/visual-after-keep.json`,
  `tables/visual-after-end.json`, `tree/visual-line-nest.json`; tests
  `list_visual_test.go`.

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
- **R6.4** Three things tell the user, kept apart. The status line is one
  slot with two tones, cleared by the next key: an error (a key that
  failed, `ctx.Error`) reads `Error: TEXT` in the error style in place of
  the hints; a plain message (`ctx.Info`: progress, success, "nothing to
  do") reads before the hints, cut to half the width. Either replaces the
  other. A plain message is never drawn as an error and an error never as
  plain text: every sink has both tones (no fallback to the error tone),
  and `ctx.Status`, the old name that invited plain messages into the
  error tone, is deprecated; `strictcheck.Check` fails an app's tests on
  any use. Check: `TestStatusInfoReachesEverySink`, an app's strictcheck
  test. `Notices` persist until `notice.clear`; the
  footer shows the newest at most half the width with `(+N more)`, so the
  hints always keep the rest. The CLI prints the same as `error:`,
  `status:` and `notice:` lines. Story: `notices/persist.json`,
  `notices/footer-keeps-hints.json`, `chrome/status-info-plain-80.json`,
  `chrome/status-error-clears-80.json`; goldens `chrome/status-info-80`,
  `chrome/status-error-80`.
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
  the modal width: the prefix, key, operation and description wrap, the
  arguments cut with `…` and drop first when narrow (`1›` counts them, `z l`
  scrolls them in). Golden: `testdata/help-80.golden`; stories
  `help/prefix-columns-{80,120}.json`.
- **R6.9** Help orders a section's rows so a sequence reads with its
  prefix: inside each section (the sections keep their order: the
  answering noun innermost first, app, movement) the keys that start no
  sequence come first in the order they answer, then each prefix's row
  (R6.10) in the order it first answers, the rows under a prefix sorted by
  their final key (characters case folded, lower case first, then named
  keys, so the generated `esc` cancel is last), a longer prefix's row under
  the one it extends (`g`, then `g c` under it). `BindingHelp` keeps
  resolution order; only the modal sorts. (Until v0.28.0 the key was two
  columns, prefix then key, the prefix drawn once per group: R6.10
  replaces that with the whole sequence.) Story:
  `help/prefix-columns-{80,120}.json`; test `TestHelpOrderGroupsPrefixes`.
- **R6.10** Help shows a key that switches to a "next key" state the same
  way whatever does it: a row bound to `mode.enter` into a pending mode
  (the terminal leader `ctrl+a` → `leader_pending`) and the shared first
  key of sequences (`g` of `g c`). Each such prefix has a row, its key
  alone: an entering row reads `mode.enter`, `enters leader_pending: waits
  for the next key (N keys)` (and what the key does alone after the
  timeout); a prefix bound to nothing alone gets a made row, no operation,
  `waits for the next key (N keys)`; a prefix bound to an operation of its
  own keeps it. Under the prefix's row come the sequences it starts, each
  line the whole sequence in the key column (`ctrl+a v  side vim`,
  `g c x` under `g c` under `g`), each listed once. A pending mode's rows
  are the rows the same walk answers with in that mode (its noun tables,
  the focused instance's, app's), so they follow `TerminalLeader()` and a
  key entering the mode from several places lists them under each; hidden
  and unbound rows of the mode stay out; a row of the mode that enters a
  further mode lists that mode once more, and one level deeper it shows as
  `mode.enter` (no cycle). The register, macro and count rows (a `Help` of
  their own) and operators (`op`: their mode is every motion) stay one row.
  Each sequence row carries its steps (`HelpRow.Steps`): every key with
  what it does there: enters a mode, waits for the next key, the final
  operation. The made row's wording is `helpWaits` (with `prefixRow`) in
  `help_modes.go`. The help (here and pick; the table view stays a plain
  table) is a tree (R11.1): a prefix row holds its sequences and starts
  open, so `?` over a live terminal shows every leader key at once; a
  sequence row holds its steps, one key per line, and starts closed.
  `l`/`right` open, `h`/`left` close (or step to the parent), `enter`
  toggles, `j`/`k` move across every row shown, `z l`/`z h` scroll the
  columns. `/` reads a row's text with its steps and its sequences, so a
  closed row is found by what is inside it. Stories
  `help/leader-sequences.json`, `help/leader-rebound.json`,
  `help/mode-cycle.json`, `help/sequence-steps.json`,
  `help/filter-finds-steps.json`, `help/prefix-columns-{80,120}.json`;
  test `TestHelpListsPendingModeRowsAsSequences`.
- **R6.8** The tab row never drops a page silently: when the tabs do not
  fit, `‹N` at the left edge and `N›` at the right count the pages hidden on
  each side, the active tab is always on the row, and the shown tabs plus
  the counts are every page; with every tab fitting there are no markers.
  Story: `pages/tabs-{50,80,120}-{start,middle,end}.json`,
  `pages/tabs-fit.json`, `pages/tabs-50-step.json`; unit test
  `TestTabsMarkHiddenPagesOnEachSide`.

- **R6.11** Status segments ride the rule under the header, right-aligned,
  joined by ` · `, with ` ──` after them and at least `── ` of rule before:
  they take no row, so the body never shrinks or moves when one appears,
  changes or goes, and they never sit in the footer or the hints. A segment
  is a data row (`Segment`: id, label/format, one source, interval,
  timeout, tone, order), never a branch per segment. A refresh runs off the
  loop under its own timeout, one at a time per segment; the last text stays
  until the next answer, a failure draws in `state.errored`, and keys never
  wait for one. A segment that does not fit is dropped from the last; the
  first alone is cut with `…`. Tests `TestSegment*` (`segment_test.go`);
  story `segments/header-rule.json`.
- **R6.12** Anything that changes the UI has a setting to opt out of it:
  each optional area as a whole and each item inside it, as an ordinary
  setting (the Settings page, `settings set`, settings.json), checked before
  the area draws or does work. Segments: `status.segments` (the area, off
  stops every refresh), `status.segments.<id>` (one, its default the row's
  `enabled`), `status.segment-order` (the order `segment.move` writes).
  Existing examples: `focus-display`, `modal-dim`, `dim-unfocused`. Check:
  the diff declares the setting with the feature.
- **R6.13** Every page can be hidden (R6.12 for pages): one list setting,
  `pages.hidden`, holds the ids left out of the tab row and of
  `page.next`/`page.previous` (ctrl+j/k), the same for an app's own pages,
  mounted catalog pages and the Settings page; never a per-page branch. A
  new page an app adds as opt-in is declared hidden by default as data
  (`Page.DefaultHidden`, the `DefaultHidden` option, a catalog page's
  `defaultHidden`), which is the setting's default. Hiding keeps the verb
  surface: `page.open`, the `'` picker (row marked `hidden`), the palette
  and the CLI still open a hidden page, `page.open` says it is hidden, and
  its tab shows while it is current (the tab row never lies about where
  the focus is, R6.8) and goes when another page opens. `page.hide` refuses
  the last page in the tabs; a start page the user hid gives way at start
  (`Model.Init`, `OpenVisibleStart`) to the first page in the tabs.
  `page.show` brings a page back in its registration place; `pages.list`
  marks hidden pages. Read every frame, so `settings set`, `config.reload`
  and the verbs apply live. Stories `pages/hide-tab-gone.json`,
  `pages/hidden-opens-from-palette.json`, `pages/default-hidden.json`;
  tests `page_hide_test.go`, `TestCatalogPageDefaultHidden`.

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
- **R7.7** A chooser with sources shows one source's rows at a time:
  `tab`/`shift+tab` step to the next or previous source, wrapping
  (`chooser.next-source`/`previous-source`, guard `chooser-sources`, so
  tab is free in a one-source chooser); the query carries over, and the
  title names the current source in brackets (`Pick · one [two] three`).
  A live source sees the typed text and answers again on every edit. A row
  a source can open (a folder) opens with `enter` (unless
  `chooser.enter` is `pick`) and `ctrl+l`; `alt+enter` picks the
  highlighted row whatever it is; `alt+up`, and `backspace` on an empty
  query, step the source back out. A row that only opens (a folder in a
  file chooser) opens on a pick. The last source used is remembered per
  chooser kind (`chooser.remember-source`, default on; off reopens on the
  first source). Stories: `chooser/sources-cycle.json`,
  `chooser/live-source.json`, `chooser/remember-source-on.json`,
  `chooser/remember-source-off.json`; tests `TestChooserSources*`,
  `TestChooserLiveSource*`, `TestChooserRemembersTheSource`.
- **R7.8** A path is chosen, not typed blind: a path parameter (type
  `path`, `path: file|dir`) opens the path chooser with the sources
  browse (one folder's entries, folders first; a folder chooser ends with
  `use <folder>`), recent (the application's list, then this run's picks)
  and typed (completes the path being typed, read as typed; a parameter
  with `new` offers a path not there yet, after the completions), then the
  application's own (known folders, a project's). Browse starts at the
  field's folder, else the parameter's `base`, else `path.start`
  (`base`: the application's folder such as the open file's, `home`,
  `cwd`). A folder under `path.no-auto-list` (external volumes) is never
  listed on its own: browse says so until the person opens into it or
  types the path. Hidden entries list with `path.hidden` or once the
  typed part starts with `.`. Stories: `forms/path-browse.json`,
  `forms/path-dir.json`, `forms/path-typed.json`,
  `forms/path-text-field.json`; tests `TestPath*`.

### R8. Terminal

- **R8.1** `any`/`paste` rows forward everything to the pane in terminal
  mode. Story: `terminal/ctrl-c-reaches-pane.json`.
- **R8.2** `ctrl+a` (the `terminal-leader` setting, through rows keyed
  `{terminal-leader}`) enters `leader_pending`; `d` detaches, the leader
  sends itself, a bound key runs. The leader waits as every sequence does
  (R1.7: `sequence-timeout`, default `0`, waits for the next key; a
  positive one sends the leader alone when it passes), and an unbound key
  is `sequence-unknown`'s (`pass`: the leader, then the key alone;
  `swallow`: neither, with a notice). `terminal-leader-timeout` is a
  deprecated name of `sequence-timeout` for one release. Stories:
  `terminal/detach-focus-returns`, `leader-twice-sends-one`,
  `sequence-unknown-pass`, `sequence-unknown-swallow`,
  `sequence-timeout-sends-leader`, `sequence-timeout-default-waits`,
  `leader-setting-footer`, `leader-setting-detach`,
  `leader-user-row-names-setting`, `leader-timeout-deprecated-alias`.
- **R8.3** Frames reach the component only through `Deliver` with a current
  ticket; a frame for a closed terminal is dropped. Story:
  `terminal/output-ticks.json`.
- **R8.4** Every close path releases the stream exactly once. Check: unit
  test with a counting `TerminalReleaser`.

### R9. Inputs and the editor

- **R9.1** Readline and Vim rows are `editor` noun defaults from
  `bindings/editor.json`; an app overrides by binding on a noun or instance
  on the stack or in a binding file; it never edits the JSON. Check: the
  diff.
- **R9.1a** Every input answers the standard editing keys in insert, as
  rows of the `editor` noun's insert table (no key names in code):
  `alt+backspace`/`ctrl+backspace` kill the word before the cursor (what
  `alt+b` moves over, `input.kill {motion: word-previous}`), `ctrl+w` the
  WORD or path component before it, `ctrl+u`/`super+backspace` to the
  line's start, `alt+d`/`ctrl+delete` the word after (`word-next`, what
  `alt+f` moves over), `ctrl+k` to the line's end; `alt+left`/`ctrl+left`
  and `alt+right`/`ctrl+right` move by word, `super+left`/`super+right`
  (and `meta+left`/`meta+right`, how a kitty terminal's cmd+arrow decodes)
  to the line's ends, beside `ctrl+a`/`ctrl+e`/`home`/`end`. Kills go to
  the yank history (`g y`). With a select range open (R9.6) a kill
  removes the range. The alternate kill rows are hidden from the footer.
  Tests `TestInputEditingKeys*`, `TestPromptEditingKeys`,
  `TestTerminalSequencesReachTheRows` (recorded byte sequences); stories
  `inputs/word-delete-keys.json`, `inputs/word-motion-keys.json`.
- **R9.2** Offsets count runes; rendering accounts for cell width. Story:
  needed (`inputs/wide-runes`), with a CJK or emoji string.
- **R9.3** Undo and redo round-trip every editing operation: for each
  `input.*` verb that changes text, `undo` restores the prior text and
  cursor. Check: table-driven unit test.
- **R9.4** A count applies to every motion and operator that Vim gives one
  to, and an out-of-range count clamps silently. Story: needed
  (`editor/count-clamps`).
- **R9.5** Dot-repeat replays the last `Repeatable` operation with its count
  and register that can run at the focus: one available there, and for
  typing into an input (the editor's implicitly guarded `input.*` rows) an
  input focused. Typing into a line that has since closed is never
  replayed outside an input; the change before it is. With nothing to
  repeat, `.` is not an error: `repeat.empty` `info` (default) says
  `nothing to repeat` in the plain tone (R6.4), `silent` shows nothing.
  Stories: `actions/dot-after-input-closes.json`,
  `actions/dot-nothing-to-repeat.json`, `actions/dot-nothing-silent.json`;
  tests `TestDotSkipsTypingOutsideAnInput`, `TestDotDoesNotRepeatMotion`;
  needed (`editor/dot-repeat`).
- **R9.6** Select: a component that creates or renames a thing may open its
  line with text selected (`Input.SelectText`, `SelectAll`), like Vim's
  select mode. The first text typed or pasted replaces the selection and
  typing continues in insert; backspace or delete removes it; `esc`, a
  motion (`left`/`right` land on its edges), an undo or any other edit
  closes it and keeps the text (a motion moves as it would from the
  range's end: `end`/`ctrl+e` to the end, `home`/`ctrl+a` to the start, a
  word motion by a word); a kill key removes it as backspace does;
  `enter` does what it does on the line,
  with the text kept. Every printable key types, `j` included: moving away
  is `esc` then a motion. The selection is drawn in the visual-selection
  style (reverse). Select is a state of the input, not a mode: the person
  is inserting, the mode is `insert`, and no binding table is added (modes
  stay few: what the person is doing). Stories: `tree/select-type-replaces.json`,
  `tree/select-esc-keeps.json`, `tree/select-move-keeps.json`; tests
  `TestSelectText*`.
- **R9.7** Inline completion is data on the trigger: a token runs to
  whitespace or one of the trigger's `Boundaries` (a jq `.` after `|` or
  `(`); accepting keeps the trigger unless the trigger says `DropTrigger`;
  a `SearchAt` search is given the whole text and cursor, so it can
  complete what the text before the token names (a document path). A
  choices source that declares a `query` parameter is called with the text
  typed so far wherever its choices are listed: the palette word, a form's
  suggestions chooser (which then asks again as the person types), and the
  check of a value. Tests `TestCompletionDropTrigger`,
  `TestCompletionTriggerBoundaries`, `TestCompletionSearchAtSeesTheText`,
  `TestChoicesSeeTheTypedText`, `TestFormSuggestionsFollowTheQuery`,
  `TestPaletteChoicesSeeTheWord`.

### R10. Delivery and jobs as the user sees them

- **R10.1** Application work never blocks the loop; a `Pending` result's
  completion lands on its owner or is dropped if the owner closed. Check:
  the diff and the delivery tests.
- **R10.2** A newer job in the same slot cancels the older; the stale
  completion never paints. Story: needed (`jobs/slot-replaces`).
- **R10.3** Confirmation is a property of the verb, opened at the focus, and
  identical from a key, the palette and the CLI. Story:
  `modals/confirm-yes.json`; needed for the palette and CLI paths.
- **R10.4** A foreground program (`Foreground`, an actions.json row's
  `"foreground": true`) has the whole terminal: the TUI suspends, nothing
  draws over it, and the TUI comes back where it was when the program
  exits. The exit is always told: `PROGRAM exited` (the plain tone), or
  `Error: PROGRAM exited with status N` with the job failed. Story:
  `foreground/exit-status.json`; tests in `foreground_test.go`.

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
- **R11.6** A new node can open with a placeholder selected (R9.6):
  `outline.add {where, text, select: true}` writes the node with `text` at
  once and opens its line selected; `outline.edit {select: true}` and
  `Tree.Edit(id, true)` do the same for an existing line. Typing writes a
  rename after the pause; `esc` keeps the placeholder and writes nothing
  more. Editing a line is renaming its node: `outline.edit` without
  `select` (`i`) follows the `rename.select` setting (default on: opened
  selected; off: cursor at the end, the opt-out), and so does a record
  editor whose `RecordSpec.Rename` is set (a rename form). `outline.add`
  without `select` is unchanged (empty line, cursor at the end). Stories
  as R9.6 and `tree/rename-select.json`, `tree/rename-select-off.json`;
  tests `TestOutlineAddSelectPlaceholder`, `TestOutlineEditSelect`,
  `TestOutlineEditRename*`, `TestOutlineRenameMotionsKeepTheName`,
  `TestRecordEditorRenameSelects`.
- **R11.7** Zoom (view state, never on the undo stack, on every tree):
  `z +` `outline.zoom-in` draws only the node's subtree, `z -`
  `outline.zoom-out` steps out one level, `z 0` `outline.zoom-reset` shows
  every level; `h` past the zoom's top steps the zoom out onto the parent,
  as `h` steps onto the parent anywhere. The hidden ancestors are a
  breadcrumb: the nearest `tree.breadcrumb.ancestors` (default 2; 0 all)
  named and dimmed, farther ones folded into `…` (the nested-json 1-drill
  prototype). Settings, each value an option: `tree.zoom` `manual`
  (default) | `auto` (also zooms, keeping two levels above the row, when
  the selected row's indent takes over half the width) | `off` (no zoom;
  the verbs unavailable); `tree.zoom.root` `crumb` (default: the node
  ends the breadcrumb, its children are the top rows) | `row` (it stays as
  the top row); `tree.breadcrumb` `line` (default: a line above the rows)
  | `title` (in the header beside the mode, while the tree has the focus)
  | `off`. An edit or `Select` that moves the selection out of the zoom
  steps the zoom out to show it. Stories `tree/zoom-*.json`; tests
  `tree_zoom_test.go`.
- **R11.8** A node can refuse a change: `TreeNode.Fixed` names the
  changes it refuses (`rename`, `move` (move, indent, outdent), `delete`
  (itself or any ancestor)), and `Tree.Refuse` is the application's check
  on any change. A refused change is refused from every path (keys, the
  palette, the CLI, the API, `Tree` methods) and nothing is written. The
  verbs a node's `Fixed` refuses are not available on it (guards
  `outline-renamable`, `outline-movable`, `outline-deletable`), so their
  keys do nothing, their hints drop out and the palette does not offer
  them; `Refuse` answers with its reason on the status line. Never a
  branch on a node id. Stories: `tree/fixed-root-refuses.json`,
  `tree/fixed-rename-only.json`; tests in `tree_fixed_test.go`.

### R12. Action names and aliases

- **R12.1** The action table is flat: a name (`noun.verb`, the dot a
  convention, so a verb may hold one: `dirs.base.add`) is looked up whole,
  and an alias is one more name for the same action. An alias runs the
  canonical action; the handler is told the name and its noun (the subject
  side) in `Ctx.Invoked` / `Runtime.Invoked`. Test
  `TestAliasRunsTheCanonicalActionAndTellsTheHandlerItsName`.
- **R12.2** An alias is never a second entry. The palette lists the
  canonical verb with its aliases on the label (`forget-row · also drop`)
  and a typed alias narrows to it; `?` help names the canonical operation
  and lists the aliases once in its description (`· also: drop`); the
  CLI shows them as the verb's oden aliases (`show, display`), the catalog
  and `operations.list` as `aliases`, `bindings list` as a row's `aliases`
  beside its canonical request (`via` keeps a row's alias spelling).
  Stories: `actions/alias-in-palette.json`,
  `actions/help-lists-alias-once.json`; test
  `TestAliasesAreListedOnceUnderTheCanonicalName`.
- **R12.3** A renamed verb keeps its old name as a deprecated alias for one
  release (`NounBuilder.Renamed(old, since, removeAfter)`, Keep actions);
  its first use in a runtime posts one notice naming the new name, and a
  `bindings.json` row on the old name still works, `bindings check` saying
  what it resolves to. Story: `actions/old-name-binding.json`.
- **R12.4** A bare or short name a user wants is a configured alias row in
  `actions.json` (`{"alias": "pane.k", "action": "pane.kill"}`), not code.
  A row that would take the name of an action or of another alias is
  skipped and reported (a notice at load; `actions check`, `actions reload`
  and `bindings check` list it). Stories: `actions/user-alias.json`,
  `actions/alias-clash-reported.json`.
  An alias row may preset arguments (`"args": {"force": true}`): every path
  that runs the alias (a key, the palette, the CLI, the API) runs its action
  with them, and a typed word, a flag or a row's argument of the same name
  wins. A bare alias (`w!`, no noun) takes its action's noun and is typed as
  one word in the palette and on the CLI; a row names it `noun.w!`. A bare
  alias named like a verb is refused unless `aliases.shadow` is `allow`
  (then the typed word runs the alias, `noun verb` still the verb). Stories:
  `actions/alias-preset-args.json`, `actions/alias-shadow-refused.json`,
  `actions/alias-shadow-allowed.json`; tests in `alias_args_test.go`,
  `cli` `TestCLIBareAliasWithArgs`.
- **R12.5** A write is never filled silently: a request with a required
  parameter missing fails naming it (API, CLI: `NAME is required`), and the
  palette asks for it instead of running; nothing reads the value from the
  focus or a guess. Tests `TestWritesAreNeverSilentlyFilled`,
  `TestCLIAliasesRunTheCanonicalVerb`.
- **R12.6** The palette asks for missing arguments with a form. Enter on a
  line whose verb still lacks a required word, or a key whose row leaves a
  required argument out, opens the args form (`Runtime.OpenArgsForm`, modal
  `args-form`): one field per parameter, the required ones first, an
  optional one marked `(optional)`, the words already typed filled in, the
  focus on the first missing one (typing, when it is a text field). Choices
  and noun-typed parameters are picked from a type-to-filter chooser
  (`form.suggest`, `/`), free text is typed. Nothing is filled from the
  focus or a guess (R12.5). `enter` is `form.run` once every field is
  valid, else `form.next-missing` (the field that still needs a value,
  opening its chooser); `esc` in normal mode cancels with nothing run and
  the palette's line back as typed; there is no save key. A run that fails
  reopens the form with the values and `! not run:`. The setting
  `palette.missing-opens-form` (default on) turned off restores
  `missing NAME (type): description`. The CLI and the API never open it.
  Stories: `palette/form-missing-one.json`, `palette/form-noun-autocomplete.json`,
  `palette/form-choices.json`, `palette/form-prefill.json`,
  `palette/form-esc-cancels.json`, `palette/form-setting-off.json`,
  `palette/form-binding.json`, `palette/form-look.json`; tests in
  `palette_form_test.go`. A path parameter's field opens the path chooser
  (R7.8): a stop whose `enter`, `space` or `i` opens it (`path.field`
  `picker`, the default), or a text field whose `ctrl+space` (and `enter`
  in normal mode) opens it (`text`); a pick is written at once. A form
  that opens on a missing path stop opens its chooser over it, so typing
  reaches the path. When the only missing required parameter is a path,
  `forms.single-path` says what opens (P9): `chooser` (default) the path
  chooser alone, its pick running the action (optional parameters left
  out, esc runs nothing); `alone` the same only when the path is the
  verb's one parameter, else the form; `form` always the form.
  `TestPathFormField`, `TestPathArgsForm`; stories
  `palette/form-single-path-opens-chooser.json`,
  `palette/form-single-path-esc-cancels.json`,
  `palette/form-single-path-setting-form.json`,
  `palette/form-path-with-optional.json`.

### R13. Card timelines: inside a card

- **R13.1** Inside a card is a focus place, not a mode: the timeline's
  body instance (`<id>/body`, noun `ochazuke.timeline-body`, a part of the
  card list) holds the focus, the mode stays what it was, and the card
  keeps its focused border. `l` and `h` each bind several actions with no
  conditions (`l`: `timeline.open`, then `timeline.enter`; `h`:
  `timeline.close`, then `timeline.leave`; inside, `h`:
  `timeline.fold-step`, then `timeline.leave`), and the first available
  answers (R1.8). The card's state is the actions' availability: `open`
  on a closed card, `close` on an open one, `enter` while there is a card,
  `leave` on a card or inside, `fold-step` inside on a step in an open
  section:

  | Where | `l` | `h` |
  |---|---|---|
  | closed card | `timeline.open` | `timeline.leave`: the parent stop (`ParentStop`), else stays |
  | open card | `timeline.enter`: onto its first step | `timeline.close` |
  | inside, in an open section | `timeline.enter`: nothing (the section is open) | `timeline.fold-step`: folds the section the step is in (a tool call, thinking, an answer; any of its paragraphs), the cursor on its title |
  | inside | `timeline.enter`: opens the step's folded section, else nothing | `timeline.leave`: back to the card, still open (`esc` too) |

  `l` is one key with no sequence and no timing; `enter`/`space` still
  toggle (closing the card from inside puts the focus on the card); `?`
  help and the footer show only the row that answers at the place.
  So `h` inside folds an open section first and leaves on the next `h`.
  Stories: `timeline/step-in.json`, `timeline/step-out.json`,
  `timeline/step-enter-toggles.json`, `timeline/step-fold.json`; tests
  `TestTimelineLAndHByPlace`, `TestTimelineHFoldsAnOpenStep`,
  `TestTimelineActionsReportAvailabilityByCardState`.
- **R13.2** Inside, `j`/`k` move by step (`timeline.step {delta}`, counts
  work): a card's `Steps`, else its body's paragraphs (blank-line
  separated, a fenced code block one paragraph) and then each section as
  one step. Steps and cards move by the half-page rule (R3.14): a step or
  card taller than the window is read half a window per key, the next one
  taken once it shows (`j` into a tall one shows its top, `k` its end).
  Past the last step `j` goes to the next card, open or closed. On the
  last card, from the card or its last step, `j` first scrolls (half a
  window at most per key) until the card's end shows, then spills to the
  next stop as R3.4, or, at the page's last stop or with `NoSpill`,
  stays: the focus never moves (mtm's Turns timeline is `NoSpill`). `k` on
  the first card scrolls to its top the same way; past the first step `k`
  goes back to the card. A count repeats the key, stops at the edge, and
  only a move from the edge spills. Stories: `timeline/step-spill.json`,
  `timeline/step-back.json`, `timeline/step-tall.json`,
  `timeline/step-explicit.json`, `timeline/card-half-page.json`,
  `timeline/card-end-clamp.json`, `timeline/step-end-clamp.json`,
  `timeline/card-top-clamp.json`, `timeline/spill-after-end.json`; tests
  `TestTimelineHalfPageRule`, `TestTimelineNoSpillScrollsToTheEnd`.
- **R13.3** The current step is marked in the left padding of each of its
  lines (`▌`, the accent, bold), never by reversed text; the window
  scrolls to keep the step (or its shown half) on screen, and the page
  sees the step's lines (`PlacedAt`). Stories:
  `timeline/step-marker-{dark,mono}-80.json` (goldens, `highlighted: 0`).
- **R13.4** `y` inside yanks the current step's text into the registers
  (`timeline.yank-step`; the active register, else the unnamed); an app
  that also copies to the system clipboard rebinds `y` on the body noun
  to its own verb and keeps `timeline.yank-step`. Story:
  `timeline/step-yank.json`.
- **R13.5** The body follows the selection: it keeps the focus while its
  card stays selected (a streaming patch, a refresh), and a selection
  that moves (`G`, the filter) or a card that goes hands the focus to the
  selected card. Test `TestTimelineBodyFollowsTheSelection`.
- **R13.6** Inside a card the body sits under the card: the focus path is
  body, card, list, timeline, so a key the body does not bind answers as
  it does on the card (an app's rows on its card noun: `esc`, `R`, `Y`),
  and the footer and `?` help offer those rows. Story
  `turns/card-rows-inside.json`.
- **R13.7** `gj`/`gk` inside move by screen row (`timeline.row {delta}`,
  counts work): the header step's lines and every body line but blank
  ones. The marker covers the row alone and the step under it is the
  current step (`y`, `l`, `h` act on it); `gk` crosses into the step
  above; past the last row `gj` goes on as `j`, before the first `gk`
  goes back to the card; `j`/`k` go on by step from the row's step.
  Story `turns/row-gj-gk.json`.
- **R13.8** `J`/`K` (and `ctrl+e`/`ctrl+y`) inside scroll the window; a
  scroll that takes the current step (or row) off screen moves the cursor
  to the nearest one shown (the first going down, the last going up), so
  the next step key goes on from what is on screen. A motion pressed
  before the next frame is drawn (after `G`, an open, a step entered)
  measures from where that frame puts the window. Stories
  `turns/scroll-keeps-step.json`, `turns/k-after-g-past-open-card.json`,
  `turns/opened-tall-step-j-scrolls.json`.

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
  could be a branch in Go and could be a row is a row; a condition on
  when it applies is the action's availability, not a guard on the row.
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
- A failed standing experience check (re-invoking an open view, a stable
  frame size while scrolling, resize, repeat, escape from each state, open
  from elsewhere) is at least `major` even without a rule id, labelled
  `J <slug>` with a proposed rule.
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
