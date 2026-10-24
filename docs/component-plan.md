# Component and input design record

Recorded: 2026-10-23.

This records the discussion about building a Matomeru-like application with Ochazuke. It separates confirmed requirements from proposed APIs and topics still awaiting discussion. The user subsequently authorized and received the Readline-only input and split-picker implementation below; the remaining proposals are not implemented or authorized by this record.

Related explanation: [Extensibility](extensibility.html). Existing project work remains in [TODO.md](../TODO.md); this record does not replace that list.

## Confirmed requirements and direction

### Readline-only input is a flag

- [x] Add a Readline-only editing option to the existing input implementation.

The user explicitly confirmed that this is a flag, not a separate editor. The input remains a text editor rather than entering Vim normal mode. Starting in insert mode alone would be insufficient because ordinary Vim-enabled inputs switch to normal mode on Escape.

Existing single-line and multiline inputs already share Vim-style normal editing, Readline insert editing, undo history, and registers. Reuse that implementation.

Implemented as `Input.ReadlineOnly`, set before `Runtime.AddInput`. Registration starts insert mode, and `SetMode` preserves it. Unbound Escape does not change mode or cursor position; applications can bind Escape to their own cancellation operation. Inputs that do not enable the flag retain their existing behavior. Undo/redo, multiline editing, and path-aware kill/yank remain shared.

### Preserve path-aware word deletion

Path input should support an editable absolute path and recognize `/` as a word boundary for backward deletion. Killed text must remain available to yank back with `Ctrl-Y`.

This behavior is partly present already: `Ctrl-W` treats whitespace and `/` as boundaries and keeps filename punctuation together. The current behavior belongs to the shared editor, not a separate path-only input type.

```js
before = "/work/ochazuke/src/main.go|";
after = "/work/ochazuke/src/|";
killed = "main.go";
```

The `|` marks the cursor in this illustration. Editing policy, path-value rules, and completion-token boundaries are separate concerns. No second editor implementation is required merely to support paths.

### Keep the chosen split picker design

- [x] Integrate the preferred split picker design into the shared picker.

The user prefers the split presentation. Its top field should be an editable absolute path, not merely a field labeled "Find". Bindings must remain configurable rather than become fixed renderer behavior. Function keys are not acceptable controls.

Implemented in the production `Picker`: result list on the left and selected-entry metadata plus an asynchronous text/directory preview on the right, with a compact list/metadata fallback on narrow terminals. The initial path expands to an absolute path and stays editable through the shared Readline-only input. Preview reads are bounded and stale deliveries cannot replace a newer selection. The exploratory variants remain separate in `.worktrees/picker-lab`.

Production defaults are `Ctrl-N/P` for results, `Alt-Up` for parent, Tab for completion, Enter for open/select, `Ctrl-S` for confirmation, and Escape for immediate cancellation. They are ordinary `picker`-layer insert-mode bindings, not renderer shortcuts. `set.binding` overrides/adds bindings; the existing `Layer.Bindings` slice supports removing or replacing defaults. Help derives from effective bindings. No additional remapping API or function-key controls were introduced.

### Focus selects the innermost item layer

- [ ] Connect reusable component focus and row selection to the existing runtime context path.

The user's item1/item2 example describes alternative paths, not two items stacked together:

```js
// The sidebar is focused and the repository item is active.
context = {
	Path: ["app", "workspace", "sidebar", "repo-1"],
	InputID: ""
};

// Moving to the worktree replaces only the final entry.
context = {
	Path: ["app", "workspace", "sidebar", "worktree-1"],
	InputID: ""
};
```

The item supplies its own bindings and action overrides. Sidebar, page, and application layers retain shared behavior. The runtime owns these layers; it is not itself an entry in `Context.Path`.

The existing layer resolver already gives inner entries precedence. The remaining component integration is to keep that path synchronized with focus and selection. Each entry must refer to a registered layer.

Focus moving into an editor changes the active branch and identifies the input. An item that remains selected in an unfocused list must not keep intercepting the editor's keys.

