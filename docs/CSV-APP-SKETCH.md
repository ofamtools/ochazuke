# CSV reader and editor: second-consumer sketch

Status: built as `examples/csv` with the grid component in `grid/` (branch csv-app), finished on branch csv-pass. The README "CSV example" section lists what the code covers; the sketch below is the brief it followed. Finished in the csv-pass round: `transform` on row (all four cell-set nouns now have it, rule below), the sourced pick list for the transform name, `t i` title and `t s` slug, verb guards (selection verbs only during a selection, row and column verbs only in the grid), the filter field as the sheet's second focus stop, the column confirm naming the header, `:` inside a selection, a selection held under a modal, and the palette listing the focused noun's verbs first. Still deferred: sticky marks across modes, counts on `dd`/`yy`/`u`, the `A1` reference layer, hiding guarded verbs from the footer hints and `bindings list` (both read `bindingRows` in `resolve.go`; in this app the stack already keeps them consistent), and a confirm prompt that names a column when `column.delete` runs from the palette. One deviation: the filter field is a prompt (Readline, insert pinned), so `{` typed there is text; `shift+tab`, `tab` and `esc` return to the remembered cell, and `{`/`}` from the grid both reach the field (two stops, wrapping) (the template expands request arguments only, so the palette asks for `name`).

