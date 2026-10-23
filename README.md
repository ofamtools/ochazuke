# Ochazuke

A Go application framework on Charmbracelet Bubble Tea v1.3, with retained pages, default Lip Gloss styling, partial-screen modals, layered actions and bindings, modal text editing, a nested command palette, and live CLI control.

Status: working standalone framework and demo. Neither gmtm nor Matomeru is changed. Existing editors are retained; Bubbles provides the new page/modal scrolling viewports.

## Explore the architecture

Open the [interactive architecture field guide](docs/architecture.html) locally:

```sh
open docs/architecture.html
```

No server, install, or internet connection is needed. Search 12 components, inspect their responsibilities and source locations, step through keyboard/CLI/picker/modal journeys, and experiment with retained modal state. The state lab illustrates ownership; it does not connect to a running Go application. The linked [architecture map](docs/architecture-map.html) supports node exploration and light/dark themes; its editable specification is [architecture-map.json](docs/architecture-map.json).

The guide links to four generated pages: [Actions](docs/actions.html), [Default bindings](docs/bindings.html), [Go API](docs/api.html), and [CLI & socket](docs/transport.html). Each page has separate navigation links; the action, binding, and Go API pages have their own search. Actions and bindings reflect a fresh demo, not a running application's customized registry. Editor grammar shortcuts remain in the [keyboard guide](#keyboard-guide).

Regenerate after changing registrations or Go declarations/comments:

```sh
go run ./cmd/docgen
```

Go's built-in documentation also works directly in the Ochazuke project directory:

```sh
go doc
go doc -all .
go doc Runtime.Execute
```

The HTML API catalog uses the standard `go/doc` library, with no documentation-server dependency. Edit `cmd/docgen/main.go` for the layout; do not hand-edit the four generated pages. Keep all four HTML files beside `architecture.html` when copying the guide.

## Run on macOS

Requires Go 1.25 or newer.

```sh
go run ./cmd/ochazuke run
```

For a reusable executable:

```sh
go build -o /tmp/ochazuke ./cmd/ochazuke
/tmp/ochazuke run
```

In another terminal:

```sh
/tmp/ochazuke operations list
/tmp/ochazuke page open --json '{"id":"playground"}'
/tmp/ochazuke input undo
/tmp/ochazuke input undo --json '{"steps":3}' --target directory
/tmp/ochazuke state get --json '{"path":"/inputs/directory/text"}'
```

Global `--socket PATH` goes before `run` or `NOUN VERB`. It selects a specific application instance. The default socket on macOS is `~/Library/Caches/ochazuke/app.sock`. `run --headless` uses the same Bubble Tea event loop without terminal rendering.

### Keybinding sandbox

Run `ochazuke keybinding` (or `go run ./cmd/ochazuke keybinding`) without starting the demo or socket server. The left column shows the latest key message; the right column logs messages newest first, retaining the last 200 and showing as many as fit the terminal.

Names come directly from Bubble Tea's `tea.KeyMsg.String()`, quoted to make whitespace visible. Ctrl-C is reserved for exit. Keys that the terminal encodes identically cannot be distinguished by the sandbox.

Define dummy actions with repeatable `--bind 'KEYS=ACTION'` options:

```sh
go run ./cmd/ochazuke keybinding \
  --bind 'ctrl+s=save' \
  --bind 'g g=go-top' \
  --bind 'alt+x=alternate'
```

These are real Ochazuke app-scope bindings dispatched through `Runtime.HandleBinding` and the operation registry. Each dummy action prints `ACTION: <identifier>` in the sandbox's latest-result area and history, without performing any app operation. The left column lists the effective bindings; history labels unmatched keys, pending sequences, and cancelled sequences. A later definition of the same key sequence replaces the earlier one.

Coalesced ordinary typing is split into keys, like the app's dispatcher, so multi-key sequences work during rapid typing. Bracketed paste remains one logged message and does not trigger bindings. Escape cancels a pending sequence.

## Full-project example

[`examples/full-project`](examples/full-project/README.md) is a standalone application assembled from separate configuration, action, binding, and page modules. Its JSON files define pages, nested interaction layers, inputs, actions, key bindings, modals, modes, and appearance. Go registries supply the handlers and renderers named by that configuration.

