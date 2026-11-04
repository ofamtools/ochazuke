# List view sorting

`z s` sorts a cell-mode table by its current column: ascending, descending,
then off. On a row-select table, list or tree it opens the standard field
chooser; picking the same field cycles the same three states. A column's
normalized title names its prop (lower case, spaces replaced with dashes).
If that title does not identify an available prop, the same field chooser opens.

The `ochazuke.list` verbs work from keys, the palette and API alike:

| Verb | Arguments | Result |
| --- | --- | --- |
| `sort` | `by: "status,-score"` | Stable multi-key sort, status first, score descending within it |
| `sort` | `by: ""` | Restore the app's order |
| `sort-column` | no direction | Cycle the current column, or choose a field in row mode |
| `sort-column` | `desc: true` or `false` | Set descending or ascending explicitly |
| `sort-clear` | none | Restore the app's order, keeping filters |

Every verb accepts `id`, defaulting to the focused list, tree or chooser.
A leading `-` or trailing `!` on a field means descending. Prop aliases resolve
to their declared names. `list-sortable` guards the sort verbs and `z s`;
`list-sorted` guards clearing an active sort. Existing actions stay available.

Sorting reuses the props offered by prop filtering, declared or derived.
Secrets, `NoFilter`, `PropsOff`, `list.props-off` and prop guards also apply.
A row without a prop record can sort by its titled columns, including a row
whose `Data` is an opaque ID. Column fallback supplies text, or the declared
prop's kind when its name matches; it does not add filter fields.

Enums follow their declared order, numbers compare numerically (including
large integers), and durations and booleans compare by value. Text props
containing only ISO dates or `time.Time` values compare chronologically,
including timezone offsets. Other text compares without case. Mixed date/text
props compare entirely as text. Blanks go last in either direction. Equal
keys keep the app's order. A tree sorts siblings, keeping subtrees together.

Sectioned tables sort within each contiguous section by default, preserving
section order and headings. `list.sort-sections=off` disables local sorting
on sectioned lists; `within` restores it. The rule applies to every list,
not to particular nouns or applications.

Sorting runs after filtering. `Items()` and the app's source rows retain their
order; selection, marks and focus follow IDs. `SetItems`, `Patch`, `SetColumns`
and a tree's `SetNodes` keep the view sort and apply it to the replacement
rows. `Sort()` returns a copy of the keys; `SetSort([]SortKey)` replaces them.
A chooser exposes the same methods and preserves its ordinary-row sort across
`SetRows`. Fixed, trailing and create rows keep their chooser roles.

Ordinary view sort lives in memory on the list/controller. Saving a filter
through `list.filter-save` stores conditions and optional sort keys in the
existing `list.filters` setting. Applying it through `filter-saved`, the
saved-filter picker or cycling restores both, retaining `/` text. Older
definitions without sort restore app order. There is no new store or file;
unsaved sort is not restored on restart. The filter form offers one sort key at its top. `z /` opens it on lists
and trees; `ctrl+f` opens it in a chooser. Tab and Shift+Tab move between
controls. From the initial field picker, Shift+Tab reaches sort; type a prop
and Enter to apply it. Shift+Tab twice from fields reaches direction; Enter
flips asc/desc. Multi-key sorting remains available through `list.sort`.

The compact controls share the standard chooser and form record. Type a prop
and Enter to see its values, then type to narrow them. Enter picks one and
returns to fields; Escape then Space toggles several values and Enter
returns. Space remains search text in insert mode. Counts
cover all source rows. Free text applies as typed; Enter keeps it and returns.
From fields, Tab reaches active conditions and the next Tab reaches the save
name. On active conditions, Enter edits, `d d` removes and `!` inverts.

In an input, Escape enters normal mode. `!` flips the selected field's is/not;
`C` runs `form.clear`, clearing sort and every condition, including conditions
the fields cannot represent. `i` resumes editing; capital C and ! remain text
while inserting. Normal `u` / `ctrl+r` undo / redo written changes. Escape in
normal mode closes with changes kept. These composed choosers opt in through
`ChooserSpec.NormalMode`; ordinary choosers keep their default pinned input
and single-Escape close behavior.

`list.filter-layout` defaults to `compact`; `fields` keeps the previous
one-field-per-prop form. `list.filter-sort` defaults to true and hides the
sort section when false.
`list.filter-clear-hidden-sort` defaults to false: clearing the form preserves
a sort whose controls are hidden. Set it to true to clear that sort too.
`list.filter-form=false` keeps the older prop/value chooser flow unchanged.
`list.filter-picker-sort=true` opts into a Sort entry using the existing
sort-field chooser; this setting defaults to false. Source-owned views do not offer
sort controls.

The save row is enabled by `list.filter-save` (default true). Tab to its name,
type a name and Enter to save through `list.filter-save`. An existing name
shows **Replace** before Enter overwrites it. In the name input, Escape then
`d d` deletes through `filter-saved-delete`. App defaults remain declarations;
deleting a user override reveals the default. The row shows the applied name.
Normal `z ]` / `z [` cycle inside or outside the form. Cycling wraps; no saved
filters disables it. Invalid definitions are skipped with the existing notice.
View edits clear the applied-name marker; the typed name stays for easy
replacement. `list.filter-save=false` hides the row without deleting data.

The form adds no Ctrl+Alt chords or direct part-focus shortcuts. Tab replaces
those jumps, direction uses Enter on its control, and normal `!`, `C` and
`d d` cover the remaining changes. `filter-form.focus`, `.direction`, `.save`
and `.delete-saved` remain available as verbs, including in the palette.

`list.sort-indicator` defaults to `true` in `settings/ochazuke.json`. It adds
`▲` / `▼` to sorted column titles, and `▲2` / `▼2` for the second key (and so
on). Turning it off hides the indicators and leaves the sort active. The
arrows take part in normal column sizing; fixed widths still truncate normally.
Plain lists, trees without columns and choosers show a separate `sort field ▲`
label. It adds no condition or filter count; the same setting hides it.

For virtual windows and partial/source-owned results, pass `NoSort()` to
`AddList` / `AddTree`, or set `ChooserSpec.NoSort`. Sorting is unavailable there
and `SetSort` refuses nonempty keys. A chooser's live source also owns its row
order and cannot sort locally. Hosts must declare partial views explicitly:
a list cannot infer that the rows it was given are only a source's window.
A future source-side view handler can provide whole-source sorting.

## Compared with Vim

| Vim form | Help | Ochazuke |
| --- | --- | --- |
| `:sort` | `:help :sort` | Similar ordering; `list.sort` is a stable view over typed props, while Vim edits buffer lines. Clearing restores app order. |
| `:sort!` | `:help :sort` | Descending is `list.sort {by: "-field"}` or `sort-column {desc: true}`. Blanks remain last. Vim reverses buffer-line order, including blanks. |
| `zs` | `:help zs` | Different: Vim scrolls horizontally; ochazuke already scrolls columns with `z h` / `z l`, so `z s` cycles view sorting. |

No bare `:sort` / `:sort!` aliases or standard store-list CLI sort parameters
are added in this scope. The explicit list verbs are available through the
existing command paths.
