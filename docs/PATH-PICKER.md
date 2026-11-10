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

Every key is a binding row (`bindings/picker.json`, `bindings/picker-browse.json`), reachable from the palette and the CLI and rebindable. In normal mode (hybrid and columns) the picker's own rows are guarded by `picker-up` (the picker navigates), so a filter picker without navigation keeps the editor's keys.

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

## Compared with Vim

| Vim form | `:help` tag | Here |
|---|---|---|
| `h` `j` `k` `l` | `motion.txt` left-right-motions, up-down-motions | same (netrw: `-` up, `<CR>` into; here `h`/`l` as in Miller-column browsers) |
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