```sh
go run ./examples/full-project
go run ./examples/full-project --config ./examples/full-project/config
go run ./examples/full-project --smoke
```

The default configuration is embedded in the executable. `--config` reads an editable directory instead. The smaller [`examples/core`](examples/core/main.go) remains a headless example of action fallback, modal restoration, and isolated undo.

## Pages and default presentation

`run` launches the demo, not an application you must adopt. It starts on **Overview**. The framework accepts your own page IDs, renderers, state, inputs, and registered actions.

| Demo page | Purpose |
|---|---|
| Overview | Navigation and a short framework tour |
| Playground | Text, path and multiline paths fields, shared yanks, nested drafts, path picker |
| Commands | Live operation catalog, argument schemas, and remembered Playground bindings |
| Appearance | Live theme and density controls: `t` cycles theme, `d` toggles density |

In normal mode, `Ctrl-J` advances and `Ctrl-K` goes back. The demo does not bind Alt-number shortcuts.

These are suggested starter/demo pages. Domain pages such as Agents, Panes, Projects, and application Settings belong to consuming applications; the framework does not require those names or behaviors.

### Registering your own pages

`Page` retains a base `Frame` and a renderer. Its context starts with `["app", page.ID]`. Register the page's layer and any inputs before registering the page:

```go
r := ochazuke.NewRuntime()
r.Layers["home"] = &ochazuke.Layer{ID: "home", Noun: "page"}
err := r.RegisterPage(&ochazuke.Page{
    ID: "home",
    Title: "Home",
    Frame: &ochazuke.Frame{
        ID: "home",
        Context: ochazuke.Context{Path: []string{"app", "home"}},
        State: map[string]any{},
    },
    Render: func(r *ochazuke.Runtime, width, height int) string {
        return "Welcome to your application."
    },
})
if err != nil { panic(err) }
if err := r.OpenPage("home"); err != nil { panic(err) }
model := ochazuke.NewModel(r)
if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
    panic(err)
}
```

Registration does not activate a page. Switching reuses the page's exact frame and input instances: selection, focus, modes, drafts, undo history, and scrolling survive a round trip. Normal-mode `Ctrl-J/K` cycles pages. Page-specific bindings replace those from the previous page through the existing layer resolver. Page tabs have no numeric shortcuts; use the palette to search large page sets.

Ordinary modals block page switching. The palette closes before dispatching a selected operation, so choosing `open → page` works when no other modal is underneath it. Focusing an inactive page's input does not silently navigate; open that page first. Explicitly targeted editing operations may still update an offscreen input without moving focus.

```sh
/tmp/ochazuke pages list
/tmp/ochazuke page open --json '{"id":"commands"}'
/tmp/ochazuke page next
/tmp/ochazuke page previous
/tmp/ochazuke state get --json '{"path":"/activePage"}'
/tmp/ochazuke state get --json '{"path":"/pages/playground/frame/context"}'
/tmp/ochazuke surface scroll --json '{"lines":8}'
```

### Styling and modal behavior

- Default styling uses restrained teal/green accents, rounded panels, heavier focused borders, page tabs, a mode badge, and a status line.
- Themes: `adaptive`, `dark`, `light`, `mono`. Density: `comfortable` or `compact`. Adaptive follows the terminal's detected background; `NO_COLOR` disables theme colors. Focus remains visible through border weight and a tab marker.
- Modals are centered, content-sized panels, capped at 78 columns and about 75% of the terminal height. They leave a visible, subdued perimeter of the previous screen whenever the terminal can accommodate it. Nested modals retain the previous modal and page underneath.
- Tiny terminals use reduced chrome rather than overflowing the screen. Bubbles viewports handle overflowing page and modal content; `PgUp/PgDn` scroll the active surface.
- In an input modal, Escape returns insert mode to normal; another Escape closes the modal. A pending normal-mode motion/count is canceled before closing. `Ctrl-G` closes demo drafts directly. Palette and picker cancellation retain their own documented behavior.
- App-specific modal rendering is registered through `Model.ModalViews[frameID] = ModalView{Title, Render}`. The framework owns positioning and composition; the renderer receives the modal frame and available content dimensions.
- Render callbacks may return trusted Lip Gloss ANSI styling. Sanitize external text before rendering it; built-in input and filesystem labels already do so.
- Input cursors invert the existing grapheme rather than inserting a marker, so moving the cursor does not shift following text. Empty positions use a styled blank cell. Wide/combining graphemes, terminal-cell clipping, and visual-selection styling share the same renderer.

