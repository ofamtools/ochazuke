# Migration plan: Bubble Tea v2, then nouns and instances

Design source: the published assessment "Ochazuke C4 assessment and Scope redesign" (http://127.0.0.1:4387/items/863) and `~/claude/docs/ochazuke-c4-and-scope-redesign.md`. This file is the executable plan. Each phase ends green (`go test -race ./...`), with `make codemap` run and README/AGENTS.md updated for anything it changed, and is committed on its own.

Order matters: the v2 move goes first because it is mechanical and small (Tea is imported by model.go, bindings.go and picker*.go only), and because the router rewrite in phase 5 must be designed against v2 key semantics, not v1.

## Principle: fresh core, transplanted widgets

Roughly 650 of 6,300 source lines change shape: layer, frame, page and context plumbing, `dispatchKey`, per-row list layers, the view and modal composition. The editor, undo tree, registers, macros, picker, palette, registry, transport and the 4,200 lines of tests stay. So the new core (nouns, instances, resolution, delivery) is written as new files from the design document, the widgets are wired onto it, and the old plumbing is deleted once nothing references it. No rewrite of what already works.

## Phase 0: lock behavior

Status: done, commit 19b7a0f.

Goal: make the tests version-independent and capture today's dispatch order before touching anything.

- Add a test helper `press(m, "ctrl+x")` that builds a Tea key message from the same key strings bindings use. Replace the 82 direct `tea.KeyMsg` / `tea.Key*` constructions in 8 test files with it. Swapping Tea versions later changes one function.
- Add golden tests for `dispatchKey` ordering: macro pending, palette active, paste, input pending count, binding match, esc on a modal, editor fallthrough. These are the behaviors phase 5 must preserve.
- Acceptance: tests pass on v1 with no production code changed.

## Phase 1: Bubble Tea v2, mechanical

Status: done, commit 5666536. Decisions: paste still reaches dispatchKey as `[text]` (phase 5 may revisit); Ctrl-Space is `ctrl+space`; go directive 1.26.

- `go.mod`: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`; bump `go` directive as the modules require.
- `Model.View()` returns `tea.View`; `AltScreen` set on the view, `tea.WithAltScreen` removed from cmd and examples.
- `Update`: `case tea.KeyPressMsg` builds `MacroKey{Key: v.String()}`; new `case tea.PasteMsg` builds `MacroKey{Key: v.Content, Paste: true}`. Delete the rune-splitting loop in model.go and cmd/ochazuke/keybinding.go.
- Bubbles: `viewport.New(viewport.WithWidth, WithHeight)`, setters for Width/Height/YOffset; `filepicker.DefaultStyles()`; textinput style struct.
- Theme: `tea.RequestBackgroundColor` in `Init`, `lipgloss.LightDark(msg.IsDark())` on `BackgroundColorMsg`, `color.Color` for the two `TerminalColor` uses. Hold the dark flag in `Appearance`.
- Swap the phase 0 helper to v2 constructors. `KeyCtrl*` constants are gone; use `Code` plus `Mod`.
- Acceptance: all tests green, demo runs, `ochazuke keybinding` sandbox runs, `space` key still matches `"space"` bindings.

## Phase 2: real cursor and modal composition

Status: done, commit f4a3ec5. Decisions: `Page.Render` and `ModalView.Render` return `Block` (pages without inputs return `Text(s)`), since the cursor cannot reach the shell through a string; `InputBlock` always carries its input's cursor and the caller composes it only for the focused input (`Input` has no focus flag; focus is runtime state); string wrappers (`InputView`, `Page/Panel/Split/Sidebar`, `ComposeModal`, `Picker.View`) remain; a modal drops the cursor of everything beneath it, and the shell hides the cursor unless `Context().InputID` names an input; the palette reports its search and argument editors. Lip Gloss notes: `Canvas.Compose(layer)` ignores the layer's X/Y, so the two layers go through `NewCompositor`; `Canvas.Render` trims trailing blanks, so rows are re-padded with `fitSurface`.

- Introduce `Block{Text string; Cursor *Pos}` as the render result of `InputView`, `Layout.Panel/Split/Sidebar`, `ComposeModal` and `Model.View`. Each composer offsets the child cursor. Only the focused component supplies one. Set `tea.View.Cursor` from it, shape by state: bar in insert, block in normal, underline in visual.
- Remove the `\x1b[7m` fake cursor from `InputView`; keep selection painting.
- Rebuild `ComposeModal` on `lipgloss.NewCanvas` with two `Layer`s (backdrop, modal). Drop `ansi.Cut` and the reset patching.
- Acceptance: cursor_view, surface and layout tests updated to the new shape; a 56x18 modal and a 48x15 layout render as before minus the fake cursor.

## Phase 3: nouns and instances under the existing layer API

Status: done, commit ae3b321. Decisions: `Runtime.Focus(id)` already names the focus method, so the leaf is read through `FocusID()` and derived from `Context()`; nouns are keyed by layer ID (full-project rows share a display noun but have distinct bindings, which phase 4 resolves); an input whose ID is not also a layer is an instance without a noun, since the editor noun arrives in phase 5; a second parent for one instance ID is recorded by `AmbiguousInstances`, not guessed, and the demo and full-project sessions record none.

No behavior change. This phase only adds the new model beneath the old one.

Write the new core as new files from the design, not as edits to runtime.go: `noun.go` (Noun, registration, verb and binding declaration), `instance.go` (Instance, parent links, focus, derived ancestry), `resolve.go` (the stack walk, used by phase 5). runtime.go only gains the wiring that builds nouns and instances from existing layers. The old plumbing is deleted in phase 5, not rewritten in place, so runtime.go shrinks to the registry and Execute.

- Add `Noun{Name, Verbs, Bindings map[state][]Binding, Actions, Grammar}` and `Instance{ID, Noun, Parent, State}`.
- Build them at registration: every existing `Layer` becomes a noun of the same name plus one instance of the same id. `AddInput`, `NewList`, `RegisterPage`, palette and picker registration set `Parent` from the path they already receive.
- `Runtime.Focus` is one instance id. `Context()` is derived: ancestors of focus, plus `InputID` when the focused instance is an input. Keep `Layer` as a read view over its noun for callers and tests.
- Acceptance: all phase 0 golden tests and the Execute-driven suite pass unchanged.

## Phase 4: items name nouns, targets are instances, one mode rule

Status: done, commit 7c6bd0f. Decisions: `ListItem.Layer` was renamed to `ListItem.Noun` outright (all callers are in this repository), and a row's noun must be registered (a `Noun`, which in this phase is also a layer of the same name); a row instance is created by `SetItems` with `Parent` = the list and removed when the row disappears, a retained row keeps its instance and any `Instance.Bindings` override, and a row ID may not be a layer, an input or another list's row; `Context.Path` may now hold row IDs, so `validateContext`, `ExecuteAt`, `EffectiveBindings` and the picker's action loop resolve a non-layer element through its instance's noun (`layerFor`), which keeps the old loops and the new walk in agreement for the phase 3 equivalence tests (the test helpers that re-implemented the layer loop were taught the same rule); `Request.Target` and `Runtime.Focus` accept any instance, inputs still through their registration path; `context.select` prefers `instance` and keeps `path`; `Runtime.Mode(c)` reads the focused instance's `State` with ancestor inheritance (fixing the top-frame bug), `mode.set` on a frame writes the innermost path element's instance (so a mode set on a modal body whose path is only `app` is now visible beneath it, as the instance rule implies), and `Input.Mode` remains a mirror written by `Input.storeMode` so the editor compiles unchanged; the full-project Items page mixes `repo`, `worktree` and `branch` rows with per-noun verbs, keys, a `?` bindings modal and a `rows` palette source.

- `ListItem.Layer` becomes `ListItem.Noun`. Rows are instances with `Parent` = the list; `SetItems` creates and drops them. Delete per-row layer registration from the README and examples.
- `Request.Target` resolves any instance id. Inputs, lists and (later) terminals share one addressing.
- `Runtime.Mode(c)` reads the focused instance's `State`. Delete `Frame.State["mode"]` and `Input.Mode` as separate truths (Input keeps a setter that writes the instance state).
- Per-instance binding overrides allowed above the noun's bindings.
- Acceptance: the full-project Items page works with mixed nouns in one list; `?` shows different verbs for different rows.

## Author-facing API points (inputs to phases 5 to 7)

From a usage sketch of the finished framework done with the owner in the forked design session on 2026-10-24. These shape constructors; each phase applies the ones it touches.

1. **Declaration verb.** `r.Noun("x")` reads like a trigger. Declare with `r.Define("worktree")` (verbs, bindings, handlers); keep noun and verb as the user-facing vocabulary in the palette and CLI. Phase 5.
2. **Unified instance constructors** taking Parent and Noun: `r.Page(id, title, Noun(...), render)`, `r.List(id, Parent(page))`, `r.Input(id, Parent(page), Noun("prompt"))`, `r.Terminal(id, Parent(page), stream)`. Today these are four unrelated registration paths. Phases 5 and 7.
3. **Binding argument sources.** `FromKey("register")` (planned), plus `FromPayload("id")` to read the focused instance's or modal's payload, and `Run(FromPayload("request"))` to execute a stored Request. Phase 5.
4. **Modal payload and per-verb confirm.** `ctx.OpenModal(kind, Payload{...})` creates one instance parented at the focus. Confirmation is a property of a verb, not a noun per call site: `Define("worktree").Verb("remove", h).Confirm("Remove {{id}}?")` compiles to a framework `confirm` noun whose `y` runs the stored request and `n` pops. Config can add or remove a Confirm on any verb. Phases 5 and 6.
5. **Application handler signature.** `ctx` carries the Instance (id, noun, payload); `Result` expresses done, failed, or pending with a generation. The phase 6 delivery contract appears at the author API, and application verbs run off the loop. Phase 6.

Already decided: per-instance overrides allowed and rare; verb names shared across nouns with the pair as the atom; the `list` noun owns only list verbs; multi-selection fans out `<row noun>.verb` per row.

## Phase 5: one stack walk over mode tables, everything is a noun

Status: done, commit d46c78e. Decisions:

- The stack is the focused instance's ancestry outward to `app`, with the `modal` noun's level inserted just inside `app` while a modal is open, and the `editor` noun appended as the fallback level of every stack. Framework defaults (Readline, Vim, counts, operators, registers, macros) therefore lose to any application row on the stack for a fresh key, which is what the phase 0 goldens require ("fresh key prefers layer binding", "app `i` beats editor `i`") and what the invariant "Readline rows are defaults, overridable per noun/instance/config" says; pending modes are exclusive so the grammar still wins mid-sequence. The `prompt` noun pins insert and binds nothing; `palette-field` is an ordinary inner noun whose own `printable`/`paste` rows beat app any-mode rows (the phase 0 finding), and palette-body rows are guarded by `self` so a field beneath keeps its own.
- Rows read at one level are merged: instance rows, then the noun's mode table, then its any-mode table (base modes only), with exact keys ranked above classes (`one-of` < `digit`/`alpha` < `printable`/`paste` < `any`) and guards (`when`) choosing among candidates in table order. Multi-key sequences are not pre-compiled into tables: the resolver matches sequence rows against the typed prefix, and the prefix mode `"<base> <keys so far>"` is a pending mode on the focused instance with `Payload{Prefix, Request: fallback}`; the timer (`BindingTimeoutMsg`, same generation guard) and a mismatching key leave it, run the fallback, and the mismatching key is dispatched fresh (the bindings suite's "mismatch consumes x" became "runs the match then x"). Escape cancels without running anything.
- Pending modes are one-shot: the answering key leaves the mode before its request runs unless the request is `mode.enter`, `count.append`, `mode.leave` or `input.cancel`; that is what lets a replayed macro dispatch its keys in the restored mode. `Payload` is `{Return, Count, OpCount, Op, Prefix, Request, Shadow}`; the register is not in the payload because `ActiveRegister` already holds it. `Input.Count()`/`Pending()` read the instance; `Input.Mode` mirrors only the editing mode (`Payload.Return` while pending). A pending mode entered on an inheriting leaf leaves back to `""`. Counts have two tables (`count_pending` extends `normal`, `visual_count_pending` extends `visual`) because the base rows differ; `operator_g_pending` and `g_pending` carry the operator and count in the payload; `count_pending`'s `any` row leaves and re-dispatches (`replay: true`), the other pending modes swallow an unknown key as before. The `space` key is not printable; it has an explicit insert row.
- Recording is the `macro_recording` shadow mode implemented as `Func` rows on the `app` noun's `macro_recording` table (`any`, `paste`, `q`), applied by `Dispatch` whenever `Macro.Recording != ""` and `Depth == 0`; replay keeps the depth flag. `MacroState.Pending/Count` and `prefix.macro` are gone. `leader_pending` is only a unit test of the same shape (`TestLeaderPendingShadowShape`).
- Owner additions relayed mid-phase, done here: pages are instances of kind `page` under `app` (`Instance.Component` holds the `*Page`; `Runtime.Page` is the current page; `PageList()` is app's page children in registration order via a per-instance sequence; `Pages`, `PageOrder`, `ActivePage` are gone) with the invariant "`Runtime.Page` is an ancestor of the base frame's focus" asserted in the demo and full-project session tests; a page that starts on a list starts on its selected row. The binding config is the row shape (`{noun|instance, mode, keys|pattern, request|null, from, when, replay}`) loaded by `bindings.load {path}` / `LoadBindings` / `Bind`, printed by `bindings.list`; `set.binding` is gone. The editor tables are `editor_bindings.json`, embedded and loaded at init with three generators (`expand`, `extends`, `pending`); rows for `input` operations get the `input` guard automatically so a page focus can still enter macro and replay modes. `editing.go` and `editor.go` keep only the handlers; no key-name switch remains.
- Deleted: `Layer`, `Runtime.Layers`, `InputPaths`, `Frame.Context`/`Frame.State` (a frame is `{ID, Focus}`), `validateContext`, `validateSelection`, `SelectContext`, `syncNouns`, `RegisterNoun`, `EffectiveBindings`, `EffectiveBindingsFor`, `ContextFromFocus`, `AmbiguousInstances`, `HandleInputKey`, `literalInput`, `Input.prefix`, `Input.ReadlineOnly/Count/Pending/operatorCount`, `Model.dispatchKey`, `Runtime.dispatchKey`, `prefix.input`, `prefix.macro`, `set.binding`. Moved: scroll offsets to `Model.scroll`, the yank-history selection to `Runtime.yankSelection` (the modal renders its own window), the full-project example's page state to its own map. Carry-overs to phase 6 as the owner listed: `Runtime.Inputs` and `Runtime.Lists` as instance payloads; registers, yank history, active register and macro state into one component.
- Registration API: `r.Define(name)` builder (`Verbs`, `Verb`, `Action`, `Pin`, `Mode(m).Bind/Pattern/Unbind/When`), `r.Instance(id, Parent(..), Kind(..))`, `r.AddInput(in, Parent(..), Kind(..))`, `NewList(r, id, items, Parent(..))`, `RegisterPage(&Page{ID, Title, Focus, Render})`, `Push(&Frame{ID, Focus})`. Row constructors `Do`, `Enter`, `Leave`, `Func`, with `.FromKey`, `.FromPayload`, `.Guard`. Guards are per runtime (`DefineGuard`). `context.select` keeps `instance` and `path`. Multi-selection: `list.mark` (Space on the `list` noun) and `list.apply {verb}` fan `<row noun>.verb` out per marked row with a skipped report.
- Golden tests: ten of the eleven pass against the new walk unchanged in intent (rewritten as mode/key/next-mode tables where that was clearer). `TestDispatchDropsUnmatchedBindingPrefix` changed deliberately: the prefix is a mode, the mismatched key reaches the editor, and an app `any` row sees the mismatched key but never the prefix key. `TestDispatchPaletteHonorsAnyModeAppBindings` changed from documenting the phase 0 finding to asserting its resolution. Tests that bound function keys now bind `ctrl+t`.

Design confirmed by the owner on 2026-10-24 (confirmation relayed through the forked design session). The walk reads **mode tables only, no grammar hooks**. Every pending condition is an exclusive mode with a small typed payload, and the resolver is one lookup per stack level.

```js
// resolver, complete
for (inst of stack(focus)) {
  table = nouns[inst.noun].bindings[state.mode.name]
  hit = table[key] ?? table[classOf(key)] ?? table.any     // exact, then class (digit, alpha, printable, alpha-q, one-of), then any
  if (hit) return hit(key, state, inst)
}

// modes carry payload {register, count, op, shadow}
normal:           { i: mode.set insert, d: enter operator_pending{op:delete}, g: enter g_pending, <digit>: count.append, q: enter macro_pending }
operator_pending: { w: motion.apply word, d: line.apply, any: enter normal }
g_pending:        { g: move first-line, any: enter normal }          // "g g" is sugar for this; the ambiguity timeout is a timer cmd that fires mode.set with the fallback, same generation guard as today
macro_pending:    { <alpha>: macro.start{register:key}, any: enter normal }
macro_recording:  { any: macro.append(key) then re-dispatch in payload.shadow and store the next mode back in shadow,
                    q: stop only when shadow is normal with empty payload }   // replay keeps the depth flag so replayed keys are not re-recorded
leader_pending:   { <bound>: run command, any: forward prefix+key to the pane, restore shadow }   // terminal leader, same shape
```

Binding row shape, identical in code and config: `{noun, mode, keys | pattern, request | null, argsFromKey?}`. Same noun+mode+pattern replaces a default, a new pattern adds, `null` unbinds. `bindings.list` prints exactly this for the current stack and mode. Go sketch: `r.Noun("editor").Mode("normal").Bind("d", Enter("operator_pending", Payload{Op: "delete"})).Bind(Digit, Do("count.append"))`; `.When(ShadowIs("normal"))` for guarded rows; `Func(...)` only where re-dispatch is needed (recording, leader).

Work:

- Implement the resolver above in `resolve.go` (phase 3 left `EffectiveBindingsFor`/`ActionFor` there; extend the row shape with patterns and `null`, and the mode with a typed payload). Replace `dispatchKey` with it.
- Move the Readline switch and the Vim switch from editing.go into the `editor` noun's mode tables, generated from a compact spec: Readline rows in `insert`, Vim rows in `normal` and the visual modes, with `operator_pending`, `g_pending`, count and register handling as pending modes with payload. `bindings.list` and help now report all of it, pending modes included.
- `prompt` noun replaces `ReadlineOnly`: editor tables, mode pinned to insert, Escape unbound so a page or modal can bind it.
- Palette becomes a noun on a modal instance; "esc pops a modal" becomes a row in the `modal` noun; macro recording becomes the `macro_recording` shadow mode (replay keeps the depth flag). The terminal leader, when phase 7 adds it, is `leader_pending` with the same shape.
- Multi-selection fan-out: a verb over marked rows runs `<row noun>.verb` per row, skipping and reporting unsupported nouns.
- Delete `InputPaths`, `Frame.State`, `validateContext`, `validateSelection`, `Layer` and the layer code in runtime.go, pages.go and configuration.go. Deletion, not editing: anything still needed is already in the new files.

Consequences and care points: the phase 0 goldens become table rows (mode, key, next mode, action); users can rebind inside pending modes; payload is a struct, not a map; help in a shadow mode shows the shadow's bindings plus the mode's own rows (a presentation rule, not a resolver rule); the phase 0 finding that the macro grammar is consulted twice per key disappears by construction.

Acceptance: phase 0 golden tests pass against the new walk, with the one intended change (unmatched prefix handed to the innermost mode's `any` row instead of dropped) updated deliberately; `bindings.list` lists every key the editor answers to.

## Runtime end state (owner input relayed from the forked design session, 2026-10-24)

After phase 6, `Runtime` is: operation registry + instance registry (nouns, instances, parent links) + focus + `Page` + modal chain + pending-sequence state. No component-specific fields.

1. `Runtime.Inputs` and `Runtime.Lists` go away: inputs and lists are instances with typed payloads looked up through the instance registry. Phase 6.
2. `Registers`, `YankHistory`, `ActiveRegister` and `Macro` move off `Runtime` into one `registers` component under the phase 6 delivery contract, reached through operations, not fields. Phase 6.
3. Pages are instances of kind `page` under `app` with an explicit `Runtime.Page`; done in phase 5.
4. Binding config is the row file loaded by `bindings.load` and printed by `bindings.list`; done in phase 5.

## Phase 6: component delivery contract and operation kinds

Status: done, commit 98d6401. Decisions:

- Delivery contract (`delivery.go`): `Component{HandleMessage(tea.Msg) bool; PendingCommand() tea.Cmd; Close()}` on `Instance.Component`; results are `Completion{Ticket{Owner, Generation}, Value, Err}`. One generation per instance (`Instance.generation`, unexported): `Runtime.Start(owner, work, then, ctx)` cancels the owner's earlier work, bumps the generation and queues a `tea.Cmd`; `Deliver` hands a current completion to the owner's `then` callback and `HandleMessage`, and drops a stale or orphaned one; `closeInstance` (called by `Pop` for modal instances, `SetItems` for dropped rows, `removeInstance`) cancels work, retires the generation and calls `Close`. `Model` has no `Pickers` field: `Update` calls `Runtime.Deliver` first and `Runtime.PendingCommands` last, and the runtime walks the instance registry for components. The picker keeps its three per-stream generations internally and only gained `Close`; the palette is not a component (no off-loop work).
- Operation kinds: `Operation.Kind` is `interaction` (default) or `application`; `Operation.App` is an `AppHandler func(Ctx) Result` with `Ctx{Runtime, Context, Args, Instance}` and `Done(v)`, `Failed(err)`, `Pending(work func(context.Context) (any, error)).Then(fn)`. `Define(noun).Verb(name, h)` takes a `Handler` or an `AppHandler` (parameter type `any`, checked at registration, so existing function literals compile); an application verb cannot override an existing operation. `Pending` returns the `Ticket` to the caller (CLI and palette see it as the result) and the completion lands through the contract. `operations.list` and docgen carry `kind`; the palette labels application verbs `verb · application` and confirmed verbs `· confirm`, and its argument header shows the kind; the full-project `?` modal appends the kind per row.
- Modal payload and confirm: `OpenModal(kind, Payload)` creates an instance `{ID: kind, Noun: kind, Parent: focus, Payload}` registered as a modal instance and pushes `Frame{kind, kind}`; the stack walk already puts it first. `Payload` gained `Prompt`; `Request` gained `Confirmed` (on the wire, so scripts can skip prompts). `Operation.Confirm` holds the template; `ExecuteAt` opens the confirm modal instead of running when it is set and the request is not confirmed, storing `{request, Confirmed: true, Target: focus}` so `y` (`confirm.accept`: pop, then `Execute`) runs it at the origin even though the confirm body is focused. The `confirm` noun's any-mode table: `y`/`enter` accept, `n`/`esc` pop, `any` ignores. `.Confirm` applies to the last `Verb` on the builder; `confirm.set {operation, prompt}` adds or removes one at run time; the full-project `worktree.remove` confirm comes from a `confirm` field in `actions.json` and the smoke presses `d n` then `d y`.
- Runtime end state: `Runtime.Inputs`, `Lists`, `Registers`, `YankHistory`, `ActiveRegister`, `Macro` and `yankSelection` are gone. `Instance.Component` holds the `*Input` or `*List`; `r.Input(id)`, `r.List(id)`, `r.Inputs()`, `r.Lists()` read the registry. `Registers{Named, History, Active, Macro, selection}` is the component of the `__registers` instance under `app`, reached through `r.Registers()` and the existing operations; it implements the contract (Close abandons a recording). The remaining fields are `Nouns, Instances, Frames, Page, Modes, Operations, Quit, Pending, BindingTimeout, pendingModes, guards, prefix, bindingVersion, modeGeneration, rows, modalInstances, seq, commands`, asserted by `TestRuntimeHoldsNoComponentFields` (the "transport hook" of the end-state note is `commands`, the queue the adapter drains). Snapshot keeps `/inputs`, `/lists`, `/registers`, `/yankHistory`, `/activeRegister`, `/macro` and adds `/components`.
- Editor fallback (review item relayed mid-phase): `stack()` appends the `editor` level only when an input (`r.Input(id) != nil`: editor, prompt or palette field, which is why the test is on the component and not the noun name) is on the ancestry; otherwise the walk ends at `app`. So that a page or row focus still counts, selects registers, records and replays, the `q`, `@`, `"`, `digit-0` and `count_pending` rows moved from the editor spec to `registers_bindings.json`, loaded onto the `app` noun after the editor spec (with the same `extends` so `count_pending` carries them); the app `count_pending` `any` row is guarded by the new `no-input` guard so an input's own `count_pending` rows (motions, `0`) still win. The editor keeps `visual_count_pending` and its own `count_pending` `esc`/`any`. `TestEditorFallbackNeedsAnInputOnThePath` covers: no editor rows listed for a row, `x u p i d` unhandled there, `2 q` still enters `macro_pending`. All eleven goldens and the phase 5 resolver tests pass unchanged.
- Not covered by the design and left as is: more than one outstanding generation per instance (a new `Start` supersedes), progress reporting from pending work (only completion), and cancelling application work from the keyboard (closing the instance is the only cancel).

- In a new `delivery.go`, generalize the picker's `HandleMessage`/`PendingCommand`/generation pattern into a contract every component implements: result carries owner id and generation; stale results rejected; closing an instance cancels outstanding work and releases held resources.
- Tag each `Operation` as `interaction` (runs on the loop) or `application` (dispatched to an application handler that performs I/O off the loop and reports through the contract). Palette, CLI and help show the kind.
- Acceptance: a fake slow application command cannot block key handling; a closed picker's late result is dropped.

## Phase 7: prove it

Status: done. Commits, in order: 9f9a0d6 (chrome unification), 86824ef (demo moved to `examples/demo`), 46b8c8b (constructor family and the terminal instance), 15c0b76 (the Console page, the acceptance check), 963df65 (docs), then a3e3d59 and 5419e32 (the chrome snapshot test, which the chrome commit had failed to save, and its corrected insert-mode assertion). Decisions:

- Chrome: `Layout.Shell(Header{Title, Badge, Subtitle, Tabs, Active}, body, Footer{Text, Error})` is the one page chrome; `Layout.PageBlock` delegates to it and `Model.View` renders through it. Deleted from model.go: `surfaceSize` (`Height-6`, `2*Padding` inset), the heading/tabs/inset composition, `pageTabs` (now `Layout.tabs`) and every hard-coded hint string: the normal footer (`Ctrl-J/K pages · : palette · PgUp/PgDn scroll · Ctrl-C quit`), the `INSERT` and `READLINE` footers, the `Record macro`, `Replay macro`, `Register` and `Pending … · Esc cancel` statuses, and the three modal hints (`Esc normal / close`, palette, Readline). `layoutChrome` and `layoutGutter` in layout.go are the only chrome constants left; the shell's horizontal inset no longer follows density (the gutter is fixed at two cells, density still sets panel padding). Hints are `BindingHints(r, focus)`: `Runtime.bindingRows` in walk order (innermost level first; `ListBindings` sorts the same rows), `key noun.verb` joined by ` · `, a `Func` row shown by its key alone. `bindingRows` stops at a level with an unguarded `any` row, as `Dispatch` does, so the terminal footer lists the terminal table only. A modal's hint may widen its panel up to 48 cells or the body width. `TestModelMacroRegisterTransitionsAndPalette` now asserts the derived `<any> macro.start` instead of the deleted prompt text; the goldens, resolver and delivery suites are unchanged.
- Demo: `examples/demo` (`demo.New()`), with local `safeText`, `firstCursor`, `focusOf` and `modeName` helpers; its eight tests moved to `examples/demo/demo_test.go` against exported API (`Picker.Close`, `Frames`, `Modes`). `cmd/ochazuke run` and `cmd/docgen` import it.
- Constructors: `Runtime.Page` (the current page id) and `r.List(id)`/`r.Input(id)` (lookups) already own the short names, so the family is `AddPage(id, title, render, Focus(..), Kind(..))`, `AddList(id, items, Parent(..), Kind(..))`, `AddInput(in, Parent(..), Kind(..))`, `AddTerminal(id, stream, Parent(..), Kind(..))` over one `InstanceOption` set (`Focus` is new; pages must stay under `app`). `RegisterPage` and `NewList` remain as thin wrappers with identical semantics; the examples use the `Add*` forms.
- Terminal: `TerminalStream{Next, Send, Paste, Resize}` is the application's daemon client (the examples use an echo stream); the fake in the framework tests is a line-buffer stream with explicit cursor coordinates, no emulator (`x/vt` was not needed). Each frame read is `Runtime.Start` work owned by the instance, so the generation advances per frame and a frame completing after `Close` or a newer read is dropped by `Accepts` before the component sees it; the terminal re-arms the next read on delivery and `PendingCommand` is nil. `ViewBlock` resizes the stream when its area changes. `Model.cursor` shapes the cursor per the pane's `Shape` while the terminal is focused. Leader: `ctrl+a` is a `Func` row entering `leader_pending` with `Payload{Shadow, Prefix: "ctrl+a", Request: send ctrl+a}` and arming the sequence timer through `Runtime.armPrefix` (split out of `enterPrefix`), so `finishPrefix` forwards the leader on timeout through the existing timer; `leaveMode` disarms the timer of the mode it leaves. Under the leader: `d` detach, `ctrl+a` literal, bound key runs, `any` and `paste` `Func` rows forward leader plus key and return. Escape under the leader is forwarded, per the component plan, not treated as cancel. Paste outside the leader is `terminal.paste` with the text; a paste under the leader sends the leader then pastes.
- Second page: commit 15c0b76 touched `examples/full-project/README.md`, `config/bindings.json`, `config/instances.json`, `config/pages.json`, `main.go`, `pages.go` and the new `terminal.go`; `git show --stat 15c0b76` lists no other file, so model.go, runtime.go, resolve.go, noun.go, instance.go and delivery.go are unchanged by it. The page's pane is `AddTerminal` under the page from config (renderer `terminal`, focus `pane`), its leader rows come from `bindings.json`, and the smoke drives the pane through the commands `Update` returns.

Owner input (relayed from the forked design session): unify the chrome. One `Layout.Page` used by the shell instead of `Model.View` and `Layout` each reserving rows with their own padding; footer and modal hint lines derived from the resolver's effective bindings for the current stack and mode, as picker_view.go already does, so hints are the same data `bindings.list` prints; delete the hard-coded hint strings in model.go and the duplicate height constants.


- Move `demo.go` out of the root package into `examples/demo`.
- Add a second page to `examples/full-project` without editing model.go or runtime.go. That is the architectural acceptance check.
- Add a live terminal instance (`terminal` noun, passthrough grammar, leader binding) fed by a fake stream in tests. The real daemon client comes with the application, not the framework.
- Acceptance: both checks pass; README and AGENTS.md describe nouns and instances instead of layers.

## After phase 7

Owner-confirmed items, relayed from the forked design session:

1. `Parameter` gains `enum` (static choices) and `source` (a verb that returns choices, such as `list.projects`). Palette argument fields, forms and CLI completion render a pick list from them. Palette modes are unaffected.
   Status: done for the palette and CLI help (branch param-source). `Parameter.Enum`, `Source` (an operation key, `noun.verb`) and `Free`; palette argument fields become filtered pick lists (`Ctrl-N/P`, Enter picks); `Execute` validates Enum statically and Source lazily, skipping a failing source; `ochazuke NOUN VERB --help` prints choices. Deferred: forms and shell completion.
2. docgen is retargeted, not dropped: generate a reference manual from the `Define` data (per noun: verbs, params with choices, confirm, interaction or application kind; per noun per mode: key tables; app page children). Hand-written docs stay (architecture guide, input modes, this plan). Delete only stale output: visual-check PNGs, pre-migration architecture-map json and html, layer-era bindings.html and actions.html that docgen regenerates.

Noted during phase 7, not done: `Terminal` frames are rows of ANSI, so a cell-buffer source (`x/vt`) would be adapted by the stream, not the framework; a pane that needs a read to restart after a dropped command re-arms only on delivery; hints have no per-row description (rows show `key noun.verb`).

Follow-up after the phases (commit ad800ea): binding rows are atomic. `Binding` lost its `Func` field; macro recording is `macro.stop` under the `shadow-idle` guard plus `macro.record` (`macro.append`, then `key.dispatch {mode: "shadow"}`), the terminal leader is an `Enter` row with `prefix` and `request` and `terminal.send` with `Join("keys", prefix, key)`, Escape under a key-sequence prefix is a generated `"<prefix> esc"` → `mode.leave` row, and hints carry the operation description (the per-row description noted above). `TestBindingRowsAreAtomic` and `Runtime.ValidateBindings` enforce it.

All phases complete.

## After phase 7: delivery contract amendment (owner decision, 2026-10-24, from the forked design session)

Two changes to the phase 6 contract in `delivery.go`. Nothing in the CSV app depends on them; the znbu app does immediately.

### 1. Staleness is per job and opt-in, not per owner

Status: done on branch `delivery-jobs`. `Ticket{Owner, Job, Slot}` replaces `Ticket{Owner, Generation}` (and `Runtime.Generation`); `Pending(work)` accumulates, `Pending(work).Replacing(slot)` replaces; `Runtime.Launch(owner, verb, result, ctx)` starts a job, `Issue`/`Accepts`/`Settle` are the ticket-level form for components that run their own commands (the picker's `index`, `search` and `preview` slots; the terminal's `read` slot). The verb is `jobs.list` (noun `jobs`), not `jobs.list`, to match `operations.list` and `bindings.list`; the job id is a number. Finished entries are retained until a `jobs.list` read returns them, at most 32; `state.get /jobs` shows running jobs. Tests: `TestComponentDeliveryAccumulatesIndependentJobs`, `TestComponentDeliveryReplacesJobsInASlot`; the picker tests are unchanged.

As landed, `Ticket.Generation` is per owning instance, so five independent jobs on one row (fetch, commit, push...) keep only the last completion. Wrong for independent work, right only for replaceable work such as a preview.

```js
ticket = { owner: "repo-1", job: "j42", slot: null }        // accumulate (default): every completion is delivered, in finish order, with its ticket
ticket = { owner: "pane-7", job: "j43", slot: "preview" }   // replace (opt-in): a newer job in the same slot on the same owner drops the older

Pending(work)                         // accumulate
Pending(work).Replacing("preview")    // replace
```

- A job is dropped only when its owner instance is closed, or when a newer job claims its slot. Never because the user did something else meanwhile.
- The runtime keeps a jobs list exposed as `jobs.list` → `[{ job, owner, verb, started, state }]`. Status rails and toasts render this list; entries clear on completion or show the error. Palette and help can show "fetch (running)" from the same data.
- Keep: Done / Failed / Pending results, work off the loop, completions applied on the loop, `Close` cancels and releases.

### 2. Nothing blocks; `await` is a mode

A verb that the user should wait for declares `await` (and a scope). The handler stays plain. While the awaited job is out, the owner (or the app) is in a pending `busy` mode whose table has only cancel rows; the spinner is how `busy` renders (verb name, elapsed, from the payload). Completion, failure or cancel leaves `busy` and restores the shadow.

```js
verbs: { save: { run: app.save, await: true, scope: "app" }, fetch: { run: app.fetch } }   // fetch is a background job
state.mode = { name: "busy", job: "j42", shadow: previous, scope: "instance" | "app" }
modes.busy = { "ctrl+c": "job.cancel", esc: "job.cancel", any: ignore }                   // ignore, not queue, to start
```

- `scope: "instance"` holds only the owner; `scope: "app"` holds everything (Vim `:w` feel).
- `await` and `scope` are properties on the verb row, so config can flip them per verb.
- `bindings list` in `busy` shows exactly the cancel rows.

Order: do change 1 first as a short correctness task (it is a bug in the contract and small); change 2 can land with the CSV app's open-file, which is its first real use.

## After the CSV app: consumer-found fixes (owner decisions, 2026-10-24, from the forked design session)

The CSV app (`examples/csv`, `grid/`) shipped against the core with one 14-line additive change. Its report named three framework gaps; checked against the resolver and runtime, they reduce to this list.

### 1. Mode is global (decision)

Vim has one mode. Phase 5 stored mode per instance (`Instance.State`, `modeState` walking the ancestry, `mode.set` writing to the focused instance), which breaks for components that recreate their focused child on every motion (the grid lost its visual mode on the first `j`).

```js
Runtime.Mode = { name, payload }          // one field; mode.set writes it; the grid's visual mode survives cursor moves
focus change → Mode = normal, payload cleared, unless the newly focused noun Pins a mode (prompt → insert, terminal → terminal)
```

Remove `Instance.State` as mode storage, the ancestry walk in `modeState`, and `Input.Mode` as a separate truth (an input is in the global mode while focused; a pinned noun sets it on focus). Per-instance data that is not mode stays where it is: editor text and undo, grid cursor, selection geometry and marks.

### 1b. Focus is a single value (decision)

Today `Runtime.Frames` is a stack of `{id, focus}` and `FocusID()` reads the top, so a modal can restore where you were. Replace it with one field, like Mode:

```js
Runtime.Focus = "confirm#1"                                       // one field
modal instance payload = { return: "r4:c2", request: {...} }      // Payload.Return already exists
modal.pop → Runtime.Focus = payload.return; remove the instance
```

`Frames` goes away; what is open is the chain of modal instances (already used for rendering). Runtime then holds `Focus`, `Mode` and `Page` as three plain fields. Invariant kept: `Page` is always an ancestor of `Focus`.

### 1c. Tab order is computed from focusable components (decision)

Like the browser without tabindex: focusability is a property of the kind, order is tree order, an explicit list is only an override.

```js
define("grid", { focusable: true }); define("status", { focusable: false })   // declared on the noun
instance = { id: "sidebar", noun: "list", focusable: false }                   // instance-level opt-out (disabled panel)
ring(page) = page.children in added order, depth-first, filtered to focusable; the walk stops at the first focusable instance on each branch (a grid is one stop, its cells are not)
focus.next / focus.previous / focus.set {id}   // runtime operations; tab / shift+tab bound on app
page.focusables (optional)                     // explicit order override, the rare case
```

- Nothing is stored by default; the ring is computed when Tab is pressed. Hiding a panel removes its stop because the instance is gone or marked unfocusable. No list to maintain.
- Modals compute their ring the same way over their own subtree.
- The runtime owns every focus change, including restore on page open and modal pop; consumer handlers move focus only through `focus.set`.
- Tab moves focus by default; a multiline editor that wants a literal tab binds it in its own insert table (inner level wins).
- Two levels of jump, two declared properties. `focusable` marks stops; `region` marks groupings (panels) that are never stops themselves. `focus.next {scope: "stop"}` (default) moves component to component, so one press leaves a 3000-row list for the button below it in the same panel; `focus.next {scope: "region"}` moves panel to panel and lands on the first (or remembered) stop inside. Both take `steps` from the count. Owner's default keys: `}` `{` for stops, `)` `(` for regions (the inverse of Vim's sentence/paragraph sizes; chosen deliberately); tab / shift+tab also bound to stops.
- Additive: `Focusable` and `Region` on Define and Instance, the three operations with a `scope` argument, optional `Page.Focusables`.

### 2. Bugs and oversights in the resolver

- `enterPrefix` builds `Payload{Prefix, Request}` and drops `Count` and `OpCount`, so `3gg` loses its count. Carry both.
- `lookup` records the matching level in `ev.Level`; `runRow` then targets `ev.Focus`. A row that matched at the row level should address the row instance by default (`Target = ev.Level`). This also fixes `Confirm("Delete row {{id}}?")`, which printed the cell id.
- Not a bug: an inner exact match (`y` on cell) runs before an outer longer sequence (`y y` on row) is considered. That is Vim's buffer-local-over-global rule and stays; bind the longer sequence at the inner level.

### 3. Small API additions

- `extends` on `Define` (table inheritance), today only in the editor spec loader. The CSV app copied motions into three tables for lack of it.
- `OpenModal` able to open a modal that contains an input (the sketch's prompt-noun editor became `AddInput` + `Push`).
- An exported way to remove an instance (the grid deletes from `r.Instances` directly).

Deferred and expected: `Parameter.source` for the transform pick list (after-phase-7 item 1); save-as through the picker needs the picker to accept a path that does not exist yet.

### 4. Follow-up: operation keys are `noun.verb` (b4a1ffe)

Keys were `verb.noun` (`undo.input`) while the CLI spelled the same operation `input undo` and the builder `Define("input").Verb("undo")`. Commit b4a1ffe flipped the key to `noun.verb` so all four agree, ownership reads as a path and a catalog sorted by key groups by noun. `cmd/renamekeys` did the rename (165 keys, 550 mentions) and is kept for the next one; `go run ./cmd/renamekeys -check -skip bindings_test.go` proves no old spelling remains. The old spelling still loads for one release: it is rewritten and reported by `ValidateBindings`, which now returns the deprecation list. Remove `modernRequest` and the `deprecations` field after that release.

## Principles borrowed from the DOM (owner decisions, 2026-10-24, from the forked design session)

The instance tree is a DOM. These apply its rules; each is additive and data-shaped.

1. **Capture phase.** The stack walk is bubbling (inner first). A row may set `capture: true` to be consulted outer-first before the walk, for the few app-level keys an inner component must not swallow (global quit, help toggle, the terminal pane's escape hatch). One flag, one extra pass.
2. **Tree queries as predicate values, no strings.** A query is an array of named predicates, ANDed: `instances.query([noun("worktree"), within("sidebar")])`, Go `r.Query(Noun("worktree"), Within("sidebar"))`, wire form `[["noun","worktree"],["within","sidebar"]]`. Framework predicates: `noun`, `id`, `kind`, `within`, `child-of`, `focusable`, `region`, `state`, plus `any`, `all`, `not`. Each is `func(*Instance, *Runtime) bool` registered by name, so an app adds `marked()` and the CLI gets it. The CLI may accept a convenience string and parse it to the array at the edge; nothing inside stores or reads a string form.
3. **Style cascade over the same predicates.** A style rule is predicates plus a style; specificity is the predicate count (`[noun("cell"), marked()]` beats `[noun("cell")]`). States and marks act as pseudo-classes (`focus`, `selected`, `disabled`). Replaces the single global `Appearance` and is the backbone of the color scheme designer in TODO.md.
4. **Component boundaries (shadow DOM).** A noun may declare `boundary: true`; queries, focus walks and style selectors stop at it unless the caller opts in. Focus already behaves this way (a grid is one stop); this extends the rule to queries and styles so a page-level selector never reaches cells by accident.
5. **Jump list (history).** Every `focus.set` and `page.open` pushes onto a history; `focus.back` / `focus.forward` walk it (Vim ctrl+o / ctrl+i). Makes `}` reversible across pages.
6a. **Action history (first piece of events out, do with the phase 8 follow-up).** `ExecuteAt` appends one entry per executed request to a bounded ring (about 500): `{seq, at, request, context: {focus, page, mode, source: "key:x" | "palette" | "cli" | "macro:a"}, result: {ok, value | error}, job?}`. Exposed as `history.list {last}` and in the snapshot at `/history`. Tests may assert on history instead of internals; the CLI `subscribe` streams the same entries later.
6. **Events out.** The runtime emits `focus`, `mode`, `page`, `modal`, `job` change events on one stream: the CLI's `subscribe`, logging, and test assertions by event rather than by snapshot. This is the only part of an "event bus" the framework owns; domain events stay in the application.

Not copied: box model / flex layout (Lip Gloss owns layout; a declarative layout tree is a separate project) and attribute-soup `dataset` (typed payloads are better).

Order: 1 and 2 with the phase 8 follow-up (tiny); 3 and 4 when the color scheme work starts; 5 and 6 when the znbu app needs them.
