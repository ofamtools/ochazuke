
# TODO

## upcoming

- [ ] Implement the confirmed [component and input requirements](docs/component-plan.md).
  - [x] Add a Readline-only flag to the shared input; preserve path-aware `Ctrl-W` and `Ctrl-Y`.
  - [x] Integrate the preferred split picker from `picker-lab`, with an editable absolute path, configurable bindings, accurate help, and no function keys.
  - [x] Connect component focus and item selection to the existing layer path; replace the innermost item layer when focus changes.
  - [ ] Implement the application terminal component's [live-input contract](docs/component-plan.md#confirmed-live-terminal-input-contract): configurable `Ctrl+A` prefix, prefix then `d` detaches without terminating the session, doubled prefix sends one literal prefix, and ordinary input bypasses outer bindings.

- [ ] Resolve the proposed APIs in the [component design record](docs/component-plan.md) before implementation.
  - [x] Define and implement the flat selectable list contract; tree expansion remains optional and unspecified.
  - [ ] Design optional inline completion triggers and `search(query)` providers; settle token boundaries, replacement behavior, and async handling.
  - [ ] Finalize optional `Runtime.Model` access and zero-or-one attached model ownership.
  - [ ] Settle remaining terminal details: component/provider API, screen updates, resizing, and connection lifecycle.
  - [ ] Continue the one-at-a-time discussion: domain detail views, specialized previews, and optional resizable panes.
  - Domain modules, persistence, and terminal-daemon integration remain application-level.

- [ ] Build tools that should be shipped or available thru the repo and whose
      code should be in the repo.
  - binding checker - will go into details in a diff file
    - checks for modes that don't have a binding to leave the mode, or that can
      get back to normal mode
    - where layers overlap other keys
  - binding sandbox/tester - could maybe be rolled into a full-featured binding
    utility
  - binding/config designer - load a list of actions, and maybe layers (idk) and
    just go to town customizing it
  - color scheme designer/previewer - preloaded with tuios's themes (grouped by
    light and dark)

- [ ] Add configurable color schemes shared by pages, panels, inputs, and modals.
  - Build on `Appearance` / `Styles`: named palettes, semantic colors, live preview, and config persistence.
  - Check contrast in light/dark terminals, ANSI-16 terminals, and `NO_COLOR`; keep focus visible without color.
  - Use the keybinding sandbox as the preview surface for every layout and modal state.

- [ ] define actions via config to call a cli's json/api version of an action
  - so "command" or "subcommand" or "format" whatever is needed to say when
    noun/verb is triggered it should run cli command:
    `base noun verb --json JSON` or something standard and easy for all clis to
    support

## done

- [x] Show contextual CLI help for every runtime noun and verb before daemon preparation.
  - Oden owns help rendering and command resolution/dispatch metadata; `cli.Mount` only maps the runtime registry to groups, operations, and parameter schemas.
  - Consumer regressions in `cli/cli_test.go` cover framework nouns, contextual help without `Shell.Prepare`, and real operation delegation.
  - Shared implementation is in the actual `../oden` repository (`oden.go`, `help.go`, `catalog.go`, `builtins.go`); adapters contain no help renderer.
  - Migrated the manual mounts in `o-ai`, `o-qbt`, and `o-inventory`, preserving output wrappers and shortcuts. Verified actual-source builds of these consumers and mtm; the active matomeru checkout was not edited.

- [x] Add reusable Lip Gloss layouts and apply them to the keybinding sandbox.
  - `Layout.Page`: title, subtitle, bounded body, and persistent footer.
  - `Layout.Panel`: titled, focused panels with visible overflow; `Layout.Split`: side-by-side panes that stack in narrow windows.
  - `Layout.Sidebar`: 28-column navigation rail, selected-row highlighting, automatic scrolling with a position hint, and narrow-window stacking. Sandbox setup uses it; callers own selection and keyboard handling.
  - `ModalSize` / `ComposeModal`: bounded overlays with a visible backdrop perimeter; terminals below 3×3 keep the backdrop instead.
  - Sandbox setup, live key feedback, editors, confirmations, and file browsing use these surfaces.
  - Verified with `go test ./...`, a CLI build, and real-terminal interactions at 80×24 and 60×18.

- [x] Cursor should not move and reposition text after cursor.
    - `InputView` now inverts the existing grapheme instead of inserting `▏`.
    - Empty/end positions use a styled blank cell; wide/combining characters,
      clipping, and visual selections are covered by `cursor_view_test.go`.
    - Verified with the full suite and an actual terminal: moving across
      `abcdef` changes the highlighted cell without shifting the text.
    - How Bubble Tea/Bubbles does it: Bubble Tea hides the native terminal
      cursor; Bubbles text inputs give the character under the cursor to
      `Cursor.SetChar`, render that character through `Cursor.View`, and then
      render the suffix starting at the next character. `Cursor.View` uses
      Lip Gloss `Reverse(true)` for its visible cursor phase. At the end it
      uses a space. Sources: [Bubbles cursor](https://github.com/charmbracelet/bubbles/blob/v0.21.0/cursor/cursor.go#L208-L218),
      [Bubbles textinput](https://github.com/charmbracelet/bubbles/blob/v0.21.0/textinput/textinput.go#L659-L683),
      [Bubble Tea terminal initialization](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/tty.go#L35).