```sh
/tmp/ochazuke themes list
/tmp/ochazuke appearance get
/tmp/ochazuke appearance set --json '{"theme":"dark","density":"compact"}'
```

Theme and density changes are atomic and apply immediately to all pages and modals. They are in-memory settings, not persisted preferences. `NewStyles(*model.Appearance)` supplies the same style vocabulary to application renderers.

### Keyboard guide

The demo starts on Overview in normal mode. Open Playground with `Ctrl-J`; Tab/Shift-Tab cycle its directory, notes, and paths fields. Overview, Commands, and Appearance support `j/k` and up/down scrolling. Demo page shortcuts are normal-mode-only, so insert-mode `Ctrl-K` still kills to line end.

Macro recording consumes exactly one register character after `q`. For `qq:`, the first `q` prompts for a register, the second starts recording into `q` and displays `REC @q`, and `:` opens the palette. A normal-mode `q` stops recording. Invalid register input reports an error but does not trap later commands in register selection.

| Context | Keys | Behavior |
|---|---|---|
| Normal input | `h j k l`, arrows | Rune/line movement |
| Normal input | `w b e`, `0 $`, `gg G` | Word, line, and document motions |
| Normal input | `i a I A` | Insert here, after cursor, at first nonblank, or at line end |
| Multiline normal input | `o O` | Open a line below/above and insert |
| Normal input | `x`, `u`, `Ctrl-R` | Delete, undo, redo |
| Normal input | Numeric prefix | Count for motions, delete, undo/redo; `Ngg`/`NG` select a line |
| Normal input | `v`, `V`, `Ctrl-V` | Character, line, or block visual selection |
| Normal input | `yy`, `p`, `P` | Yank a line; paste after/before |
| Normal app | `Ctrl-Y` | Open shared yank history |
| Normal input | `"a` | Select named register `a` for the next operation |
| Normal app/input | `qa … q`, `@a`, `@@` | Record macro `a`, replay it, repeat the last macro |
| Insert input | `Ctrl-A/E`, `Ctrl-B/F` | Line start/end, previous/next rune |
| Insert input | `Ctrl-P/N`, `Alt-B/F` | Previous/next line, previous/next word |
| Insert input | `Ctrl-W/U/K`, `Ctrl-Y` | Kill path component/to line start/to line end; paste the app's current yank |
| Insert input | Backspace, Delete/`Ctrl-D`, Enter | Delete backward/forward; newline in multiline inputs |
| Insert input | Escape | Return to normal mode |
| Normal input | Escape | Cancel a pending motion/count |
| Normal app/input | `:` | Open palette in search insert mode |
| Any editing mode | `Ctrl-Space` | Open palette without stealing Readline `Ctrl-P` |
| Normal Playground | `Ctrl-O` | Open the shared picker using the field's filter |
| Normal path/paths field | Enter | Open picker using the current value/line |
| Normal Playground / draft | `Ctrl-N` | Open a draft / nest another draft |
| Draft modal | `Ctrl-G` | Close and restore the parent |
| Normal Playground | `F2` | Inspect the handling layer |
| Normal app | `?` | Open the scrollable keyboard guide |
| Normal Appearance | `t`, `d` | Cycle theme / density |
| Normal demo | `Ctrl-J/K` | Next/previous page |
| App or modal | `PgUp/PgDn` | Scroll the active surface |
| App | `Ctrl-C` | Quit (`q` is reserved for macro recording) |

On terminals where Option does not send Meta sequences, configure Option-as-Meta to use `Alt-B/F`. `Ctrl-Space` is received as `ctrl+@` by Bubble Tea. These shortcuts are registered bindings, not hard-coded global overrides.

