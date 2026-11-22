# Ochazuke v2

Ochazuke is a Go toolkit for modal terminal UIs. A pure kernel resolves keys
against a context chain, a view tree of plain data describes the screen, and a
thin Bubble Tea adapter connects the two. Everything above the kernel is
opt-in.

Module `ofam.dev/ochazuke`, Go 1.26.

## Tiers

The tiers are defined by imports, and `deps_test.go` enforces them on every
`go test ./...`. A package outside the policy table fails the test, so a new
package has to declare where it sits.

**Tier 0** holds the primitives. They use only the standard library, with
one exception. `buffer` also imports `layout` and `github.com/rivo/uniseg`
for grapheme clusters.

- `trie` matches key sequences, with exact, prefix and ambiguous results.
- `expr` parses and evaluates the `when` expressions of bindings.
- `layout` is the constraint solver with focus order and neighbors.
- `buffer` holds cells, ANSI and cursors.

**Tier 1** is the kernel and the renderer.

- `core` is the modal engine. It imports Tier 0 only, performs no I/O and
  starts no goroutines.
- `ui` is the ViewNode schema, the context chain, themes and the renderer.
  It imports `core`, `layout`, `buffer` and Lip Gloss, never Bubble Tea.
- `tea` (package `otea`) is the only library package that imports Bubble
  Tea. Examples import it too, to run programs and send keys in smoke.

**Tier 2** is opt-in. These packages import Tier 0 and Tier 1, never Bubble
Tea or `tea`, and nothing in Tier 0 or Tier 1 imports them.

- `config` loads JSONC bindings, partial themes, settings and static views
  from bytes. It reads no files.
- `widgets/table` filters, sorts, groups and marks rows.
- `widgets/picker` is a filterable list with a preview.
- `widgets/pathpicker` browses folders and files through an `FS` interface.
- `widgets/form` infers one control per `core.ParamSpec`.
- `widgets/input` is a modal vim-style text editor.
- `widgets/bindings` is a live keybinding editor with conflict analysis,
  contextual help and `FindFreeBinding`.

Examples under `examples/` may import anything.

## Examples

Each example has an interactive mode and a `--smoke` mode. Smoke drives the
real engine, shell and renderer without a terminal, prints one line per check
and exits 0 when every check holds.

```sh
HEAVY_PROCS=1 ~/bin/heavy nice -n 10 go run ./examples/minimal           # interactive
HEAVY_PROCS=1 ~/bin/heavy nice -n 10 go run ./examples/minimal --smoke   # headless checks
```

The same two forms work for every package below. `~/bin/heavy` with
`HEAVY_PROCS=1` runs one heavy job at a time and `nice -n 10` keeps the
machine responsive. Plain `go run` works the same where neither matters.

- `./examples/minimal` is the smallest whole app in under 90 lines. One
  `core.Engine` with three keys, a `ui.ViewNode` tree and `otea.Shell`.
- `./examples/views-json` is a Tier 1 UI made only of data. `views.json`,
  `bindings.json` and `themes.json` are embedded and loaded with
  `config.LoadView`, `config.LoadBindings` and `config.LoadTheme`, and
  `config.CheckBindings` validates the keys at startup. Go adds a status line
  and two commands. `d` cycles density and `t` cycles the theme presets and
  then the themes from `themes.json`.
- `./examples/cards` is a grid of different card kinds that share one keymap
  through scopes. It shows counts, a leader layer, a sticky resize layer,
  modals and a passthrough terminal card. `--smoke --dump` also prints the
  final frame.
- `./examples/widgets` puts every Tier 2 widget in five tabs, Table, Form,
  Paths, Vim input and Bindings. Each tab is a core page with its own focus
  and mode. The footer lists the keys of the focused context and `?` opens
  help built by `widgets/bindings` for that context. Smoke works in a
  temporary folder and never changes anything else on disk.

## Design rules

- A ViewNode is plain JSON data. The same schema serves a static
  `views.json`, a UI sent by a tool or agent, and native Go views. Domain
  records never enter the tree, a node carries at most a `core.Ref`.