This does not establish independently active modes for every layer. Today, one resolved mode such as `normal` or `insert` filters bindings across the path. `Layer.Actions` overrides are resolved by path without a mode filter. No change to those mode semantics was agreed.

### Application behavior stays application-owned

Domain modules, persistence, and terminal-daemon integration belong to the application. Modules can expose their methods through registered operations and application bindings.

The application supplies repository/worktree data, item identities, labels, relationships, and handlers. Generic UI components should not need Git or agent-specific knowledge.

One root Ochazuke model and one runtime can coordinate many pages and components. A page or pane does not require another root model. A terminal daemon can own the terminal sessions while an application component displays and controls them.

## Optional capability requested: inline completion

The user proposed arbitrary trigger-based completion for prompt composition and other text inputs. The capability is optional; its exact API is not settled. "Inline completion" and "completion trigger" are suggested names, not finalized terminology.

The requested shape is:

- An application-configured character or character sequence activates completion at the beginning of a word/token boundary.
- The text after the trigger, within that token, becomes the provider's query.
- A provider conceptually accepts `search(query)` and returns suggestions.
- A plain string list is sufficient when display text and inserted text are identical.
- Applications choose which triggers/providers are enabled, including differences between agent harnesses.

```js
// Illustrative configuration, not an implemented API.
input = {
	readlineOnly: true,
	completionTriggers: [
		{ trigger: "@", search: searchFiles },
		{ trigger: "$", search: searchSkills },
		{ trigger: "[[", search: searchReferences }
	]
};

text = "Please review @src/ma";
query = "src/ma";
suggestions = ["src/main.go", "src/main_test.go"];
```

Suggested behavior, still subject to agreement:

- Reuse a selectable list rather than build separate file, skill, and agent completion widgets.
- Give the active completion list an inner binding layer while typing continues to edit the same input.
- Accept a suggestion as one undoable replacement, leaving unrelated input text intact.
- Let Escape dismiss suggestions without leaving Readline-only editing.
- Ignore stale results if providers are asynchronous.

Open choices:

- Exact token boundaries. Path-editing boundaries must not automatically split a completion query at `/`. Whitespace boundaries were suggested as a default.
- Whether acceptance preserves the trigger or replaces it too. Preserving it was suggested, not confirmed.
- Query and replacement ranges when the cursor is inside an existing token.
- Provider cancellation, error reporting, and synchronous versus asynchronous signatures.
- Overlapping trigger precedence. Longest-match behavior was suggested, not confirmed.

## Proposed API change: optional model access through runtime

The user asked whether handlers could reach the model through an optional field on `Runtime`. This is a discussed direction, not an implemented or finalized API.

```go
// Proposed field, not present in Runtime today.
Model *Model
```

This would preserve the current handler signature instead of introducing a new handler-context type. `NewModel` would attach the model, and UI-dependent handlers would access `r.Model`. A runtime without a UI would leave it nil.

The discussed ownership rule is zero or one attached root model per runtime. Model and runtime pointers can refer to each other. Exact attachment, duplicate-attachment handling, and lifecycle behavior still need a design decision.

No handler-signature migration or framework-wide naming rename was agreed. `Operation` remains the definition, `Handler` the function type, and `Operation.Run` the default implementation. The confusing use of "action" in prose and `Layer.Actions` was identified, but a rename was only suggested.

## Reusable list/tree direction, not a finalized component API

The first custom-component discussion favored a generic selectable list rather than separate repository and worktree widgets.

Proposed shared responsibilities are selection, scrolling, keeping the active row visible, retaining selection by ID across refreshes, and notifying focus changes. Tree expansion is optional if the application needs nested rows.

The application supplies records and per-item behavior. The framework's existing input editor can supply search; existing layout helpers can draw the surrounding panel. The component contract and exact tree scope are not finalized.

A layout helper arranges supplied content. A list/tree component also owns interaction state. gmtm's `internal/ui/twopane` is an example of a richer component with nested outline navigation, previews, and asynchronous results; it has not been adopted into Ochazuke by this discussion.