### Palette and selectors

- The palette opens with its search field focused in insert mode: type immediately.
- Its root displays the actual **VERBS** and **NOUNS** lists side by side. In normal mode, `h/l` and left/right switch columns; `j/k` and up/down move through results; Enter chooses.
- Choosing a verb lists compatible nouns. Choosing a noun lists its verbs and any configured instances. Moving into a nested list preserves search mode and clears the previous query, so typing immediately filters the new options. For `page`, the instances are the live pages; selecting one executes `open.page` with its ID. Search matches page titles and IDs, so navigation does not depend on numbered tabs.
- `/` enters search when browsing a list. Search and argument fields use the same Vim/Readline editor and independent undo trees as page inputs. Escape first returns an editing field to normal mode; subsequent Escape goes back.
- Argument forms show required/optional fields. Tab/arrows change fields; Enter advances and then executes. Empty optional fields are omitted. `set palette-argument` can supply an explicit empty string; `clear palette-argument` omits one.
- The originating interaction context is captured before the palette opens. Selecting `undo → input` edits the originating input, not the palette's search field.
- `Ctrl-C` cancels the palette. Errors retain the form and its drafts.
- Instance sources explicitly declare their default verb and ID argument. The palette does not infer a default from operation order. The [full-project example](examples/full-project/README.md) configures these defaults in JSON.
- The shared path picker opens with its path field in insert mode. `Ctrl-N/P` moves through results without moving the text cursor. Other Readline bindings still edit the path; `/` and whitespace delimit `Ctrl-W` kills, while dots and hyphens remain part of a filename.
- Tab completes the highlighted result into the picker's path input without confirming or closing it. Directories receive a trailing separator for continued browsing; the originating field stays unchanged until confirmation.
- Escape first enters normal mode. There, `h/left` browses the parent, `l/right` enters a directory, and `j/k` match up/down. Enter opens a directory/selects a file, and `Ctrl-S` confirms a path. Escape in normal mode cancels.
- Results recursively fuzzy-match the full relative path. Native Git supplies `.gitignore` semantics, including nested rules, negations, repository excludes, and global excludes; Git must be on `PATH`. Non-repositories use temporary Git metadata outside the browsed tree. `.git` and directory symlinks are never traversed.
- The picker publishes immediate children first, then builds the recursive index in the background. Listed paths remain selectable and Tab-completable while indexing continues. Query edits search the available index; recursive results preserve the highlighted path when it still matches. Canceled or stale results cannot overwrite newer results. A recursive failure keeps usable shallow results and shows `Index incomplete`.
- Leading `~` and `~/` resolve to the home directory. Explicitly opening a directory excluded by an ancestor repository's ignore rule makes that directory a fresh ignore boundary, so a home-repository rule such as `proj/` does not hide every project. Root-local, nested, and global ignore rules still apply.
- `Ctrl-S` confirms the highlighted result; an empty query includes a current-directory entry for `any`/`dir`. The action `choose.picker {current:true}` explicitly chooses the root. Changing the originating text while the picker is open rejects selection instead of replacing stale offsets.

#### Configuring noun instances

`NewPalette` supplies live page instances with `open.page` as their default. Applications can replace that configuration or register sources for their own nouns:

```go
err := model.Palette.RegisterNoun("page", ochazuke.PaletteNoun{
    Source: ochazuke.PagesPaletteSource,
    DefaultVerb: "open",
    Argument: "id",
})
if err != nil { return err }
```

A `PaletteSource` receives the runtime and originating context and returns `[]PaletteInstance`, each with an `ID` and `Label`. Selecting an instance passes its ID to the configured argument, then dispatches through the normal operation path. The source is queried live so newly registered pages appear without rebuilding the palette.

For nouns with sources, `select.palette` accepts `instance:<id>` or `verb:<verb>`. Its published `entries` distinguish item kinds and labels. For example, `noun:page` followed by `instance:playground` opens Playground without an argument form; `verb:open` still exposes the normal form.

### Path fields

One picker handles files and directories. Field kind determines replacement behavior; its separate filter controls eligible results:

