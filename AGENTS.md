# Ochazuke: agent guide

Go application framework on Bubble Tea **v2** (`charm.land/bubbletea/v2` v2.1.0), Lip Gloss v2 (`charm.land/lipgloss/v2` v2.0.6), Bubbles v2 (`charm.land/bubbles/v2` v2.2.1). Single flat package `ochazuke` at the repo root plus `cmd/`, `examples/`, `docs/`.

## Read in this order

1. This file.
2. `docs/CODEMAP.md`: generated, one entry per source file with declared types, methods, and the sibling files it leans on. Open the two or three files it points at instead of grepping the package.
3. `docs/MIGRATION-PLAN.md` is the completed migration record (all phases done, each with its commit and decisions). Read it for why something is the way it is and for the after-phase-7 items, not for work in progress.
4. README.md only for the section you need. The "API and ownership" section has the file-to-responsibility table and the ownership rules below in long form.

Regenerate the maps after adding, removing, or renaming files:

```sh
make codemap     # writes docs/CODEMAP.md and docs/CHARM-INVENTORY.md
```

Do not hand-edit either generated file. If a map entry is wrong, fix the code or the generator in `cmd/codemap` or `cmd/charmrefs`.

## Where does X live

| Concern | Files |
|---|---|
| Operation registry (kinds), focus, modal chain, `OpenModal`, `ExecuteAt`, snapshot, typed lookups (`Input`, `List`) | `runtime.go` |
| Delivery contract (`Component`, `Ticket`, `Completion`, `Start`/`Deliver`/`PendingCommands`), application verbs (`Ctx`, `Result`) | `delivery.go` |
| Per-verb confirm: the `confirm` noun, `SetConfirm`, prompt templates | `confirm.go` |
| Nouns, the `Define` builder, binding rows, key classes, guards | `noun.go` |
| Instances, parent links, `Payload`, ancestry, targets | `instance.go` |
| The stack walk (`Dispatch`), pending modes, key-sequence prefixes, `list.bindings` | `resolve.go`, `bindings.go` |
| Editor mode tables as data (Readline, Vim, operators, visual counts) | `editor_bindings.json`, `editor_keys.go` |
| App-noun rows for counts, registers and macros (`q`, `@`, `"`, digits) | `registers_bindings.json`, loaded by `editor_keys.go` |
| Binding files, modes, `select.context` | `configuration.go` |
| Bubble Tea adapter (delivers messages and drains commands through the runtime), renders the shell through `Layout.Shell`, `BindingHints`, modal renderers, scroll offsets, terminal cursor shape | `model.go` |
| Terminal instances: the `terminal` noun, `AddTerminal`, `TerminalStream`, frames through the delivery contract, the leader contract | `terminal.go` |
| Instance constructors sharing `InstanceOption` (`Parent`, `Kind`, `Focus`): `AddPage`, `AddList`, `AddInput`, `AddTerminal`, `Instance` | `pages.go`, `list.go`, `runtime.go`, `terminal.go`, `instance.go` |
| Pages as instances, `Runtime.Page`, navigation | `pages.go` |
| Input state (text, cursor, undo) and the editor handlers | `input.go`, `editing.go`, `editor*.go` |
| Selectable lists (rows as instances of nouns) | `list.go` |
| The `registers` component (yanks, named registers, history, active register, macro state), one instance under `app` | `registers.go`, `macros.go`, `yank_history.go` |
| Command palette (verb/noun, instances) | `palette.go`, `palette_editing.go`, `palette_nouns.go` |
| Path picker and fuzzy index | `picker.go`, `picker_search.go`, `picker_preview.go`, `picker_view.go` |
| Styling, the one page chrome (`Layout.Shell`, `Header`, `Footer`, tabs), panels, bounded modal composition | `theme.go`, `layout.go`, `surface.go` |
| Unix socket transport and CLI dispatch | `transport.go`, `cli.go`, `cmd/ochazuke` |
| Demo and example applications (no application pages in the framework package) | `examples/demo` (`demo.New()`, used by `cmd/ochazuke run` and `cmd/docgen`), `examples/core`, `examples/full-project` |

## Invariants

