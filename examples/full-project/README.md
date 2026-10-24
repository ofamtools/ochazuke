# Full-project example

A runnable workspace application with JSON configuration and separate Go modules for composition, actions, bindings, and rendering. It uses the public `ochazuke` package rather than copying the framework's editor, palette, or picker.

## Run

From the repository root, with Go 1.25 or newer:

```sh
go run ./examples/full-project
```

The executable embeds the seven files in `config/`. To edit configuration without rebuilding a compiled executable, supply a complete configuration directory:

```sh
go run ./examples/full-project --config ./examples/full-project/config
```

Configuration is loaded at startup, not watched. An external directory replaces the embedded configuration rather than merging with it. Unknown JSON fields, trailing JSON values, and invalid references produce startup errors.

Run the headless behavior check:

```sh
go run ./examples/full-project --smoke
go run ./examples/full-project --config ./examples/full-project/config --smoke
```

The smoke scenario uses the supplied example's IDs. It is not a general validator for an unrelated application configuration.

## File responsibilities

| File | Responsibility |
|---|---|
| `main.go` | Flags, terminal startup, one-shot commands, and socket transport |
| `app.go` | Construct the runtime, register configured objects, and assemble the model |
| `config.go` | Configuration types, embedded defaults, and JSON parsing |
| `actions.go` | Named Go handlers and configured operation registration |
| `bindings.go` | Validate and install the configured binding rows through `Runtime.Bind` |
| `pages.go` | Named page and modal renderers |
| `list.go` | Compose the shared selectable list and a companion editor using existing panels |
| `terminal.go` | The echo stream (the example's stand-in for a daemon client) and the console page renderer around a terminal instance |
| `config/app.json` | Application title, initial page, custom modes, palette instance sources/default actions, theme, and density |
| `config/instances.json` | Instances (`id`, `noun`, `parent`) and bare row nouns (with their `verbs`) used for action and binding resolution |
| `config/inputs.json` | Initial values, parent instance, multiline flag, `noun: "prompt"` for Readline-only fields, and path filters |
| `config/pages.json` | Page order, titles, starting focus, renderer names (`workspace`, `text`, `list`, `terminal`), field lists, and sample list records |
| `config/actions.json` | Verb/noun schemas, handler names, and `on` (the noun whose instances override an operation) |
| `config/bindings.json` | The framework's binding rows: `noun` or `instance`, `mode`, `keys` or `pattern`, `request` (`null` unbinds), `from`, `when` |
| `config/modals.json` | Modal titles, renderer names, and editable inputs |

JSON names Go handlers and renderers; it cannot contain executable Go functions. Add behavior to the appropriate registry, then reference that name in configuration. Framework operations are already registered and do not need copies in `actions.json`.

## Configuration flow

The following simplified data shows the relationships. The complete executable configuration lives in `config/`.

```js
{
  app: { modes: ["review"] },
  action: {
    verb: "greet", noun: "workspace", handler: "greet",
    parameters: {
      name: { type: "string", required: true },
      repeat: { type: "integer" }
    }
  },
  binding: {
    noun: "app", mode: "normal", keys: "g h",
    request: {
      verb: "greet", noun: "workspace",
      args: { name: "Ochazuke", repeat: 1 }
    }
  }
}
```

The registry identifier is `workspace.greet` (`noun.verb`). The CLI spelling is `workspace greet` (`NOUN VERB`). A binding, the palette, and a CLI request dispatch the same registered operation.

## Explore the application

- The sidebar marks the active page. In normal mode, `Ctrl-J/K` switches pages; the palette can open a page directly. Sidebar navigation does not take focus away from the active editor, and an open modal blocks page changes.
- Editors appear in titled panels. When the available height cannot hold every field, the page shows the focused field and a `Field N/M` hint. Tab and Shift-Tab still visit every field. Action results remain below the editor.

- **Workspace** (`overview`) contains the title and notes editors. Edit with `i`, return to normal mode with Escape, and use `u` to undo. Switch pages and return to observe retained input state.
- **Paths** contains a file input, a directory input, and a multiline paths input. In normal mode, `Ctrl-F` or Enter opens the picker with the focused input's configured filter. Tab cycles fields. File and directory selection are both supported.
- **Items** contains a reusable flat list and a Readline-only note. From startup, press `Ctrl-J` twice to open it. Tab switches list/editor focus. In the list, `j/k` or arrows select, `r` reorders records while preserving the selected ID, and `x` removes the selected sample row. Rows name nouns: on the repository row `f` fetches; on a worktree row `o` or Enter opens and `d` removes; on a branch row `c` or Enter checks out; Enter on the repository row inspects the context. `?` opens a modal listing the focused row's noun and effective keys. In the editor those letters type text instead. Tab back to the list before using normal-mode page shortcuts.
- **Guide** (`help`) is a retained text page. Help can also open as a modal.
- **Console** (`console`) holds a terminal instance (`pane`) under the page, fed by the example's echo stream. Opening the page attaches: every key goes to the pane, `ctrl+a` is the leader. Under it `d` detaches (focus returns to the page; Enter re-attaches), `:` opens the palette, `?` lists the pane's keys, `Ctrl-J/K` switch pages, `ctrl+a` sends itself and any other key sends the leader and itself. A leader left alone past the binding timeout is sent on its own. The page was added without touching the framework package: `pages.json`, `instances.json`, `bindings.json`, `terminal.go` and the `terminal` renderer branch in `pages.go`.
- `Ctrl-S` invokes the configured save action in any mode. The `notes-editor` noun overrides the application handler while the notes field is focused. This example records state in memory; it does not save a project to disk.
- `Ctrl-H` opens help. `Ctrl-O` opens the editable confirmation modal. From its editor context, `Ctrl-N` opens the details modal. Closing a modal restores its parent context.
- In normal mode, `g s` summarizes the workspace and `g h` invokes the parameterized greeting.
- In the workspace context, `Ctrl-T` sends an insert operation explicitly targeted at the title input.
- On the Paths page, `g f` focuses the directory input.
- In normal mode, `Ctrl-J/K` cycles pages and `:` opens the palette. `Ctrl-Space` opens the palette in any editing mode. `Ctrl-Y` opens yank history, and `PgUp` and `PgDn` scroll. Pages are not numbered; use noun-first palette navigation to search by page title or ID.

The framework also supplies visual selection, registers, macro recording and replay, and branching undo. These are runtime/editor features rather than duplicated configuration formats. See the repository [keyboard guide](../../README.md#keyboard-guide).

### List data and focus

The Items records are configuration examples, not live repository/worktree discovery. `actions.go` owns their mutable source data; refresh/removal calls the shared list's `SetItems`. Removing these rows changes only example memory, not files or Git worktrees.

A `list` page names its list as the starting `focus`, one companion input, and `items` with unique `id`, display `label`, and a registered `noun`. The nouns (`repo`, `worktree`, `branch`) are declared in `instances.json` with their `verbs`; `actions.json` registers the verbs (`repo.fetch`, `worktree.open`, `worktree.remove`, `branch.checkout`, each taking an optional `row`) and `bindings.json` binds keys per noun. Every row is an instance of its noun with the list as its parent. Space marks rows; `list.apply {verb}` runs `<row noun>.verb` on each marked row and reports the rows whose noun lacks the verb. `inputs.json` supports `"noun": "prompt"` for a Readline-only field.

Selecting a row changes only the leaf, which is the row ID, for example `app/items/item-list/repository` → `app/items/item-list/main`. A row verb acts on the row in its context: a key on the focused row, a request with `--target main`, or `{"row": "main"}`. Tab to the note focuses `item-note` (its path is `app/items/item-editor`). Selection changes and refreshes cannot take focus back from that editor. Page and modal restoration retain the updated selection.

Wide windows show list and note beside one another; medium windows stack them; small windows show the pane belonging to the retained page context. Selection remains visible when resizing. All controls use configured bindings or the list's ordinary defaults, with no function keys.

### Layout ownership

`Build` enables the shared model's sidebar in `app.go`:

```go
m := ochazuke.NewModel(r)
m.Sidebar = true
```

`Model` owns the application heading, mode indicator, navigation, scrolling, status footer, and modal overlays. The sidebar uses `Runtime.PageOrder` and `Runtime.ActivePage`; it has no second selection state. Apps that leave `Sidebar` false retain the tab layout.

Page renderers receive the content width and height after the shell, sidebar, borders, and padding have been deducted. They return content, not another `Layout.Page`. This prevents nested application shells.

`workspaceView` in `pages.go` composes the field panels with `Layout.Panel`. `PanelContentSize` gives each `InputView` the space inside its panel. The text renderer wraps the Guide to the available width. Page scrolling affects the main content, not the sidebar.

On narrow terminals, the navigation rail stacks above the page. Modal renderers continue to receive bounds from `ModalSize`; `Model` places them over the existing page with `ComposeModal`. Neither page renderers nor modal renderers draw their own full-screen backdrop.


### Noun instances and default actions

Type `:` in normal mode, choose `noun:page`, then search for a page title or ID. The list contains page instances alongside page verbs. Selecting a page opens it immediately; selecting `verb:open` opens the ordinary argument form instead.

`config/app.json` explicitly connects each noun to an instance source and default action:

```json
{
  "palette": [
    {"noun": "page", "source": "pages", "defaultVerb": "open", "argument": "id"},
    {"noun": "input", "source": "inputs", "defaultVerb": "focus", "argument": "id"},
    {"noun": "worktree", "source": "rows", "defaultVerb": "open", "argument": "row"}
  ]
}
```

The Go source registry in `app.go` supplies the items. `pages` reads the runtime's live page list; it does not freeze the list when the palette is constructed. `inputs` lists fields on the originating page, excluding palette and picker internals. Focusing an input does not navigate to another page. `rows` lists the list rows whose noun is the configured noun, so `noun:worktree` then `instance:docs` runs `worktree.open {row: "docs"}`.

A different default requires a registered operation with the configured string argument and no other required arguments. One instance selection supplies exactly that argument. Operations needing more information remain available through the verb list and argument form.

An instance and a verb may share a name without ambiguity. Palette selection values are `instance:<id>` and `verb:<verb>` for nouns with instance sources. Published palette `entries` include each item's value, kind, and display label.

For a running application:

```sh
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock palette open
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock palette select --json '{"value":"noun:page"}'
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock palette select --json '{"value":"instance:paths"}'
```

### Custom modes use the existing API

`app.json` declares `review`. Startup registers it through `mode.register`. In normal mode, `Ctrl-R` switches the current context to `review` through `mode.set`. In review mode, `s` invokes inspection and Escape sets the mode to `normal`.

Each instance holds its own mode: an input's instance when an input is focused, otherwise the innermost element of the context (a page, a list row, a modal body), inherited from its ancestors when unset. There is no mode stack. Setting a mode selects it directly, and modal restoration restores the parent context rather than pushing or popping modes. Custom-mode behavior comes from its bindings; it does not inherit normal-mode editor keys. Any-mode bindings still apply.
Readline-only fields, including the Items note, remain in insert mode even if `mode.set` requests a custom mode. Tab back to the list to use normal-mode page controls.

The framework API is unchanged:

```js
{ verb: "register", noun: "mode", args: { name: "review" } }
{ verb: "set", noun: "mode", args: { mode: "review" } }
{ verb: "set", noun: "mode", args: { mode: "normal" } }
```

## Call operations

Without `--socket`, commands construct a fresh configured application, execute one request, print JSON, and exit. State does not persist between commands.

```sh
go run ./examples/full-project operations list
go run ./examples/full-project workspace greet --json '{"name":"Ada","repeat":2}'
go run ./examples/full-project mode set --json '{"mode":"review"}'
```

To control one running application, start it with an explicit socket path in a private directory:

```sh
mkdir -p /tmp/ochazuke-full-project
chmod 700 /tmp/ochazuke-full-project
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock run
```

In another terminal:

```sh
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock page open --json '{"id":"paths"}'
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock mode set --json '{"mode":"review"}'
go run ./examples/full-project --socket /tmp/ochazuke-full-project/app.sock state get --json '{"path":"/activePage"}'
```

Socket commands use the running server's configuration; local `--config` does not change that server.

`run --headless` uses the same event loop and socket without terminal rendering. The socket is local and private; the example does not expose a network service. Application state and input contents are inspectable through the runtime, so do not put secrets in these example fields.

## Verify changes

```sh
go test ./...
go run ./examples/full-project --smoke
go run ./examples/full-project --config ./examples/full-project/config --smoke
```

The configuration tests cover external configuration and malformed or missing files. The smoke command drives configured key sequences and real operations, including custom modes, nested modals, picker selection, palette execution, and shared editor state.

`pages_test.go` covers responsive field navigation, visible action feedback, modal-blocked page changes, restored focus after nested modals, and Guide scrolling. The shared layout tests check that the shell displays the entire content area promised to page renderers.

For a terminal check, start at 80×24 and resize to 60×18. Open Paths with `Ctrl-J`, cycle its fields with Tab, then press `Ctrl-S` to see the action result. Open confirmation with `Ctrl-O`, nest details with `Ctrl-N`, and close both with Escape. Paths should retain the focused field. Open Guide and use `PgDn`; the sidebar should stay in place.