```go
directory := ochazuke.NewInput("directory", "/tmp", false)
directory.Kind = "path"
directory.PathFilter = "dir"

paths := ochazuke.NewInput("paths", "/tmp\n.", true)
paths.Kind = "paths"
paths.PathFilter = "any"
```

| Field kind | Picker starting context | Selection result |
|---|---|---|
| Empty / `text` | Working directory or explicitly supplied path | Insert at the cursor captured when opened |
| `path` | Entire field value | Replace the entire value |
| `paths` | Current line | Replace only that captured line, preserving the rest |

Filters are `file`, `dir`, and `any`. A `paths` field is an ordinary multiline editor: motions, `o/O`, visual selection, registers, yanks, macros, and undo operate on its text. It does not serialize a JSON array or use a separate chip/list widget. Filenames containing a newline cannot be represented as one path per line and are rejected rather than silently splitting the set.

```sh
/tmp/ochazuke picker open --json '{"filter":"file","path":"/work"}'
/tmp/ochazuke picker choose
/tmp/ochazuke picker close
```

Applications constructing their own picker register it with the model's event loop:

```go
picker := ochazuke.NewPicker(r)
model.Pickers = append(model.Pickers, picker)
model.ModalViews["picker"] = ochazuke.ModalView{
    Title: "Choose a path",
    Render: func(_ *ochazuke.Runtime, _ *ochazuke.Frame, w, h int) string {
        return picker.View(w, h)
    },
}
```

### API and ownership

Every keyboard action dispatches a registered operation. The CLI is a generic `NOUN VERB` adapter over that same registry; it has no separate list of supported actions. `operations list` returns the current catalog and argument specifications.

```go
r := ochazuke.NewRuntime()
input := ochazuke.NewInput("title", "Example", false)
if err := r.AddInput(input, []string{"app"}); err != nil {
    panic(err)
}
if err := r.Focus("title"); err != nil {
    panic(err)
}
result, err := r.Execute(ochazuke.Request{
    Verb: "undo", Noun: "input", Target: "focused-input",
    Args: ochazuke.Arguments{"steps": 1},
})
```

`Runtime` is single-owner state. In a running application, call it from Bubble Tea's update loop; external goroutines/processes use `Call`/`CallMsg`, not concurrent `Execute` calls. `ExecuteAt` accepts a captured context for explicit originating-target dispatch. `Input.SetMode` is the mode transition hook; do not assign `Input.Mode` directly.

| File | Responsibility |
|---|---|
| `runtime.go`, `configuration.go` | Registry, layer resolution, contexts, modal stack, mode/binding configuration, JSON inspection |
| `input.go`, `editing.go`, `editor*.go` | Input-owned undo, Vim/Readline keys, operators, visual geometry |
| `registers.go`, `macros.go`, `yank_history.go` | App-owned yanks/registers, key recording/replay, history modal |
| `palette.go`, `palette_editing.go`, `palette_nouns.go` | Verb/noun and live instance navigation, configured defaults, typed arguments, originating context |
| `picker.go`, `picker_search.go` | Contextual path selection, asynchronous fuzzy indexing, native Git ignores |
| `pages.go` | Page registration, retained frames, navigation, and focus isolation |
| `theme.go`, `surface.go` | Appearance operations, Lip Gloss styles, bounded modal composition |
| `model.go` | Bubble Tea adapter, page shell, Bubbles viewports, modal renderers |
| `transport.go`, `cli.go`, `cmd/ochazuke` | Private Unix socket and generic CLI dispatch |
| `demo.go`, `examples/core` | Runnable UI and noninteractive foundation example |

Local checks:

```sh
go test ./...
go test -race ./...
go run ./examples/core
```

The integrated implementation was checked locally on macOS with the test suite and live terminal/CLI sessions. Checks covered retained page edits and scroll, page-specific bindings, blocked navigation under modals, nested restoration, palette-driven page navigation, live appearance changes, and a narrow 56×18 modal. Regression tests and a live rapid-input check cover Bubble Tea's coalesced typing events without treating bracketed paste as commands.

