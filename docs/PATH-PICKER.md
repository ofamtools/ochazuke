# The path picker's styles

Every place that asks for a path opens the same picker (`Runtime.OpenPathChooser`): a form's path field, a parameter of a verb, a mounted extension's path parameter, an application's own directory picks. One setting, `pickers.directory.style`, says how it is drawn. It is read at each open, so the Settings page switches it live, and no caller branches on it.

| Style | What it is | Opens in |
|---|---|---|
| `list` (default) | The sources one at a time (browse, recent, typed, find, the application's); `ctrl+o` between them. The picker as it has always been. | the search line |
| `hybrid` | One list in sections, all on show: Recent, a section for each application source that declares one, and Browse (the folder being looked at). Typing filters every section. | normal mode |
| `columns` | Miller columns: the folder above, the folder, and a preview of the highlighted entry. | normal mode |

## Never confined

The sources (recent, the project's folders, the bases, a declared scope) are starting places. From any row of any source, and from wherever the picker starts, going up goes to the folder above and on, one at a time, to `/`; going into a folder works under it; a path typed or pasted anywhere always works, whether or not any source lists it. A parameter's `Start` and an application's walk roots say where to begin; they never limit what can be reached. Only `path.no-auto-list` (external volumes) holds a folder back, and only from being listed *on its own*: opening it or typing it lists it.

## Keys

Every key is a binding row (`bindings/picker.json`, `bindings/picker-browse.json`), reachable from the palette and the CLI and rebindable. Navigation rows use `picker-up` (the source navigates). `/` and `i` use `picker-normal-mode`, so any picker that opts into normal mode can resume editing its filter, including a noun-list scope without navigation.

| Key (normal mode) | Verb | Does |
|---|---|---|
| `j` `k`, `down` `up` | `picker.move {delta}` | the highlight, a full-width line; spills nowhere (clamped) |
| `g g` / `G` | `picker.move {to: first\|last}` | first / last row (the first and last edges) |
| `0` / `$` | `picker.goto {to: first-col\|last-col}` | first column (home) / last column (the deepest place visited) |
| `h` / `l` | `picker.up` / `picker.into` | the folder above / into the highlighted folder (`columns`: between columns) |
| `~`, `g /` | `picker.goto {to: ~ \| /}` | home, the root |
| `enter` | `picker.enter` | pick the highlighted row (a folder in a folder pick; a file in a file pick; in a file pick a folder opens) |
| `u` | `picker.pick {current}` | use this folder (the one shown); in a file pick the highlighted file |
| `n` | `picker.new` | name a new folder here; `enter` makes it and goes into it, `esc` drops the name |
| `m` / `M` | `picker.mark {all}` | mark / mark all, in a list parameter's picker; `enter` takes the marks in the order marked |
| `.` | `picker.toggle {name: hidden}` | show or hide hidden files (this open only) |
| `]` `[` | `picker.move {sections, delta}` | next / previous section (`hybrid`); headings never take the highlight |
| `/`, `i` | `ochazuke.input.begin` | the search line (filter, or type or paste any path); `esc` returns |
| `o` / `R` / `y` | `picker.open-with` / `picker.reveal` / `picker.yank-path` | open, show in the file manager, copy the path of the highlighted entry (open and open-with need an agent's confirmation) |

The `list` style types in its search line, so the same verbs have `alt+` keys in any mode: `alt+up` up, `alt+down` back to a source's own rows, `alt+h` home, `alt+/` root, `alt+u` use this, `alt+n` new folder, `alt+m` / `alt+a` mark / mark all, `alt+.` hidden, `alt+v` / `alt+e` / `alt+y` open / reveal / copy path. `ctrl+l` goes into the highlighted folder; `enter` does too, and `alt+enter` picks it (`picker.enter`).

## The relation walk

`thing walk` opens this same columns picker over the registered nouns and
their relation graph. It starts at the focused thing when there is one;
otherwise it starts with nouns that have things. Nouns show counts, with the
focused noun first, then the nouns visited recently in this run. A noun's
things and inbound reference sections load bounded pages from stores that
support `Query`; mounted relation-kind sections browse ordered relation
keys, reading only the current window's endpoints. Previous page and Next
page are navigable rows, not a whole-table load. `/` searches across all
matching things, including later relation pages. Searching a mounted
relation kind reads its candidate endpoints individually to find every
match and an exact count; it never asks the extension for its whole table.
If a noun cannot provide its count, its row stays visible with `?` and the
error rather than making the whole noun column fail. It can still be
entered; a list-read failure is shown in that column.

`l` follows a noun or thing; `h` retraces the walk and restores that column's
filter and highlight. The breadcrumb records the route. `gg`/`G` select the
first/last row, and `0`/`$` return to the first/deepest visited column.
The folder keys (`~` home, `g /` root, `n` new folder) are neither hinted
nor answered in the walk: a browse verb is available only where the current
source can do it (`picker-goes`, `picker-makes`).
Related columns show named path/link props, outgoing owner and other
references, and the same inbound reference and relation-kind sections as
the show form. Mounted nouns use their catalog's props and proxy stores.
On its own, `enter` goes to a thing's index, or opens the selected value:
through its noun's free `open` verb with the value's prop explicitly named,
or, when the noun declares an `open` verb of its own, through the opener
(`opener.run`), so that verb keeps its own meaning and arguments.

When a palette command, key or `Ctx.Need` asks for a missing declared
subject, the picker offers nearby candidates when available, the noun's
list, and the walk. `S` steps scopes in normal mode (`alt+s` while editing);
`/` begins filtering. The walk marks a thing of the needed noun, or a path
prop for a path parameter, with `✓`. `enter` takes a match; other things
remain visible and `enter` or `l` walks through them. Even a matching thing
can be explored with `l` before taking it.

A path request is the path picker it always was, with the walk as one more
source, last: the title lists the sources with the current one in brackets
(`Choose file · [browse ~/proj] recent typed find walk`), `ctrl+o` and
`alt+o` step them as in any path picker, the footer offers `ctrl+o` while
typing, and the source last used is remembered. `S` and `alt+s` step the
same list (`PickerSpec.ScopeKeys`: one mechanism, two keys). An
application's own file system source named `walk` reads `on disk · walk`
beside the relation walk and is remembered under its own name. With
`picker.scopes` or `subject.ask` off the walk is not offered and nothing
differs from the path picker.

Noun asks keep the argument form first; choosing its missing field opens
these scopes. Ordinary record-choice editing and already populated fields
keep their existing sources and editing behavior.

Only what nothing at the focus supplies is asked for. A thing on the focus
path of the verb's noun, or of a noun that names that noun's actions by an
alias (a `decision` row under a `question` verb: the same thing seen as
another noun), is the optional subject, for a key and a palette line alike;
so are a parameter's `From` value and a thing of a `Focus` parameter's noun.
A noun the verb also takes as a parameter of its own fills that parameter,
never the verb's own subject.

Asking never changes which verb a key runs. A verb's availability is decided
first, by its own guards: a row whose verb is unavailable at the focus passes
the key on to the next row or level, whatever the verb could ask for. Only an
available verb asks.

`subject.ask` defaults to `true`; `false` restores what every route did
before the ask, exactly: a missing required argument still opens the args
form (the path picker for a lone path), `Ctx.Need` still asks, the palette
still asks for the verb's own `Focus` target, and the other subjects are
left to the verb, which refuses. `picker.scopes=false` leaves the
graph scope out of these requests; standalone `thing walk` is still
available from the palette. `walk.columns` defaults to `3`: parent, current
column and the highlighted thing's next column. `2` shows parent/current;
`1` shows only the current column. Hidden side/preview columns do not read
their stores; revealing them loads their contents. Narrow terminals
collapse to the current column when fewer than three columns are enabled.
These settings do not change the directory picker's existing style.

## Compared with Vim

| Vim form | `:help` tag | Here |
|---|---|---|
| `h` `j` `k` `l` | `motion.txt`, `h`, `j`, `k`, `l` | similar: `j`/`k` move rows; `h`/`l` navigate graph/directory columns, rather than characters |
| `gg` `G` | `gg`, `G` | same: first and last row |
| `0` `$` | `0`, `$` | similar: first and last *column* (home and the deepest place visited), not the line ends |
| `/` | `/` | similar: opens the filter on the search line; it filters, it does not jump |
| `~` | `~` | different: Vim's `~` toggles case; here home, as a shell's `~` is |
| netrw `gh` | `netrw-gh` | similar: `.` shows or hides hidden entries |
| netrw `d` | `netrw-d` | similar: `n` names a new folder |
| netrw `mf` | `netrw-mf` | similar: `m` marks; `enter` acts on the marks |
| netrw `x`, `gx` | `netrw-x` | similar: `o` opens through the opener table (the OS by default) |
| `:cd`, `:e` | `:cd`, `:edit` | similar: a typed or pasted path is the typed source's, with completion |
| `]]` `[[` | `]]`, `[[` | similar: `]` `[` move between sections |
| netrw `-`, `<CR>` | `netrw-browse-maps`, `netrw--`, `netrw-cr` | similar: `h` retraces one step and `l` explores the highlighted thing; in the graph, `enter` takes a matching subject instead of exploring it |

The walk has no direct Vim equivalent: a relation graph may have cycles and
multiple owners, unlike netrw's directory hierarchy. Its breadcrumb records
the route actually taken, not a unique parent path. The directional motions
and first/last rows follow Vim's spatial convention; the column edges,
filtering and subject selection deliberately differ.
