# Ochazuke

A proposed Go application framework on top of Bubble Tea. Bubbles may supply individual widgets underneath it.

Status: requirements and initial design; no implementation yet. Neither gmtm nor Matomeru is changed by this proposal.

## Requirements

- General UI infrastructure with a stack of stateful modals.
- Pushing a modal preserves the previous modal's state; popping restores the previous interaction exactly.
- Vim-style interaction with arbitrary, application-defined modes.
- Customizable keybindings scoped to modes, components, or the application, plus global bindings.
- Public operations available through an API and CLI tools, including a central CLI with subcommands.
- JSON export of application state and individually addressable public values.

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

Vim-style modes alone do not constitute a Vim text editor. Operations such as `ciw`, `d3w`, registers, undo, and repeat require an editing engine. The extent of Vim text-editing compatibility remains to be decided.

## Scoped, customizable keybindings

Bindings invoke named commands instead of embedding application behavior in key handlers.

```js
[
  {
    scope: "global",
    keys: ["ctrl+p"],
    command: "palette.open"
  },
  {
    scope: "task-editor",
    mode: "normal",
    keys: ["i"],
    command: "mode.set",
    args: { mode: "insert" }
  },
  {
    scope: "task-editor",
    mode: "insert",
    keys: ["escape"],
    command: "mode.set",
    args: { mode: "normal" }
  }
]
```

Proposed precedence:

1. Focused component.
2. Active modal.
3. Application.
4. Global fallback.

Within a scope, mode-specific bindings precede mode-independent bindings. User configuration overrides defaults in the same scope. Shortcuts that must override every scope should be explicitly reserved rather than implicitly granting that behavior to all global bindings.

Multi-key sequences such as `g g` require pending-sequence state, cancellation, and defined incomplete-sequence behavior. Unmatched text in insert mode is delivered to the focused editor.

## Shared command and query API

One registry describes supported operations, their input schemas, and their result schemas. Commands mutate state; queries inspect it.

Expose supported public operations and inspectable values through this registry. Whether the requirement to expose every method/function includes internal helpers needs clarification before implementation; the proposed boundary keeps private implementation details out of the compatibility contract.

The same registered operation is accessible through:

- The Go API.
- Keybindings.
- The command palette.
- A central CLI with subcommands.
- Optional individual CLI executables delegating to the same interface.

Illustrative CLI shape; these commands do not exist yet:

```sh
ochazuke modal push --json '{"component":"task-editor","args":{"taskId":"123"}}'
ochazuke modal pop
ochazuke mode set insert
ochazuke bindings list --mode normal --format json
ochazuke state get --path /modalStack --format json
ochazuke state get --path /modalStack/0/values/title --format json
```

JSON inspection covers declared public state, including registered component state, modes, effective bindings, and domain values. Functions, terminal handles, and private widget internals are not JSON state. Sensitive fields need explicit export rules. Path syntax and result envelopes remain to be specified.

## Live CLI control

A separate CLI process cannot directly access a running TUI's memory. Live control requires a transport, proposed as a local Unix socket, with an explicit application-instance selector.

CLI commands enter Bubble Tea's message loop. The event loop remains the sole owner of UI state; transport handlers do not mutate models concurrently. Queries must also obtain a consistent view through the runtime.

Keyboard, Go API, palette, and CLI use the same command semantics. Queries expose the same public state as JSON.

## Proposed implementation boundary

Start as a standalone reusable Go module with a small example application. Prove modal restoration, arbitrary modes, key resolution, JSON inspection, and live CLI control together before integrating into gmtm or Matomeru.

Open decisions:

- Bubble Tea major version and supported integration targets.
- Extent of Vim text-editing compatibility versus mode/key-routing support.
- Final public API, CLI schema, and inspection contract.
- Live instance discovery, transport access controls, and secret-export policy.
- Whether persistence across restarts is required.