The editor/picker expansion also passed the full suite and race detector. Live terminal/CLI checks exercised all three visual shapes, cross-field block paste and undo, named registers, counted macro replay, history selection without insertion, `o/O`, slash-delimited kills, contextual path replacement, recursive fuzzy ignore-aware picking, and a 48×15 layout.

## Requirements

- General UI infrastructure with a stack of stateful modals.
- First-class pages retaining their interaction state across navigation.
- Styled partial-screen modal overlays that leave the preceding screen visible.
- Pushing a modal preserves the previous modal's state; popping restores the previous interaction exactly.
- Vim-style interaction with arbitrary, application-defined modes.
- Customizable keybindings scoped to modes, components, or the application, plus global bindings.
- Public operations available through an API and CLI tools, including a central CLI with subcommands.
- JSON export of application state and individually addressable public values.
- Nested command palette with two top-level lists: verbs and nouns. Either can be chosen first.

## Stateful modal stack

Preserve modal instances rather than destroying and reconstructing them. Each frame owns its interaction state: mode, focused field, draft values, cursor, selection, and scroll position.

Illustrative state (JavaScript notation, not a Go API contract):

```js
{
  id: "edit-task",
  component: "task-editor",
  mode: "insert",
  focusedField: "description",
  values: {
    title: "Build UI framework",
    description: "Preserve nested modal state"
  },
  cursor: { row: 1, column: 12 },
  selection: null,
  scroll: { x: 0, y: 3 }
}
```

Opening a directory picker pushes another frame. Closing it restores the editor's interaction state. A child can return a result to its parent, such as a selected directory.

Invariants:

- Only the top frame receives keyboard input.
- Covered frames retain local editing state.
- Background results target the originating frame, not whichever modal is currently on top.
- Shared application data is separate from modal-local state. Pop restores an interaction, not a stale copy of the entire database.
- In-memory preservation and persistence across process restarts are separate capabilities. Restart persistence is not yet specified.

## Extensible interaction modes

Modal windows and editing modes are different concepts. A modal is a stack frame; a mode determines input behavior within a scope.

```js
{
  modes: ["normal", "insert", "visual", "command", "directory-search"],
  applicationMode: "normal",
  activeFrame: {
    id: "edit-task",
    mode: "insert"
  }
}
```

Applications and extensions register mode names; the framework does not restrict them to a fixed enum.

Text fields, single-line text inputs, and multiline text areas support the basic Vim motions and Readline bindings listed above. Normal mode interprets Vim keys; insert mode interprets Readline keys. Full Vim compatibility is not implied.

## Input-local editing and undo

Each text input owns its editing state and undo tree. Histories are independent between fields and remain part of preserved modal-local state. An insert session is one undo change, including kills/yanks and open-line creation. Editing after undo creates a new branch without deleting alternatives. `redo.input` accepts optional `steps` and a zero-based `branch` index for its first step; omitted `branch` selects the newest child. API edits outside insert mode create separate changes.

Inputs expose editing verbs through the shared operation registry. In particular, `undo` accepts an optional number of steps, defaulting to one change. The input handles its own history; outside callers do not manipulate the tree.

The application supports dispatching an editing operation to the focused input without requiring callers to know its identity or implementation. JSON requests:

```js
[
  { target: "focused-input", verb: "undo", noun: "input" },
  { target: "focused-input", verb: "undo", noun: "input", args: { steps: 3 } }
]
```

The first request undoes the focused input's last change; the second requests three undo steps from that same input's history. These operations are available through the shared API and CLI as well as UI bindings. Normal/insert modes determine keyboard interpretation, not whether an external caller can invoke the input's editing verbs.

## Shared yanks, registers, and macros

Selection and undo belong to each input. Yanks, history, registers, and macros belong to `Runtime`, so a yank from notes can be pasted into paths or a modal editor.

```js
{
  registers: {
    '"': { yank: { text: "ab\nef", shape: "block", width: 2 } },
    a: { yank: { text: "alpha", shape: "char" } }
  },
  yankHistory: [
    { text: "ab\nef", shape: "block", width: 2 },
    { text: "alpha", shape: "char" }
  ]
}
```

