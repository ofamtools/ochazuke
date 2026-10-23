# Ochazuke

A Go application framework on Charmbracelet Bubble Tea v1.3, with a runnable example, layered actions and bindings, stateful modals, modal text editing, a nested command palette, and live CLI control.

Status: working standalone foundation. Neither gmtm nor Matomeru is changed. Existing widgets are retained; Bubbles is the preferred starting point for new standard widgets.

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
/tmp/ochazuke input undo
/tmp/ochazuke input undo --json '{"steps":3}' --target directory
/tmp/ochazuke state get --json '{"path":"/inputs/directory/text"}'
```

Global `--socket PATH` goes before `run` or `NOUN VERB`. It selects a specific application instance. The default socket on macOS is `~/Library/Caches/ochazuke/app.sock`. `run --headless` uses the same Bubble Tea event loop without terminal rendering.

### Keyboard guide

The example starts in normal mode. Tab/Shift-Tab switch between directory and notes inputs.

| Context | Keys | Behavior |
|---|---|---|
| Normal input | `h j k l`, arrows | Rune/line movement |
| Normal input | `w b e`, `0 $`, `gg G` | Word, line, and document motions |
| Normal input | `i a I A` | Insert here, after cursor, at first nonblank, or at line end |
| Multiline normal input | `o O` | Open a line below/above and insert |
| Normal input | `x`, `u`, `Ctrl-R` | Delete, undo, redo |
| Normal input | Numeric prefix | Count for motions, delete, undo/redo; `Ngg`/`NG` select a line |
| Insert input | `Ctrl-A/E`, `Ctrl-B/F` | Line start/end, previous/next rune |
| Insert input | `Ctrl-P/N`, `Alt-B/F` | Previous/next line, previous/next word |
| Insert input | `Ctrl-W/U/K`, `Ctrl-Y` | Kill word/to line start/to line end; yank local killed text |
| Insert input | Backspace, Delete/`Ctrl-D`, Enter | Delete backward/forward; newline in multiline inputs |
| Insert input | Escape | Return to normal mode |
| Normal input | Escape | Cancel a pending motion/count |
| Normal app | `Ctrl-P` | Open palette |
| Any editing mode | `Ctrl-Space` | Open palette without stealing Readline `Ctrl-P` |
| Normal app | `Ctrl-O`, `Ctrl-T` | Directory/file selector |
| Normal app | `Ctrl-N` | Open an independent draft modal |
| Draft modal | `Ctrl-G` | Close and restore the parent |
| Normal app | `F2`, `?` | Inspect the handling layer; toggle help |
| App | `Ctrl-C`; normal-mode `q` | Quit |

On terminals where Option does not send Meta sequences, configure Option-as-Meta to use `Alt-B/F`. `Ctrl-Space` is received as `ctrl+@` by Bubble Tea. These shortcuts are registered bindings, not hard-coded global overrides.

### Palette and selectors

- The palette root displays the actual **VERBS** and **NOUNS** lists side by side. Left/right switch columns; `j/k` or arrows move; Enter chooses.
- Choosing a verb lists compatible nouns; choosing a noun lists compatible verbs.
- `/` focuses a search input. Search and argument fields use the same Vim/Readline editor and independent undo trees as page inputs. Escape first returns an editing field to normal mode; subsequent Escape goes back.
- Argument forms show required/optional fields. Tab/arrows change fields; Enter advances and then executes. Empty optional fields are omitted. `set palette-argument` can supply an explicit empty string; `clear palette-argument` omits one.
- The originating interaction context is captured before the palette opens. Selecting `undo → input` edits the originating input, not the palette's search field.
- `Ctrl-C` cancels the palette. Errors retain the form and its drafts.
- File/directory selectors use `j/k`, Enter to open a directory/select a file, `-` to go to the parent, `Ctrl-S` to choose the current directory/highlighted file, and Escape to cancel.

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
| `input.go`, `editing.go` | Input-owned undo trees and the Vim/Readline adapter |
| `palette.go`, `palette_editing.go` | Verb/noun navigation, typed arguments, originating context |
| `picker.go` | File/directory selection using registered actions |
| `model.go` | Bubble Tea update/view adapter |
| `transport.go`, `cli.go`, `cmd/ochazuke` | Private Unix socket and generic CLI dispatch |
| `demo.go`, `examples/core` | Runnable UI and noninteractive foundation example |

Local checks:

```sh
go test ./...
go run ./examples/core
```

The integrated implementation was checked locally on macOS with the test suite and a live terminal/CLI session. The latter exercised grouped undo/redo, captured-context palette dispatch, modal restoration, Readline line movement, layer resolution, and picker lifecycle.

## Requirements

- General UI infrastructure with a stack of stateful modals.
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

JSON inspection covers registered inputs and their histories, modal frames, modes, layer definitions, effective bindings, and application frame state. `get.state` accepts a JSON Pointer (`""` for the full state; `~1` escapes `/` and `~0` escapes `~` in a key). All inputs registered with this foundation are public state: do not register secrets without adding an explicit export policy. Handlers and terminal handles are not serialized.

## Live CLI control

The live transport is a local Unix socket. Its parent directory must be owned by the current user and have no group/other access; the socket is mode `0600`. Startup refuses to overwrite an existing socket. Use different `--socket` paths for different instances. After an unclean exit, verify the old instance is stopped before removing a stale socket.

CLI commands enter Bubble Tea's message loop. The event loop remains the sole owner of UI state; transport handlers do not mutate models concurrently. Queries must also obtain a consistent view through the runtime.

Keyboard, Go API, palette, and CLI use the same command semantics. Queries run through the same event loop and return detached JSON values. The wire protocol accepts one newline-terminated request per connection and returns `{ "result": ... }` or `{ "error": "..." }`. The CLI prints the result as JSON and returns a nonzero exit code for errors.

## Scope and limits

- Standalone Go module and example application; no gmtm/Matomeru integration.
- Basic Vim/Readline subset, not a full Vim engine: no visual selections, operators/text objects, dot-repeat, or repeated insert counts.
- Cursor and edit offsets count Unicode runes, not grapheme clusters. Rendering accounts for terminal cell width.
- Undo trees are in-memory text snapshots. No persistence across restarts or kill ring.
- Custom modes are registered names. Applications supply their mode-specific bindings; unknown modes are not silently interpreted as normal or insert.
- Modal frames preserve their own state and existing input instances. Closing a modal restores interaction state, not a stale copy of shared application data.
- Handler registration and widget construction are Go APIs. User-facing operations, including configuration, palette navigation, field editing, and picker actions, are registered and available through the CLI.