Palette as a command line (branch ex-palette): `:` opens one line, and the transform rule reads as words. On a cell `transform upper` changes the cell (the noun is omitted, so the focused cell's noun answers), `transform row upper` (or `tr r up`, or `row transform upper`) the row under the cursor, `transform column upper` the column, and during a selection `transform selection upper` the selected cells; outside one, `selection` is refused as not applicable. The cursor never moves, and under the palette the grid's cursor cell and selection stay visible, dimmed.

Rule: every noun that resolves to a cell set has `transform` and the same `t` rows (`t u`, `t l`, `t t`, `t i`, `t s`): cell (the cell, or the selection when one is active), row (every cell of the row), column (every visible cell of the column), selection (the selected cells). The innermost noun on the stack answers, so with a cell focused `t u` is `cell.transform`; the row's rows answer when the row itself is the focus (`--target r1`, `bindings list` for `r1`), and the palette and CLI reach all four.

A modal TUI CSV reader and editor built on ochazuke. Lightweight on purpose: one file in memory, file I/O only on open and save, no daemon, no database, nothing off the loop. It exercises modes, the editor, registers, confirmation, the palette, and a 2D selection the list does not have, so it is the phase 7 second-consumer candidate: it must be buildable without touching the core.

## State the app owns

```js
App = {
  path:   "people.csv",
  header: ["name", "email", "role"],
  rows:   [["ann", "a@x", "dev"], ["bo", "b@x", "ops"]],
  cursor: { row: 0, col: 0 },
  dirty:  false,
  undo:   [],                       // stack of patches, see Transformations
  ui:     ochazuke.Runtime,         // the framework owns focus, modes, modals, editor text, registers
}
```

Instances carry only ids, nouns and labels: rows are `r12`, columns `c2`, cells `r12:c2`. Domain data never lives on an instance.

## Nouns

```js
define("sheet", {                                   // the open file, page level
  verbs: { open: app.open, save: app.save, saveAs: app.saveAs, addColumn: app.addColumn, sort: app.sortBy, undo: app.undo, redo: app.redo },
  bindings: { normal: { "ctrl+s": "save", "ctrl+o": "open", "+": "addColumn", "S": "sort", u: "undo", "ctrl+r": "redo" } },
})

define("cell", {
  verbs: { edit: app.editCell, clear: app.clearCell, yank: app.yankCell, paste: app.pasteCell },
  bindings: {
    normal: { i: "edit", enter: "edit", x: "clear", y: "yank", p: "paste",
              h: "grid.left", j: "grid.down", k: "grid.up", l: "grid.right",
              "0": "grid.rowStart", "$": "grid.rowEnd", "g g": "grid.top", G: "grid.bottom",
              v: enter("visual-block"), V: enter("visual-row"), "ctrl+v": enter("visual-column") },
  },
})

define("row", {
  verbs: { delete: { run: app.deleteRow, confirm: "Delete row {{id}}?" }, insertBelow: app.insertRow, duplicate: app.dupRow },
  bindings: { normal: { "d d": "delete", o: "insertBelow", "y y": "duplicate" } },
})

define("column", {
  verbs: {
    rename: app.renameColumn,
    delete: { run: app.deleteColumn, confirm: "Delete column {{id}}?" },
    sort:   app.sortBy,
    filter: app.filter,
    transform: { run: app.transform, params: { name: { type: "string", source: "transformations.list" } } },
  },
  bindings: { normal: { r: "rename", D: "delete", s: "sort", "/": "filter" } },   // D is an instance row: delete {name: header}
})
```

Stack with a cell focused: `[cell, row, column, grid, sheet, app]`. `dd` resolves on row, `s` on column, `i` on cell, `ctrl+s` on sheet. Verbs without bindings are reached through the palette's command line (`sort column`, `column sort`, or `sort` alone, which takes the nearest noun on the focus path that has it), which completes the focused noun's words first, and through the CLI (`csv column trim --target c2`). A user adds a key with one config row. For a large catalog, tag verbs for grouping, and for an open-ended tail use one verb with a sourced parameter (`transform: { params: { name: { source: "list.transforms" } } }`).

## Page tree

```js
sheet = addPage("sheet", { noun: "sheet" })
grid  = addGrid("grid", { parent: sheet })          // new component, see below
grid.setData(app.header, app.rows)
```

## Editing a cell reuses the editor

```js
app.editCell = ({ instance }) => {
  const { row, col } = app.at(instance.id)
  return ctx.openModal("cell-editor", { noun: "prompt", text: app.rows[row][col], payload: { row, col } })
}
// prompt: Readline, insert only, Escape unbound; the modal noun binds esc → cancel, enter → commit
app.commitCell = ({ payload, text }) => app.apply([{ at: `r${payload.row}:c${payload.col}`, after: text }])
```

Undo inside the editor is the editor's own tree. Undo of table edits is the app's patch stack.

## Visual modes: three, like Excel

Selection is payload on the mode. Motions move the cursor; the shape decides what is selected.

```js
state.mode = { name: "visual-block", anchor: { row: 2, col: 1 }, cursor: { row: 4, col: 3 } }
selected = {
  "visual-block":  cells from anchor to cursor, rectangular     // v
  "visual-row":    whole rows   anchor.row .. cursor.row        // V
  "visual-column": whole columns anchor.col .. cursor.col       // ctrl+v
}

visualCommon = {
  h: "grid.left", j: "grid.down", k: "grid.up", l: "grid.right", "0": "grid.rowStart", "$": "grid.rowEnd",
  "g g": "grid.top", G: "grid.bottom", o: "selection.swapAnchor",
  v: enter("visual-block"), V: enter("visual-row"), "ctrl+v": enter("visual-column"),   // switch shape, keep anchor
  esc: enter("normal"),
  y: "selection.yank", d: "selection.delete", p: "selection.paste", c: "selection.clear", s: "selection.sort", f: "selection.fill",
}
modes["visual-block"] = modes["visual-row"] = modes["visual-column"] = visualCommon
```

Verbs receive the resolved cell set. The shared register stores yanks with their shape, as the editor's registers already do for block and line yanks, so `V y p` pastes rows, `ctrl+v y p` pastes columns, a block paste fills a rectangle. Non-adjacent selection, which Excel handles badly: the selection payload carries a `marks` set.

```js
state.mode = { name: "visual-block", anchor, cursor, marks: new Set(["r2:c1", "r9:c4"]) }
visualCommon += { m: "selection.toggleMark", M: "selection.clearMarks", "ctrl+a": "selection.all" }   // toggle by current shape
cellsOf(selection) = union(rangeFor(shape, anchor, cursor), marks)
```

Every verb takes a cell set, so marks cost nothing downstream; they survive a shape switch and cursor moves, and `esc` drops them with the mode. Sticky marks across modes would move to app state with a visible indicator; start without that.

Open decisions: delete versus clear on a column selection (`D` versus `d`), and whether row and column modes move only along their axis (recommended, matching Vim's `V`).

## Transformations: a map, one verb, one patch

Transformations are data, not verbs. One verb applies them; bindings are that verb with the name filled in.

```js
transformations = {
  trim: v => v.trim(), upper: v => v.toUpperCase(), lower: v => v.toLowerCase(),
  title: v => v.replace(/\b\w/g, c => c.toUpperCase()), slug: v => v.toLowerCase().replace(/\W+/g, "-"),
}                                                   // grows without touching nouns or bindings
sheetTransformations = { fillDown: (sheet, cell) => ..., formula: (sheet, cell, expr) => evaluate(expr, sheet) }

transform: { run: app.transform, params: { name: { type: "string", source: "transformations.list" } } }
bindings: { normal: { "t u": do("transform", { name: "upper" }), "t l": do("transform", { name: "lower" }) } }

app.transform = ({ instance, selection, args }) =>
  app.apply(cellsOf(instance, selection).map(c => ({ at: c, after: run(args.name, c) })))   // one cell or many
```

The palette shows `transform` once; after it (and its noun, or none) the list completes every name from `transformations.list`, and a unique prefix is enough (`transform row up`). The handler dispatches by which map holds the name: text transforms see only the value, sheet transforms see a read view of the whole sheet. Both return the same patch.


patch = { cells: [{ at: "r3:c2", before: "x", after: "X" }] }   // also the undo record
app.apply = patch => { record before; write after; dirty = true; undo.push(patch); grid.refresh() }
```

Cell references for formulas: instance ids (`r3:c2`) internally; `A1` style can be layered on in the evaluator.

## The grid component (the framework gap this app exposes)

There is no grid. The list creates one instance for the selected row only; a grid does the same in two dimensions: one instance for the focused cell, parented to a row instance, parented to the grid, with `row` and `column` as nouns in between. It is built outside the core, implements the same component contract as the picker (delivery, refresh by stable id, scroll in two axes), and owns cursor and viewport. If building it needs something the core does not provide, that is a phase 7 finding.

## What it proves

Mode tables with counts and sequences (`3j`, `dd`, `gg`), shadow-free visual modes with payload, registers shared between editor and grid, per-verb Confirm, the picker for open and save-as, the palette as the index for unbound verbs, a component added without touching the core.