- `v`, `V`, and `Ctrl-V` select characters, whole lines, and display-cell rectangles. Motions extend selections; `y/d/c/p` yank/delete/change/replace. Escape cancels selection. Normal `yy/dd/cc` act on whole lines; motion operators and counts work too.
- `p/P` preserve the yank's shape. Block operations handle ragged rows and wide glyphs; block change repeats a contiguous first-row insertion across selected rows when leaving insert mode. Each block edit is one undo step. Single-line inputs reject line/block-shaped pastes rather than flattening them.
- `"a` selects register `a` for the next operation; `"A` appends. The unnamed register is shared; `0` holds the last yank, `1`–`9` hold line/multiline deletions, and `-` holds small deletions. `"_` discards without changing shared yanks or history.
- Normal `Ctrl-Y` or `open.yank-history` opens history. `j/k` or arrows highlight an entry; Enter makes it the unnamed yank and closes **without inserting**. Then use `p/P` or insert-mode `Ctrl-Y` to paste. Escape cancels. History keeps the newest 100 entries, including duplicates.
- `qa … q` records into register `a`; `qA` appends. `@a` replays, `3@a` repeats three times, and `@@` repeats the last macro. Mode changes, bindings, focus changes, and modal/palette interactions use the same dispatcher as live keys. Register yanks and macro recordings share storage: replacing one replaces the other.
- Macro replay is bounded to 16 nested calls and 10,000 dispatched keys. Errors are reported, not swallowed; earlier edits remain undoable rather than being rolled back as a transaction. Bare `Runtime` replay requires a `NewModel` dispatcher.

```sh
/tmp/ochazuke yank-history open
/tmp/ochazuke yank-history select --json '{"index":0}'
/tmp/ochazuke register get --json '{"name":"a"}'
/tmp/ochazuke register set --json '{"name":"a","text":"shared","shape":"char"}'
/tmp/ochazuke input paste --json '{"register":"a"}'
/tmp/ochazuke macro replay --json '{"register":"a","steps":2}'
```

`yank.input` captures a selection/current lines; `paste.input` inserts a stored yank. Insert `Ctrl-W/U/K` publish shared kills and `Ctrl-Y` pastes the unnamed text literally. These operations use the same registry as the keyboard, palette, and CLI. All this state is in memory.


## Scoped, customizable keybindings

Bindings invoke registered requests instead of embedding application behavior in key handlers. Each layer owns its binding list:

```js
{
  id: "directory",
  noun: "directory",
  bindings: [
    {
      mode: "normal",
      keys: "U",
      request: { verb: "undo", noun: "input", args: { steps: 3 } }
    },
    {
      mode: "insert",
      keys: "esc",
      request: { verb: "set", noun: "mode", args: { mode: "normal" } }
    }
  ]
}
```

`set.binding` updates a layer's mode/key pair at runtime. Its `request` argument is a JSON-encoded request string. `list.bindings` reports effective layer bindings; if none matches, the input's built-in Vim/Readline interpreter handles the key through the same operation registry.

Bindings and actions compose in layers along the current selection path. The application is always the outermost layer. Apply each more-specific layer on top, overriding matching bindings and action names while retaining everything it does not override.

Illustrative selection path (JavaScript notation, not an API contract):

```js
{
  layers: ["app", "panes-page", "agent", "directory"],
  lookupOrder: ["directory", "agent", "panes-page", "app"]
}
```

Here, a directory in the selected agent's details is the most-specific noun. Its bindings and actions take precedence over the agent's, the page's, and the app's. If the selected parent is a terminal instead, that layer is `terminal`; the same rule applies.

For each invoked binding or action, use the first definition found from innermost to outermost. Missing definitions fall through to the parent layer, all the way to the app. An inner layer does not replace the entire outer binding or action set. Once a definition is found, it handles the invocation; fallback is for absent definitions, not failed actions.

The path follows the active page and nested selected/focused nouns, not a fixed list of component types. Changing the selection changes the effective layers. Modal and input scopes participate in that active interaction path; covered modal frames do not receive input.

Within each layer, mode-specific bindings precede mode-independent bindings, and user configuration overrides defaults in the same layer. Layer precedence comes first: an inner layer's binding wins over an outer layer's binding even if the outer binding is mode-specific. App-level bindings are fallback definitions, not unconditionally reserved shortcuts.

