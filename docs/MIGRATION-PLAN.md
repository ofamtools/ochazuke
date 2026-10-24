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

- Introduce `Block{Text string; Cursor *Pos}` as the render result of `InputView`, `Layout.Panel/Split/Sidebar`, `ComposeModal` and `Model.View`. Each composer offsets the child cursor. Only the focused component supplies one. Set `tea.View.Cursor` from it, shape by state: bar in insert, block in normal, underline in visual.
- Remove the `\x1b[7m` fake cursor from `InputView`; keep selection painting.
- Rebuild `ComposeModal` on `lipgloss.NewCanvas` with two `Layer`s (backdrop, modal). Drop `ansi.Cut` and the reset patching.
- Acceptance: cursor_view, surface and layout tests updated to the new shape; a 56x18 modal and a 48x15 layout render as before minus the fake cursor.

## Phase 3: nouns and instances under the existing layer API

No behavior change. This phase only adds the new model beneath the old one.

Write the new core as new files from the design, not as edits to runtime.go: `noun.go` (Noun, registration, verb and binding declaration), `instance.go` (Instance, parent links, focus, derived ancestry), `resolve.go` (the stack walk, used by phase 5). runtime.go only gains the wiring that builds nouns and instances from existing layers. The old plumbing is deleted in phase 5, not rewritten in place, so runtime.go shrinks to the registry and Execute.

- Add `Noun{Name, Verbs, Bindings map[state][]Binding, Actions, Grammar}` and `Instance{ID, Noun, Parent, State}`.
- Build them at registration: every existing `Layer` becomes a noun of the same name plus one instance of the same id. `AddInput`, `NewList`, `RegisterPage`, palette and picker registration set `Parent` from the path they already receive.
- `Runtime.Focus` is one instance id. `Context()` is derived: ancestors of focus, plus `InputID` when the focused instance is an input. Keep `Layer` as a read view over its noun for callers and tests.
- Acceptance: all phase 0 golden tests and the Execute-driven suite pass unchanged.

## Phase 4: items name nouns, targets are instances, one mode rule

- `ListItem.Layer` becomes `ListItem.Noun`. Rows are instances with `Parent` = the list; `SetItems` creates and drops them. Delete per-row layer registration from the README and examples.
- `Request.Target` resolves any instance id. Inputs, lists and (later) terminals share one addressing.
- `Runtime.Mode(c)` reads the focused instance's `State`. Delete `Frame.State["mode"]` and `Input.Mode` as separate truths (Input keeps a setter that writes the instance state).
- Per-instance binding overrides allowed above the noun's bindings.
- Acceptance: the full-project Items page works with mixed nouns in one list; `?` shows different verbs for different rows.

## Phase 5: one stack walk, everything is a noun

- Resolution: for each instance from focus outward, noun bindings for the current state, then any-state bindings, then the noun's grammar. First hit wins. Sequence pending state keyed by (focus, state). Unmatched sequence is offered to the innermost grammar before being dropped.
- Move the Readline switch and the Vim switch from editing.go into the `editor` noun (bindings for Readline, grammar for counts, operators, pending motions). `list.bindings` now reports them.
- `prompt` noun replaces `ReadlineOnly`: editor grammar, state pinned to insert, Escape unbound.
- Palette becomes a noun on a modal instance; "esc pops a modal" becomes a `modal` noun binding; macro recording becomes a transient noun pushed on focus.
- Multi-selection fan-out: a verb over marked rows runs `verb.<row noun>` per row, skipping and reporting unsupported nouns.
- Replace `dispatchKey` with the walk in `resolve.go`. Delete `InputPaths`, `Frame.State`, `validateContext`, `validateSelection`, `Layer` and the layer code in runtime.go, pages.go and configuration.go. Deletion, not editing: anything still needed is already in the new files.
- Acceptance: phase 0 golden tests pass against the new walk, with the one intended change (unmatched prefix handoff) updated deliberately.

## Phase 6: component delivery contract and operation kinds

- In a new `delivery.go`, generalize the picker's `HandleMessage`/`PendingCommand`/generation pattern into a contract every component implements: result carries owner id and generation; stale results rejected; closing an instance cancels outstanding work and releases held resources.
- Tag each `Operation` as `interaction` (runs on the loop) or `application` (dispatched to an application handler that performs I/O off the loop and reports through the contract). Palette, CLI and help show the kind.
- Acceptance: a fake slow application command cannot block key handling; a closed picker's late result is dropped.

## Phase 7: prove it

- Move `demo.go` out of the root package into `examples/demo`.
- Add a second page to `examples/full-project` without editing model.go or runtime.go. That is the architectural acceptance check.
- Add a live terminal instance (`terminal` noun, passthrough grammar, leader binding) fed by a fake stream in tests. The real daemon client comes with the application, not the framework.
- Acceptance: both checks pass; README and AGENTS.md describe nouns and instances instead of layers.