## Confirmed live-terminal input contract

The user confirmed these controls after the initial design record. This is an agreed behavior contract, not implemented terminal support.

The terminal component has a live-input state that captures keyboard input for the inner terminal process. Its only default control prefix is `Ctrl+A`; the prefix is configurable. Ordinary input must not fall through to the surrounding application, page, or sidebar bindings.

| Input | Agreed behavior |
|---|---|
| Ordinary input outside a pending prefix sequence, including Escape and `Ctrl+C` | Forward unchanged to the inner terminal process |
| `Ctrl+A` | Enter pending-prefix handling without forwarding the prefix |
| `Ctrl+A`, then `d` | Detach from live mode |
| `Ctrl+A`, then `Ctrl+A` | Send one literal `Ctrl+A` to the inner process |

These are sequential keystrokes, not a three-key chord. Both completed sequences clear pending-prefix state. No other live-mode shortcuts were agreed.

Detaching returns control and focus to the surrounding application and restores its normal binding resolution. It does not terminate the terminal session or its running process.

The terminal-daemon client and protocol remain application-level. The component coordinates display and input ownership with Ochazuke's layout, focus, and event loop. This does not require a second root model.

Still undecided:

- What happens after an unrecognized prefix sequence, such as `Ctrl+A`, then `x`.
- Whether a pending prefix times out, and how incomplete sequences are handled.
- The exact component/provider API, screen-update representation, resize behavior, and disconnect/reconnect lifecycle.

### Resume point

The next terminal discussion can settle the undecided cases above. After that, continue with domain detail views, specialized previews, and optional resizable panes, one at a time. The list/tree and inline-completion proposals elsewhere in this record remain proposals; this agreement does not finalize those APIs.

## Existing capabilities to reuse

| Capability | Current implementation |
|---|---|
| Page chrome, panels, responsive splits, sidebar | `Layout.Page`, `Layout.Panel`, `Layout.Split`, `Layout.Sidebar`, and `PanelContentSize` in [layout.go](../layout.go) |
| Retained pages and modal composition | [pages.go](../pages.go), [model.go](../model.go), and [surface.go](../surface.go) |
| Single-line and multiline editors | [input.go](../input.go), [editing.go](../editing.go), and [editor.go](../editor.go) |
| Operation dispatch and inner-layer precedence | [runtime.go](../runtime.go) and [configuration.go](../configuration.go) |
| External requests delivered onto the UI loop | `CallMsg`, `Runtime.HandleCall`, and `program.Send` in [transport.go](../transport.go) and [the example entrypoint](../examples/full-project/main.go) |

Background workers must not mutate UI-owned state by calling the runtime concurrently. They can deliver a request/message to the UI loop, which invokes a handler. The existing `CallMsg` path already demonstrates this; a new general-purpose event bus is not an agreed requirement.

The application still owns starting/cancelling work and identifying which item a result belongs to. Results should not accidentally act on a different item that became focused later.

Current operation argument validation accepts strings, integers, and booleans. Complex return values are supported. Typed internal completion messages or a richer argument schema would be separate choices; a schema expansion has not been agreed.

## Remaining discussion agenda

These are future conversations, not technical blockers and not approved implementation tasks. Continue one component at a time.

1. Live terminal component: the input contract above is settled. Remaining topics are unknown/incomplete prefix sequences, component API, screen display, resizing, and connection lifecycle.
2. Domain detail views: determine which are ordinary application renderers inside existing panels rather than new reusable widgets.
3. Specialized previews: discuss only formats the application actually needs, such as diffs, Markdown, or JSON trees.
4. Resizable pane workspaces: optional. Existing responsive fixed layouts already work; interactive split manipulation is additional scope.

The Readline-only input and split picker are implemented. Verification includes the Go suite, the full-project smoke command, and an interactive terminal run exercising previews, directory navigation, path kill/yank, remapped help/cancellation, and typing after an attempted normal-mode transition. The terminal component, list/tree API, inline completion, and optional model-access proposals remain unimplemented.
