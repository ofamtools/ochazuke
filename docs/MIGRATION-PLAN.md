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

## Phase 5: one stack walk over mode tables, everything is a noun

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

## Phase 6: component delivery contract and operation kinds

- In a new `delivery.go`, generalize the picker's `HandleMessage`/`PendingCommand`/generation pattern into a contract every component implements: result carries owner id and generation; stale results rejected; closing an instance cancels outstanding work and releases held resources.
- Tag each `Operation` as `interaction` (runs on the loop) or `application` (dispatched to an application handler that performs I/O off the loop and reports through the contract). Palette, CLI and help show the kind.
- Acceptance: a fake slow application command cannot block key handling; a closed picker's late result is dropped.

## Phase 7: prove it

- Move `demo.go` out of the root package into `examples/demo`.
- Add a second page to `examples/full-project` without editing model.go or runtime.go. That is the architectural acceptance check.
- Add a live terminal instance (`terminal` noun, passthrough grammar, leader binding) fed by a fake stream in tests. The real daemon client comes with the application, not the framework.
- Acceptance: both checks pass; README and AGENTS.md describe nouns and instances instead of layers.