- Keys resolve against the context chain, from the focused node out to
  `app`. Any ancestor with a scope or a ref adds a frame. A binding matches a
  scope and a mode, and may add a `when` expression over facts. Bubble
  bindings try the innermost scope first, capture bindings the outermost.
- Modes may name a base mode. A passthrough mode sends every key it does not
  bind to the focused widget as a `PassToWidgetEffect`, which is how an
  editor or a terminal takes text.
- Layers are transient key maps such as a leader. A layer can exclude
  commands by tag and can be sticky until cancelled.
- Sequences such as `g g` wait for the next key. When a shorter binding is a
  prefix of a longer one, the shorter one fires after the prefix timeout
  (one second by default) or when the next key breaks the sequence. Digits
  build a count.
- `core.Engine.Step` is pure and never mutates the state it is given.
  Handlers return the next state plus effects (async work, processes,
  timers, quit, custom effects) and the shell performs them. A newer async
  effect in the same slot cancels the older one.
- The shell renders right after input and coalesces background results to
  60 frames per second.
- The renderer caches a subtree by node ID, `CacheKey`, size, focus and
  theme. The theme part is its `Name`, `Version`, `Density` and
  `BorderStyle`. A host that edits roles or tones in place bumps `Version`.
  Switching density, border style or to a theme with another name needs no
  bump.
- Widgets are models with `HandleCommand` and `View`. A host routes the
  commands of a scope to its widget, and the widget renders plain ViewNodes.

## Density and borders

`ui.Theme.Density` sets how much room controls take. An empty or unknown
value renders as balanced.

- `ui.DensityCompact` gives each form field one row and cards no spacing.
- `ui.DensityBalanced` is the default. Form fields take three rows and card
  heads get a spacer.
- `ui.DensitySpacious` gives each form field four rows and pads cards.

`ui.Theme.BorderStyle` picks the frame of panels, cards and overlays. An
empty or unknown value renders as rounded.

- `ui.BorderRounded` is the default, with round corners.
- `ui.BorderSharp` uses square corners.
- `ui.BorderMinimal` draws top and bottom hairlines with open sides.
- `ui.BorderNone` draws no border glyphs, only titles and spacing.

Rounded and sharp frames turn heavy on focus and minimal gains a bar on the left. `ui.ThemePresets()` lists
the built-in themes, each with its own density and border style (dark, dark
compact, dark spacious, light and high-contrast dark). `config.LoadTheme`
reads `density` and `borderStyle` too and keeps the base values when they
are absent. The views-json and widgets examples cycle density live with `d`
and the presets with `t`.

## Path picker

`pathpicker.Model` lists folders first, then files. In `PickDir` and
`PickAny` mode a pinned `[Use this folder]` row heads the list and opening
it picks the folder on screen. `PickFile` has no pinned row. Opening a
directory drills into it and opening a file picks it when the mode allows
files. A query that starts with `/`, `~`, `./` or `../` is a typed path and
is resolved by `CmdOpen` or `CmdSelect`.

`Roots` lists named starting folders. `CmdRootCycle` (or `CycleRoot`) goes
to the next root, or the first when none is active, and `JumpRoot` goes to
one by index. Every pick is recorded in `Recents`, newest first.
`CmdMkdir` (or `CreateDir`) creates one child folder in the current folder
and highlights it, when the `FS` also implements `MkdirFS`. `OSFS` does.
All file access goes through the `FS` interface, so tests and hosts can
supply their own.

In the widgets example the Paths tab binds these to keys. `r` cycles roots
and a count jumps (`2r`), `~` starts a typed path, `enter` opens, `u` uses
the current folder from anywhere in the list, `+` creates a folder, `p`
toggles the preview and `.` toggles hidden files. The footer and `?` show
the rest.

## Tests

```sh
HEAVY_PROCS=1 ~/bin/heavy nice -n 10 go vet ./...
HEAVY_PROCS=1 ~/bin/heavy nice -n 10 go test -p 1 ./...
```

`go test` includes the dependency policy and a 100k-row performance harness.