Multi-key sequences such as `g g` require pending-sequence state, cancellation, and defined incomplete-sequence behavior. Unmatched text in insert mode is delivered to the focused editor.

## Nested verb/noun command palette

The palette's main options are just a list of verbs and a list of nouns, rather than a flat list of complete commands.

- Choosing a verb opens a nested list of nouns that support that verb.
- Choosing a noun opens a nested list of verbs associated with that noun.
- Both paths select the same registered operation; choosing the order does not change its meaning.

Illustrative relationships and navigation (JavaScript notation, not a Go API contract):

```js
{
  operations: [
    { verb: "create", noun: "task" },
    { verb: "edit", noun: "task" },
    { verb: "edit", noun: "project" }
  ],
  root: {
    verbs: ["create", "edit"],
    nouns: ["task", "project"]
  },
  verbFirst: { selected: "edit", nextNouns: ["task", "project"] },
  nounFirst: { selected: "task", nextVerbs: ["create", "edit"] }
}
```

Derive both navigation paths from the shared operation registry so they expose the same supported verb/noun pairs.

## Shared command and query API

One registry describes supported operations, validated argument types/requiredness, and result descriptions. Commands mutate state; queries inspect it. Unknown operations, unknown arguments, and invalid argument types are rejected before handler execution.

Public user actions and registered state are the compatibility surface. Implementation helpers and handler functions are not remotely callable code.

The same registered operation is accessible through:

- The Go API.
- Keybindings.
- The command palette.
- A central CLI with subcommands.
- Optional individual CLI executables delegating to the same interface.

Live CLI examples:

```sh
/tmp/ochazuke modal push --json '{"id":"draft","input":"dialog"}'
/tmp/ochazuke modal pop
/tmp/ochazuke mode set --json '{"mode":"insert"}'
/tmp/ochazuke bindings list
/tmp/ochazuke state get --json '{"path":"/frames"}'
/tmp/ochazuke state get --json '{"path":"/inputs/directory/text"}'
/tmp/ochazuke mode register --json '{"name":"directory-search"}'
```

JSON inspection covers registered pages (`activePage`, `pages`), inputs and histories, modal frames, modes, layer definitions, effective bindings, and application frame state. `get.state` accepts a JSON Pointer (`""` for the full state; `~1` escapes `/` and `~0` escapes `~` in a key). Appearance has its own `get.appearance` query. All registered inputs are public state: do not register secrets without adding an explicit export policy. Handlers, renderers, and terminal handles are not serialized.

## Live CLI control

The live transport is a local Unix socket. Its parent directory must be owned by the current user and have no group/other access; the socket is mode `0600`. Startup refuses to overwrite an existing socket. Use different `--socket` paths for different instances. After an unclean exit, verify the old instance is stopped before removing a stale socket.

CLI commands enter Bubble Tea's message loop. The event loop remains the sole owner of UI state; transport handlers do not mutate models concurrently. Queries must also obtain a consistent view through the runtime.

Keyboard, Go API, palette, and CLI use the same command semantics. Queries run through the same event loop and return detached JSON values. The wire protocol accepts one newline-terminated request per connection and returns `{ "result": ... }` or `{ "error": "..." }`. The CLI prints the result as JSON and returns a nonzero exit code for errors.

## Scope and limits

- Standalone Go module and example application; no gmtm/Matomeru integration.
- Vim-style editing, not a complete Vim implementation; no promise of text objects, dot-repeat, or full Vim scripting compatibility.
- Cursor and edit offsets count Unicode runes, not grapheme clusters. Rendering accounts for terminal cell width.
- Undo trees, yank history, registers, and macros are in memory; they do not persist across restarts.
- Custom modes are registered names. Applications supply their mode-specific bindings; unknown modes are not silently interpreted as normal or insert.
- Modal frames preserve their own state and existing input instances. Closing a modal restores interaction state, not a stale copy of shared application data.
- Handler registration and widget construction are Go APIs. User-facing operations, including configuration, palette navigation, field editing, and picker actions, are registered and available through the CLI.
