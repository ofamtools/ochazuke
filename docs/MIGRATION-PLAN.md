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

Status: done, commit 7c6bd0f. Decisions: `ListItem.Layer` was renamed to `ListItem.Noun` outright (all callers are in this repository), and a row's noun must be registered (a `Noun`, which in this phase is also a layer of the same name); a row instance is created by `SetItems` with `Parent` = the list and removed when the row disappears, a retained row keeps its instance and any `Instance.Bindings` override, and a row ID may not be a layer, an input or another list's row; `Context.Path` may now hold row IDs, so `validateContext`, `ExecuteAt`, `EffectiveBindings` and the picker's action loop resolve a non-layer element through its instance's noun (`layerFor`), which keeps the old loops and the new walk in agreement for the phase 3 equivalence tests (the test helpers that re-implemented the layer loop were taught the same rule); `Request.Target` and `Runtime.Focus` accept any instance, inputs still through their registration path; `select.context` prefers `instance` and keeps `path`; `Runtime.Mode(c)` reads the focused instance's `State` with ancestor inheritance (fixing the top-frame bug), `set.mode` on a frame writes the innermost path element's instance (so a mode set on a modal body whose path is only `app` is now visible beneath it, as the instance rule implies), and `Input.Mode` remains a mirror written by `Input.storeMode` so the editor compiles unchanged; the full-project Items page mixes `repo`, `worktree` and `branch` rows with per-noun verbs, keys, a `?` bindings modal and a `rows` palette source.

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

Already decided: per-instance overrides allowed and rare; verb names shared across nouns with the pair as the atom; the `list` noun owns only list verbs; multi-selection fans out `verb.<row noun>` per row.

## Phase 5: one stack walk over mode tables, everything is a noun

Status: done, commit d46c78e. Decisions:

- The stack is the focused instance's ancestry outward to `app`, with the `modal` noun's level inserted just inside `app` while a modal is open, and the `editor` noun appended as the fallback level of every stack. Framework defaults (Readline, Vim, counts, operators, registers, macros) therefore lose to any application row on the stack for a fresh key, which is what the phase 0 goldens require ("fresh key prefers layer binding", "app `i` beats editor `i`") and what the invariant "Readline rows are defaults, overridable per noun/instance/config" says; pending modes are exclusive so the grammar still wins mid-sequence. The `prompt` noun pins insert and binds nothing; `palette-field` is an ordinary inner noun whose own `printable`/`paste` rows beat app any-mode rows (the phase 0 finding), and palette-body rows are guarded by `self` so a field beneath keeps its own.
- Rows read at one level are merged: instance rows, then the noun's mode table, then its any-mode table (base modes only), with exact keys ranked above classes (`one-of` < `digit`/`alpha` < `printable`/`paste` < `any`) and guards (`when`) choosing among candidates in table order. Multi-key sequences are not pre-compiled into tables: the resolver matches sequence rows against the typed prefix, and the prefix mode `"<base> <keys so far>"` is a pending mode on the focused instance with `Payload{Prefix, Request: fallback}`; the timer (`BindingTimeoutMsg`, same generation guard) and a mismatching key leave it, run the fallback, and the mismatching key is dispatched fresh (the bindings suite's "mismatch consumes x" became "runs the match then x"). Escape cancels without running anything.
- Pending modes are one-shot: the answering key leaves the mode before its request runs unless the request is `enter.mode`, `append.count`, `leave.mode` or `cancel.input`; that is what lets a replayed macro dispatch its keys in the restored mode. `Payload` is `{Return, Count, OpCount, Op, Prefix, Request, Shadow}`; the register is not in the payload because `ActiveRegister` already holds it. `Input.Count()`/`Pending()` read the instance; `Input.Mode` mirrors only the editing mode (`Payload.Return` while pending). A pending mode entered on an inheriting leaf leaves back to `""`. Counts have two tables (`count_pending` extends `normal`, `visual_count_pending` extends `visual`) because the base rows differ; `operator_g_pending` and `g_pending` carry the operator and count in the payload; `count_pending`'s `any` row leaves and re-dispatches (`replay: true`), the other pending modes swallow an unknown key as before. The `space` key is not printable; it has an explicit insert row.
- Recording is the `macro_recording` shadow mode implemented as `Func` rows on the `app` noun's `macro_recording` table (`any`, `paste`, `q`), applied by `Dispatch` whenever `Macro.Recording != ""` and `Depth == 0`; replay keeps the depth flag. `MacroState.Pending/Count` and `prefix.macro` are gone. `leader_pending` is only a unit test of the same shape (`TestLeaderPendingShadowShape`).
- Owner additions relayed mid-phase, done here: pages are instances of kind `page` under `app` (`Instance.Component` holds the `*Page`; `Runtime.Page` is the current page; `PageList()` is app's page children in registration order via a per-instance sequence; `Pages`, `PageOrder`, `ActivePage` are gone) with the invariant "`Runtime.Page` is an ancestor of the base frame's focus" asserted in the demo and full-project session tests; a page that starts on a list starts on its selected row. The binding config is the row shape (`{noun|instance, mode, keys|pattern, request|null, from, when, replay}`) loaded by `load.bindings {path}` / `LoadBindings` / `Bind`, printed by `list.bindings`; `set.binding` is gone. The editor tables are `editor_bindings.json`, embedded and loaded at init with three generators (`expand`, `extends`, `pending`); rows for `input` operations get the `input` guard automatically so a page focus can still enter macro and replay modes. `editing.go` and `editor.go` keep only the handlers; no key-name switch remains.
- Deleted: `Layer`, `Runtime.Layers`, `InputPaths`, `Frame.Context`/`Frame.State` (a frame is `{ID, Focus}`), `validateContext`, `validateSelection`, `SelectContext`, `syncNouns`, `RegisterNoun`, `EffectiveBindings`, `EffectiveBindingsFor`, `ContextFromFocus`, `AmbiguousInstances`, `HandleInputKey`, `literalInput`, `Input.prefix`, `Input.ReadlineOnly/Count/Pending/operatorCount`, `Model.dispatchKey`, `Runtime.dispatchKey`, `prefix.input`, `prefix.macro`, `set.binding`. Moved: scroll offsets to `Model.scroll`, the yank-history selection to `Runtime.yankSelection` (the modal renders its own window), the full-project example's page state to its own map. Carry-overs to phase 6 as the owner listed: `Runtime.Inputs` and `Runtime.Lists` as instance payloads; registers, yank history, active register and macro state into one component.
- Registration API: `r.Define(name)` builder (`Verbs`, `Verb`, `Action`, `Pin`, `Mode(m).Bind/Pattern/Unbind/When`), `r.Instance(id, Parent(..), Kind(..))`, `r.AddInput(in, Parent(..), Kind(..))`, `NewList(r, id, items, Parent(..))`, `RegisterPage(&Page{ID, Title, Focus, Render})`, `Push(&Frame{ID, Focus})`. Row constructors `Do`, `Enter`, `Leave`, `Func`, with `.FromKey`, `.FromPayload`, `.Guard`. Guards are per runtime (`DefineGuard`). `select.context` keeps `instance` and `path`. Multi-selection: `mark.list` (Space on the `list` noun) and `apply.list {verb}` fan `verb.<row noun>` out per marked row with a skipped report.
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
normal:           { i: set.mode insert, d: enter operator_pending{op:delete}, g: enter g_pending, <digit>: count.append, q: enter macro_pending }
operator_pending: { w: motion.apply word, d: line.apply, any: enter normal }
g_pending:        { g: move first-line, any: enter normal }          // "g g" is sugar for this; the ambiguity timeout is a timer cmd that fires set.mode with the fallback, same generation guard as today
macro_pending:    { <alpha>: macro.start{register:key}, any: enter normal }
macro_recording:  { any: macro.append(key) then re-dispatch in payload.shadow and store the next mode back in shadow,
                    q: stop only when shadow is normal with empty payload }   // replay keeps the depth flag so replayed keys are not re-recorded
leader_pending:   { <bound>: run command, any: forward prefix+key to the pane, restore shadow }   // terminal leader, same shape
```

Binding row shape, identical in code and config: `{noun, mode, keys | pattern, request | null, argsFromKey?}`. Same noun+mode+pattern replaces a default, a new pattern adds, `null` unbinds. `list.bindings` prints exactly this for the current stack and mode. Go sketch: `r.Noun("editor").Mode("normal").Bind("d", Enter("operator_pending", Payload{Op: "delete"})).Bind(Digit, Do("count.append"))`; `.When(ShadowIs("normal"))` for guarded rows; `Func(...)` only where re-dispatch is needed (recording, leader).

Work:

- Implement the resolver above in `resolve.go` (phase 3 left `EffectiveBindingsFor`/`ActionFor` there; extend the row shape with patterns and `null`, and the mode with a typed payload). Replace `dispatchKey` with it.
- Move the Readline switch and the Vim switch from editing.go into the `editor` noun's mode tables, generated from a compact spec: Readline rows in `insert`, Vim rows in `normal` and the visual modes, with `operator_pending`, `g_pending`, count and register handling as pending modes with payload. `list.bindings` and help now report all of it, pending modes included.
- `prompt` noun replaces `ReadlineOnly`: editor tables, mode pinned to insert, Escape unbound so a page or modal can bind it.
- Palette becomes a noun on a modal instance; "esc pops a modal" becomes a row in the `modal` noun; macro recording becomes the `macro_recording` shadow mode (replay keeps the depth flag). The terminal leader, when phase 7 adds it, is `leader_pending` with the same shape.
- Multi-selection fan-out: a verb over marked rows runs `verb.<row noun>` per row, skipping and reporting unsupported nouns.
- Delete `InputPaths`, `Frame.State`, `validateContext`, `validateSelection`, `Layer` and the layer code in runtime.go, pages.go and configuration.go. Deletion, not editing: anything still needed is already in the new files.

Consequences and care points: the phase 0 goldens become table rows (mode, key, next mode, action); users can rebind inside pending modes; payload is a struct, not a map; help in a shadow mode shows the shadow's bindings plus the mode's own rows (a presentation rule, not a resolver rule); the phase 0 finding that the macro grammar is consulted twice per key disappears by construction.

Acceptance: phase 0 golden tests pass against the new walk, with the one intended change (unmatched prefix handed to the innermost mode's `any` row instead of dropped) updated deliberately; `list.bindings` lists every key the editor answers to.

## Runtime end state (owner input relayed from the forked design session, 2026-10-24)

After phase 6, `Runtime` is: operation registry + instance registry (nouns, instances, parent links) + focus + `Page` + modal chain + pending-sequence state. No component-specific fields.

1. `Runtime.Inputs` and `Runtime.Lists` go away: inputs and lists are instances with typed payloads looked up through the instance registry. Phase 6.
2. `Registers`, `YankHistory`, `ActiveRegister` and `Macro` move off `Runtime` into one `registers` component under the phase 6 delivery contract, reached through operations, not fields. Phase 6.
3. Pages are instances of kind `page` under `app` with an explicit `Runtime.Page`; done in phase 5.
4. Binding config is the row file loaded by `load.bindings` and printed by `list.bindings`; done in phase 5.

## Phase 6: component delivery contract and operation kinds

Status: done. Decisions:

- Delivery contract (`delivery.go`): `Component{HandleMessage(tea.Msg) bool; PendingCommand() tea.Cmd; Close()}` on `Instance.Component`; results are `Completion{Ticket{Owner, Generation}, Value, Err}`. One generation per instance (`Instance.generation`, unexported): `Runtime.Start(owner, work, then, ctx)` cancels the owner's earlier work, bumps the generation and queues a `tea.Cmd`; `Deliver` hands a current completion to the owner's `then` callback and `HandleMessage`, and drops a stale or orphaned one; `closeInstance` (called by `Pop` for modal instances, `SetItems` for dropped rows, `removeInstance`) cancels work, retires the generation and calls `Close`. `Model` has no `Pickers` field: `Update` calls `Runtime.Deliver` first and `Runtime.PendingCommands` last, and the runtime walks the instance registry for components. The picker keeps its three per-stream generations internally and only gained `Close`; the palette is not a component (no off-loop work).
- Operation kinds: `Operation.Kind` is `interaction` (default) or `application`; `Operation.App` is an `AppHandler func(Ctx) Result` with `Ctx{Runtime, Context, Args, Instance}` and `Done(v)`, `Failed(err)`, `Pending(work func(context.Context) (any, error)).Then(fn)`. `Define(noun).Verb(name, h)` takes a `Handler` or an `AppHandler` (parameter type `any`, checked at registration, so existing function literals compile); an application verb cannot override an existing operation. `Pending` returns the `Ticket` to the caller (CLI and palette see it as the result) and the completion lands through the contract. `list.operations` and docgen carry `kind`; the palette labels application verbs `verb · application` and confirmed verbs `· confirm`, and its argument header shows the kind; the full-project `?` modal appends the kind per row.
- Modal payload and confirm: `OpenModal(kind, Payload)` creates an instance `{ID: kind, Noun: kind, Parent: focus, Payload}` registered as a modal instance and pushes `Frame{kind, kind}`; the stack walk already puts it first. `Payload` gained `Prompt`; `Request` gained `Confirmed` (on the wire, so scripts can skip prompts). `Operation.Confirm` holds the template; `ExecuteAt` opens the confirm modal instead of running when it is set and the request is not confirmed, storing `{request, Confirmed: true, Target: focus}` so `y` (`accept.confirm`: pop, then `Execute`) runs it at the origin even though the confirm body is focused. The `confirm` noun's any-mode table: `y`/`enter` accept, `n`/`esc` pop, `any` ignores. `.Confirm` applies to the last `Verb` on the builder; `set.confirm {operation, prompt}` adds or removes one at run time; the full-project `remove.worktree` confirm comes from a `confirm` field in `actions.json` and the smoke presses `d n` then `d y`.
- Runtime end state: `Runtime.Inputs`, `Lists`, `Registers`, `YankHistory`, `ActiveRegister`, `Macro` and `yankSelection` are gone. `Instance.Component` holds the `*Input` or `*List`; `r.Input(id)`, `r.List(id)`, `r.Inputs()`, `r.Lists()` read the registry. `Registers{Named, History, Active, Macro, selection}` is the component of the `__registers` instance under `app`, reached through `r.Registers()` and the existing operations; it implements the contract (Close abandons a recording). The remaining fields are `Nouns, Instances, Frames, Page, Modes, Operations, Quit, Pending, BindingTimeout, pendingModes, guards, prefix, bindingVersion, modeGeneration, rows, modalInstances, seq, commands`, asserted by `TestRuntimeHoldsNoComponentFields` (the "transport hook" of the end-state note is `commands`, the queue the adapter drains). Snapshot keeps `/inputs`, `/lists`, `/registers`, `/yankHistory`, `/activeRegister`, `/macro` and adds `/components`.
- Editor fallback (review item relayed mid-phase): `stack()` appends the `editor` level only when an input (`r.Input(id) != nil`: editor, prompt or palette field, which is why the test is on the component and not the noun name) is on the ancestry; otherwise the walk ends at `app`. So that a page or row focus still counts, selects registers, records and replays, the `q`, `@`, `"`, `digit-0` and `count_pending` rows moved from the editor spec to `registers_bindings.json`, loaded onto the `app` noun after the editor spec (with the same `extends` so `count_pending` carries them); the app `count_pending` `any` row is guarded by the new `no-input` guard so an input's own `count_pending` rows (motions, `0`) still win. The editor keeps `visual_count_pending` and its own `count_pending` `esc`/`any`. `TestEditorFallbackNeedsAnInputOnThePath` covers: no editor rows listed for a row, `x u p i d` unhandled there, `2 q` still enters `macro_pending`. All eleven goldens and the phase 5 resolver tests pass unchanged.
- Not covered by the design and left as is: more than one outstanding generation per instance (a new `Start` supersedes), progress reporting from pending work (only completion), and cancelling application work from the keyboard (closing the instance is the only cancel).

- In a new `delivery.go`, generalize the picker's `HandleMessage`/`PendingCommand`/generation pattern into a contract every component implements: result carries owner id and generation; stale results rejected; closing an instance cancels outstanding work and releases held resources.
- Tag each `Operation` as `interaction` (runs on the loop) or `application` (dispatched to an application handler that performs I/O off the loop and reports through the contract). Palette, CLI and help show the kind.
- Acceptance: a fake slow application command cannot block key handling; a closed picker's late result is dropped.

## Phase 7: prove it

Owner input (relayed from the forked design session): unify the chrome. One `Layout.Page` used by the shell instead of `Model.View` and `Layout` each reserving rows with their own padding; footer and modal hint lines derived from the resolver's effective bindings for the current stack and mode, as picker_view.go already does, so hints are the same data `list.bindings` prints; delete the hard-coded hint strings in model.go and the duplicate height constants.


- Move `demo.go` out of the root package into `examples/demo`.
- Add a second page to `examples/full-project` without editing model.go or runtime.go. That is the architectural acceptance check.
- Add a live terminal instance (`terminal` noun, passthrough grammar, leader binding) fed by a fake stream in tests. The real daemon client comes with the application, not the framework.
- Acceptance: both checks pass; README and AGENTS.md describe nouns and instances instead of layers.

## After phase 7

Owner-confirmed items, relayed from the forked design session:

1. `Parameter` gains `enum` (static choices) and `source` (a verb that returns choices, such as `list.projects`). Palette argument fields, forms and CLI completion render a pick list from them. Palette modes are unaffected.
2. docgen is retargeted, not dropped: generate a reference manual from the `Define` data (per noun: verbs, params with choices, confirm, interaction or application kind; per noun per mode: key tables; app page children). Hand-written docs stay (architecture guide, input modes, this plan). Delete only stale output: visual-check PNGs, pre-migration architecture-map json and html, layer-era bindings.html and actions.html that docgen regenerates.