- `Runtime` is single-owner state. Call it from the Bubble Tea update loop. Other goroutines and processes go through `Call`/`CallMsg`, never concurrent `Execute`.
- `Runtime` holds no component fields: operation registry, nouns, instances and parent links, focus, `Page`, `Frames`, pending-sequence state, `BindingTimeout`, `Quit` and the command queue the adapter drains (`TestRuntimeHoldsNoComponentFields` pins the exact field list). Inputs, lists, pages, the picker and the registers are instances whose `Component` holds the typed object; read them with `r.Input(id)`, `r.List(id)`, `r.PageOf(id)`, `r.Registers()`, never a map on `Runtime`.
- Components implement the delivery contract (`HandleMessage`, `PendingCommand`, `Close`). Every result carries its owner instance id and a generation (`Ticket`); `Deliver` drops a stale or orphaned `Completion`, `closeInstance` (Pop, `SetItems`, `removeInstance`) cancels outstanding work and calls `Close`. The `Model` drives components from the instance registry; there is no component list on it.
- An operation is `interaction` (default, runs on the loop) or `application` (an `AppHandler func(Ctx) Result` registered through `Define(noun).Verb`). Application work returned as `Pending` runs off the loop as a `tea.Cmd`; the framework never blocks the loop on application I/O, and the completion lands on the owning instance or is dropped if it closed. Palette, `list.operations`, docs and help show the kind.
- Confirmation is a property of a verb (`Operation.Confirm`, set by `.Confirm("…")` or `set.confirm`), not a modal per call site: it opens the `confirm` noun's modal with `Payload{Prompt, Request}` parented at the focus; `y` runs the stored request (`Request.Confirmed`), `n`/`esc` pop, other keys are swallowed.
- Every keyboard action dispatches a registered operation through one stack walk (`Runtime.Dispatch`): from the focused instance outward to `app` (the `modal` noun before `app` while a modal is open), then the `editor` noun as the fallback when an input is on the path (a list row or page focus ends at `app`, so editor rows neither answer nor appear in `list.bindings` there; counts, registers and macros are `app` rows and still do). At each level the instance's own rows sit above its noun's; the mode table is read before the any-mode table; an exact key beats a key class (`digit`, `alpha`, `printable`, `paste`, `alpha-q`, `one-of:…`) beats `any`. The CLI is a generic `NOUN VERB` adapter over the same registry. Adding a feature means registering an operation and a row, never a key case: no key-name switch statements exist in `editing.go` or `editor.go`.
- Every pending condition is an exclusive mode with a typed `Payload` on the instance: `count_pending`, `operator_pending`, `g_pending`, `register_pending`, `macro_pending`, `replay_pending`, a key-sequence prefix (`"normal g"`), the `macro_recording` shadow. Exclusive means only that mode's table answers (no any-mode rows); one-shot means the answering key consumes the mode before its request runs unless the request is `enter.mode`, `append.count`, `leave.mode` or `cancel.input`. Rows are `Request`s; `Func` rows exist only where a row re-dispatches (recording, leader).
- Change modes with `Input.SetMode`. Never assign `Input.Mode` directly.
- Modals keep their own frame and existing input instances. Closing one restores interaction state, not a copy of shared data.
- Offsets count runes, not grapheme clusters. Rendering accounts for cell width.
- Custom modes are registered names. Unknown modes are not treated as normal or insert.
- Mode resolution: the mode is the `State` of the focused instance, inherited from the nearest ancestor with a non-empty `State`, else `normal`; a pending mode is entered on the focused instance and leaves back to the raw `State` it had (`Payload.Return`). `Input.Mode` mirrors only the editing mode (normal, insert, visual*) the pending mode will return to. Focus is one id per frame (`Frame.Focus`, `Runtime.FocusID`); `Context.Path` is derived. There are no layers and no `Frame.State`: application state lives with the application, scroll offsets on the `Model`.
- `Runtime.Page` is the current page, an instance of kind `page` under `app`; it is always an ancestor of the base frame's focus. Pages are ordered by registration (`PageList`), there is no page map.
- List rows are instances of their nouns (`ListItem.Noun`), created and removed by `SetItems`. Never define a noun per row; give the row's noun the verbs and rows, or put an override on the row instance. Marked rows fan out through `apply.list {verb}` as `verb.<row noun>` per row.
- Readline and Vim rows are the `editor` noun's defaults, generated from `editor_bindings.json`; an application overrides a key by binding it on any noun or instance on the stack (rows there resolve before the fallback), or in a binding file (same noun+mode+keys replaces, `"request": null` unbinds). The `prompt` noun pins insert mode. `list.bindings` lists every key the focus answers to in its mode, pending modes included.
- Hints derive from bindings. The footer, every modal's hint line and the picker's help are `BindingHints`/`ListBindings` output for the focused instance and mode; there are no hand-written hint strings in the framework, so a hint cannot drift from a row. One chrome: `Model.View` renders through `Layout.Shell`, the same method `Layout.PageBlock` uses; no second set of height or padding constants exists.
- The framework package holds no application pages. The demo is `examples/demo`; tests that need an application build one from the public API.
- A terminal is an instance of the `terminal` noun in the `terminal` base mode: its `any`/`paste` rows forward everything to the pane, `ctrl+a` enters `leader_pending` (`Payload{Shadow, Prefix, Request}` plus the sequence timer), under which `d` detaches, `ctrl+a` sends itself, a bound key runs, an unbound key sends leader and key in one `Send`, and the timeout sends the leader alone. Frames reach the component only through `Runtime.Deliver` with a current ticket; the stream (daemon client) is the application's, never the framework's.
- Never bind function keys (F1 to F12) anywhere, including demos, sandboxes, and tests. Use ordinary keys or Ctrl/Alt combinations and keep help text in sync with the real binding.
- This is Bubble Tea **v2**. The v1 idioms are gone: no `tea.KeyMsg` struct (`tea.KeyPressMsg` and `tea.PasteMsg`; a paste reaches `Dispatch` as `MacroKey{Key: "[text]", Paste: true}` and matches only `paste` rows), no `View() string` (`View() tea.View` with `AltScreen` set there, not via program options), no `lipgloss.AdaptiveColor` or renderers (`Appearance.Dark` from `tea.BackgroundColorMsg` feeds `lipgloss.LightDark`), no `github.com/charmbracelet/{bubbletea,lipgloss,bubbles}` imports. Ctrl-Space is `ctrl+space`, not `ctrl+@`. Lip Gloss v2 always emits styles; tests inspecting rendered text use the `mono` theme or `ansi.Strip`.

