# List view sorting

`z s` sorts a cell-mode table by its current column: ascending, descending,
then off. On a row-select table, list or tree it opens the standard field
picker; picking the same field cycles the same three states. A column's
normalized title names its prop (lower case, spaces replaced with dashes).
If that title does not identify an available prop, the same field picker opens.

The `ochazuke.list` verbs work from keys, the palette and API alike:

| Verb | Arguments | Result |
| --- | --- | --- |
| `sort` | `by: "status,-score"` | Stable multi-key sort, status first, score descending within it |
| `sort` | `by: ""` | Restore the app's order |
| `sort-column` | no direction | Cycle the current column, or choose a field in row mode |
| `sort-column` | `desc: true` or `false` | Set descending or ascending explicitly |
| `sort-clear` | none | Restore the app's order, keeping filters |

Every verb accepts `id`, defaulting to the focused list, tree or picker.
A leading `-` or trailing `!` on a field means descending. Prop aliases resolve
to their declared names. `list-sortable` guards sorting and `z s`.
Clearing an already clear sort succeeds with a plain status message.

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
A picker exposes the same methods and preserves its ordinary-row sort across
`SetRows`. Fixed, trailing and create rows keep their picker roles.

Ordinary view sort lives in memory on the list/controller. Saving a filter
through `list.filter-save` stores conditions and optional sort keys in the
existing `list.filters` setting. Applying it through `filter-saved`, the
saved-filter picker or cycling restores both, retaining `/` text. Older
definitions without sort restore app order. There is no new store or file;
unsaved sort is not restored on restart. The filter form edits the primary sort
key; changing direction preserves secondary keys, including through undo.
`z /` opens it on lists and trees; `ctrl+f` opens it in a picker.
Tab follows the drawing order: sort field, direction, field picker, the
current value control when open, active conditions, save name, then back to
sort. Shift+Tab reverses that order. The form starts on fields, so Shift+Tab
reaches direction and a second Shift+Tab reaches sort. Enter on direction
flips asc/desc. Multi-key sorting is available through `list.sort`.

The compact controls share the standard picker and form record. Type a prop
and Enter to see its values, then type to narrow them. Enter picks one and
returns to fields without removing a value already selected. Escape then
Space toggles several values, and Enter returns to fields. Escape again in
the value picker's normal mode returns without changing the selection.
Space remains search text in insert mode. Counts cover all source rows.
Free text applies as typed; Enter keeps it and returns to fields. Reentering
a text field starts insert mode. On active conditions, Enter edits, `d d`
removes and `!` inverts. `/` shows the ordinary list search line there.

Normal-mode text inputs retain their editing keys: `d d` deletes text.
`u` and `ctrl+r` walk one chain from any stop: a text input's own history
first, then the view's changes; past the end they do nothing. `C` runs
`form.clear` and `D` `filter-form.delete-saved` from any stop, and the palette
offers both wherever the keys work. Cycling saved filters is one undo step. `!` flips a filter
field's is/not. In picker normal mode, `j`, `k`, `g g` and `G` move through
the choices. `i` resumes editing. Ordinary pickers remain pinned in insert;
composed pickers opt into these two modes with `PickerSpec.NormalMode`.
`picker.hidden-count` defaults to true and shows off-screen matching rows
on the query line; false hides the count. `picker.stable-height=false`
disables the compact form's fixed-height padding.

`list.filter-layout` defaults to `compact`; `fields` keeps the previous
one-field-per-prop form. `list.filter-sort` defaults to true and hides the
sort section when false.
`list.filter-clear-hidden-sort` defaults to false: clearing the form preserves
a sort whose controls are hidden. Set it to true to clear that sort too.
`list.filter-form=false` keeps the older prop/value picker flow unchanged.
`list.filter-picker-sort=true` opts into a Sort entry using the existing
sort-field picker; this setting defaults to false. Source-owned views do not offer
sort controls.

The save row is enabled by `list.filter-save` (default true). Tab to its name,
type a name and Enter to save through `list.filter-save`. An existing name
shows **Replace** before Enter overwrites it. The name remains a text input
in normal mode; its editing keys never delete saved filters. From a non-text
control, `D` opens a saved-filter deletion picker, then asks for confirmation.
App defaults remain declarations; deleting an override reveals its default.
Normal `z ]` / `z [` cycle inside or outside the form. Cycling wraps; no saved
filters disables it. Invalid definitions are skipped with the existing notice.
View edits clear the applied-name marker; the typed name stays for easy
replacement. `list.filter-save=false` hides the row without deleting data.

The form adds no Ctrl+Alt chords or direct part-focus shortcuts. Tab replaces
those jumps, direction uses Enter on its control, and normal `!`, `C` and
`d d` on conditions cover the remaining changes. `filter-form.focus`,
`.direction`, `.write-saved` and `.delete-saved` remain callable while a
filter form is open. The modal retains `ochazuke.form` on its ancestry,
so existing user bindings on that noun still apply.

`list.sort-indicator` defaults to `true` in `settings/ochazuke.json`. It adds
`▲` / `▼` to sorted column titles, and `▲2` / `▼2` for the second key (and so
on). Turning it off hides the indicators and leaves the sort active. The
arrows take part in normal column sizing; fixed widths still truncate normally.
Plain lists, trees without columns and pickers show a separate `sort field ▲`
label. It adds no condition or filter count; the same setting hides it.

For virtual windows and partial/source-owned results, pass `NoSort()` to
`AddList` / `AddTree`, or set `PickerSpec.NoSort`. Sorting is unavailable there
and `SetSort` refuses nonempty keys. A picker's live source also owns its row
order and cannot sort locally. Saved filters still apply their conditions here;
the view ignores saved sorting while its source owns the order.
Hosts must declare partial views explicitly: a list cannot infer that the
rows it was given are only a source's window.
A future source-side view handler can provide whole-source sorting.

## Compared with Vim

| Vim form | Help | Ochazuke |
| --- | --- | --- |
| `:sort` | `:help :sort` | Similar ordering; `list.sort` is a stable view over typed props, while Vim edits buffer lines. Clearing restores app order. |
| `:sort!` | `:help :sort` | Descending is `list.sort {by: "-field"}` or `sort-column {desc: true}`. Blanks remain last. Vim reverses buffer-line order, including blanks. |
| `zs` | `:help zs` | Different: Vim scrolls horizontally; ochazuke already scrolls columns with `z h` / `z l`, so `z s` cycles view sorting. |
| `dd`, `u`, `CTRL-R` in a text input | `:help dd`, `:help u`, `:help CTRL-R` | Same text deletion, undo and redo. A save name is text, not a saved-filter deletion command. |

No bare `:sort` / `:sort!` aliases or standard store-list CLI sort parameters
are added in this scope. The explicit list verbs are available through the
existing command paths.