## Checks

```sh
gofmt -l . && go vet ./...
go test ./...
go test -race ./...
go run ./examples/core
go run ./examples/full-project --smoke
```

## References: other Charm projects under ~/proj

Facts (versions, import counts, entry points, docs present) are generated into `docs/CHARM-INVENTORY.md` from `docs/charm-refs.txt`. This section is the judgment: which project to open for which question, and which two files to read first.

Version split matters. Only `gmtm` is still on Bubble Tea v1. This repo and everything else are v2 (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`).

| Project | Open it for | Read first |
|---|---|---|
| `gmtm` | Closest v1 sibling. Bubbles textinput/viewport/textarea, `x/vt` terminal emulation, TUIOS protocol client, path picker, JSON explorer | `docs/architecture.md`, `docs/tuios-protocol-notes.md`, `internal/app/run.go`, `internal/ui/pathpicker/run.go` |
| `matomeru` | v2 workspace app. Command bus, CLI-first design so agents drive it without keystrokes, SQLite persistence, ultraviolet renderer | `AGENTS.md`, `cmd/mtm/wiring.go`, `internal/bus` |
| `redeck-astra`, `redeck-forms` | Variants of matomeru (same Go module name). Treat as branches; check `git log` before trusting either over `matomeru`. `redeck-forms` for form work | `AGENTS.md`, `cmd/mtm/wiring.go` |
| `redeck` | v2 dashboard over the tuios daemon built as independent units on a command bus and event runner. Has a written contract for units | `docs/agent-brief.md`, `docs/contract/`, `internal/ui/run.go` |
| `tuios-deck` | v2 dashboard with both a TUI and a web page over one data layer. Focus and history units | `AGENTS.md`, `cmd/deck/main.go`, `internal/focus` |
| `session-browser` | Smallest clean v2 example: two-pane browser, 21 files, no TUIOS dependency | `internal/tui/model.go`, `docs/plan.md` |
| `tuios-inbox` | Smallest v2 TUI, 8 Go files, fronting a Bun HTTP server | `cmd/tuios-inbox-tui/main.go`, `internal/tui` |
| `tuios` | The terminal multiplexer itself. v2 plus ultraviolet, `x/xpty`, colorprofile, and the SSH stack below. Heaviest reference; use its docs, not its source, unless you need one mechanism | `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/LIBRARY.md`, `docs/protocol.md` |
| `tuios-session-browser` | Fork of tuios (same module) adding session capture and `fang` for styled CLI help | `docs/SESSION_CAPTURE.md`, then as tuios |

### The Charm SSH stack used by tuios

- `charm.land/ssh`: an SSH **server** library, a fork of gliderlabs/ssh, wrapping `x/crypto/ssh` with a net/http-like API. Provides `ssh.Session` (the per-connection handle with PTY, env, and I/O), `ssh.PublicKey`, `ssh.Context`, and `ssh.PublicKeyHandler`. tuios uses it to parse `authorized_keys` and as its connection type in `internal/server/auth.go` and `internal/app/ssh_conn.go`.
- `charm.land/wish/v2`: an SSH **app server** built on that library. `wish.NewServer` takes options (`WithAddress`, `WithHostKeyPath`, `WithPublicKeyAuth`, `WithMiddleware`) and runs a middleware chain per session. `wish/bubbletea` runs a Bubble Tea program for each SSH client and `wish/logging` logs connections. tuios uses it for `tuios ssh` in `internal/server/ssh.go`.
- `charm.land/tone`: CharmTone, Charm's color palette. Every color is a `tone.Key` that satisfies `color.Color`, so it drops straight into Lip Gloss. Named colors such as `Pepper`, `Char`, `BBQ`, `Iron`, `Butter`, `Smoke`, `Squid`; `tone.Keys()` lists them. tuios uses it for its default UI palette and contrast checks in `internal/theme/ui.go`.

### Other Charm packages seen in the inventory

- `github.com/charmbracelet/ultraviolet`: the cell-buffer renderer under Bubble Tea v2. tuios draws directly with it.
- `github.com/charmbracelet/x/ansi`: ANSI parsing and width measurement. Used everywhere, including here.
- `github.com/charmbracelet/x/vt`: in-process terminal emulator (gmtm). `x/xpty`: cross-platform PTY (tuios).
- `github.com/charmbracelet/colorprofile`: detects terminal color depth and downsamples.
- `github.com/charmbracelet/fang`: styled help and errors for Cobra CLIs (tuios-session-browser).
