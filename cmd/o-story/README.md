# o-story

A storybook-style sandbox for ochazuke. A **story** is a scenario as data: a
synthetic page, a script of events, and what must hold afterwards. The same
file opens by hand in the real chrome (header tabs, footer hints, ticker,
notices, `?` help) and runs headless as a test.

Stories never read production state: every row, label, pane line and job is
in the file. Labels may look real (`"labels": ["tuios-deck", ...]`); they are
still data.

```sh
go install ofam.dev/oden/cmd/o@latest   # the family's `o` command: `o X ...` runs o-X
o story                      # index of stories by area; enter opens one
o story open NAME            # one story live in the real chrome; q returns to the index
o story run [NAME|AREA...]   # headless; non-zero exit on failure
o story run --update --dir cmd/o-story/stories   # rewrite goldens on disk
o story list | history | keep N NAME | replay REF | trim REF ... | verbs
o story explore [NAME|AREA...] [--depth N] [--steps N] [--out DIR]   # the state explorer
o story explore NAME --watch [--delay D]   # watch the explorer run in a TUI; same report
o story explore --dedupe DIR [--reproduce] [--out DIR]   # group the findings of saved reports
```

`go test ./...` inside `cmd/o-story` (or `make test` at the root) runs the
composites through monogatari's headless driver.

## Area composites

The committed suite is twelve walkthroughs in `stories/composites/`.
Each has one sequential script and step-numbered assertions, including the
distinctive checks from the value report's KEEP sources. Source names remain
in event notes so a failure's event number identifies its original regression.

| Composite | Walkthrough |
|---|---|
| `lists` | Lists and tables: filters, saved views, sort, grouping, folds, windowed paging, marks, four edges |
| `forms` | Record and args forms: validation, failed writes, undo, choices, completion, fill-in, scopes |
| `palette` | Classic and board commands: prefix matching, aliases, parameters, results, archive-first deletion |
| `help` | Help trees, table/pick views, binding conflicts, docs reinvocation and steady scrolling |
| `pickers` | Noun and path choices: list, hybrid, columns, sources, preview, creation, navigation |
| `nouns` | Generated indexes and forms, empty indexes, creation, edit, archive/restore, related sections, history, undo |
| `tree` | Outline editing, failed writes, wrapping, folds, moving, visual undo groups, zoom |
| `focus` | Custom and pending modes, focus restoration, modal boundaries, registers, panes |
| `settings` | Settings tabs, sections, bad values, lists, session writes, undo, themes and About |
| `terminal` | Terminal leaders, overlays, jobs, late answers, timeline yanks and navigation, diffs |
| `cli` | Command shell, catalog mounts, loading/failure, structured answers, widgets, foreground exit |
| `text` | Markdown and text viewers: editing, wrapping, links, notes, search, yanks and motions |

The demo's optional `checkpoints` field is a list of starting fixtures, not
another list of stories: it contains no scripts or expectations. `seed: -1`
starts before the first checkpoint. `ctrl+alt+n` runs `walkthrough.next`,
loading the next fixture through monogatari's existing reset. This isolates
incompatible starting configurations and intentional failures, cancels old
jobs, and gives each checkpoint fresh temporary directories. Scripts continue
and assertion offsets never restart. The final reset returns to the opening
fixture, so the same loaded walkthrough can be replayed.

The hidden capture row belongs to the app instance, not the fixture's initial
pages, so it also works after a verb creates a noun index or opens Settings.
Its actual-key guard keeps it out of live hint/binding scans. Hypothetical
help deliberately lists guarded rows, including this one; assert the picked
noun/mode and its operations, not a fixed wrap or number of visible rows.

The app maintains `checkpoint-start` metadata so recordings and player rewinds
start from the original fixture, even when saved halfway through a walk.
Snapshots retain their current checkpoint index and can continue into the next.
Keep true restart/persistence scenarios in ordinary tests rather than giving
an isolated checkpoint a different storage lifetime. `restart_test.go` retains
the table pins, column widths, fit-width and pane-divider regressions.

There are no full-screen goldens in the committed composites. Assert focus,
mode, modal, writes, registers, visibility, styles and short content regions.
Do not pin inventory counts, a generated verbs table, or whole help tables.
Binding validity and typing shadows are checked during the same replay.

`make stories` runs only `TestStories`, plain and uncached. It does not run
vet, unrelated package tests, the explorer, or a README generator. The full
`cmd/o-story` suite still checks the tool's other behavior.


o-story is the demo app's host on
[monogatari](https://github.com/ofamtools/monogatari), the story library
(the format, the runner and driver, recordings, trimming, explore). The
demo app (`demo/`, `demo.App()`) is registered like any app's: its one
fixture, `pages`, builds a story's `page` and `pages` fields on a fresh
runtime, and as o-story's first app it is the default for a story that
names no `app`. `cmd/o-story` is a nested module requiring ochazuke and
monogatari; it builds through the untracked `go.work` of the ochazuke
checkout. Everything but the demo app is monogatari's host
(`monogatari/host`: the commands, the catalog, the index TUI, the player and
explore's watch mode); `main.go` is `host.Main` over
`host.Config{Name: "o-story", Apps: []monogatari.App{demo.App()}, Stories: ...}`.
The name `o-story` keeps the user stories and recordings below at
`o-story`, whichever command runs it (`o story`, `mtm story`'s
pass-through); another host (`mtm story`) keeps its own.

The Turns fixture is part of `composites/terminal`: a pane row, the card
timeline, prompt box, settings line and key readout. Its regression walks
remain in that composite's checkpoints rather than separate base stories.

## The model

State changes only through actions. A recording is the initial story plus
the ordered events. A frame is a pure function of those two. Trimming a bug
down is choosing a window of actions around the failure.

Events are keys and also the things that are not keys but change state: a
job delivering, a framework timer firing. The headless runner never lets a
message reach the model except through an event, in order, so a run does
not depend on wall-clock time. The model runs on the runner's fake clock
(`Model.Now`), which stands still between events; a `linger` tick advances
it to the timer's deadline and delivers the timer's message
(`Model.JobLingerTick`) at once, so no story waits the 4s linger.

## A story file

`stories/<area>/<name>.json` (embedded into the binary) or
`$XDG_CONFIG_HOME/o-story/stories/<area>/<name>.json` (yours, and where
`trim` writes):

```js
{ name: "spill: j at the last row", area: "motions",
  width: 80, height: 24,          // the story's area; default 80x24
  chrome: "full",                 // "full" | "none" (page body only) | "ticker-only"
  skip: "gap: ...",               // optional: a known framework gap; runs, cannot fail
  theme: "kombu", accent: "#E8833A", // a theme other than mono, the Appearance accent override
  themes: { kombu: { background: "dark", accent: "#C9A227" } }, // schemes in the themes.json shape;
                                  // one that does not validate is a notice (chrome/scheme-*)
  values: { scrolloff: 4 },       // setting values set at the start (Settings.Set, the saved layer)
  session: { stack: "stack" },    // session values above them (StartSession, SetSession), on the
                                  // driver's clock, so their time reads 00:00 (settings/session-*)
  config: { "actions.json": [{ alias: "demo.zap", action: "demo.ping" }],
            "bindings.json": [{ noun: "demo", mode: "normal", keys: "Z", request: { noun: "demo", verb: "zap" } }] },
                                  // user config files by name, written to a temp config directory of
                                  // its own and loaded after the pages (actions, then bindings), as
                                  // Model.Init does; a problem a load reports is a notice (actions/*)
  files: ["a.json", "sub/c.json", "sub/deep/"], // a temp file tree of its own (a trailing / is a
                                  // folder); {{files}} in a record field's base or value names it,
                                  // writes show it as {{files}} (forms/path-*)
  // A page's picker: "{{files}}/smg" opens the framework path picker from
  // a focused input. Its initial isolated scan settles before the first
  // frame; later raw picker job messages are not scripted job events.
  state: true,                    // a temp state directory of its own (removed at the end), kept
                                  // across { restart: true } events: what the app remembers (panes.json)
  page: {                         // or pages: [{ id, title, ... }, ...]
    lists: [{ id: "a", rows: 2 },                  // rows a1, a2
            { id: "b", rows: 3, noSpill: false, noMarks: false, clearFilterOnEnter: false,
              cellSelect: false },                 // cellSelect: a table with a current column (CellSelect)
            { id: "c", rows: 10, height: -1,       // 0 every row, -1 rest of the page
              labels: ["first project", "tuios-deck"], selected: 1, marked: [0] },
            { id: "d", columns: [{ title: "pane" }, { title: "state" }],
              items: [{ id: "d1", label: "w", cells: ["w", "working"],
                        tones: [{ cell: 1, tone: "state.working" }] }] }], // tones: ToneSpan (tables/tones-*)
                                  // an item may name its noun ({ id: "work", label: "Work", noun: "project" }):
                                  // a thing a store's reference prop names (palette/create-from-focus)
    order: ["a", "b", "c"],       // drawing order (default: as listed)
    layout: "stack",              // or "row": lists side by side; "split": the first two lists in Layout.SplitFocusBlock
    focus: "b1", offset: 0, noHeadings: false,
    inputs: [{ id: "name", label: "name", text: "hello", secret: false, // secret: Input.Secret (chrome/keys-echo-secret)
               completions: [{ trigger: "@", items: ["src/main.go"], fail: "" }] }],
    trees: [{ id: "t", editable: true, failWrites: 0, collapsed: ["p1"],
              nodes: [{ id: "p1", label: "one" }, { id: "c1", parent: "p1", label: "child" }],
              columns: [] }],      // columns make the tree a table; nodes bring "cells"
    terminal: { fake: true, attach: "a1", height: 6,
                hints: { key: "f", dim: true, marks: [{ row: 0, col: 0, text: "a" }] } },
                                     // hints: ctrl+a f sets the overlay and enters the hints mode;
                                     // a letter / capital / ctrl+letter logs "hint copy|type|open L"
                                     // (writes) and returns to terminal, esc cancels, nothing reaches the pane
    jobs: [{ verb: "demo.build", key: "w", finishOnKey: "W", failOnKey: "X", quiet: false, start: 0, label: "" }],
    bind: [{ noun: "demo", mode: "normal", keys: "C", do: "notice.clear" },
           { noun: "app", keys: "X", do: "demo.hide", args: { id: "b" } }],
                                  // demo.hide {id} stops drawing a list, tree, input or the
                                  // timeline, or draws it again (a tab switched away: focus/*)
    modes: { loop: { pending: true } }, // custom modes, including pending ones,
                                  // declared before bind (mode.enter { mode: "loop" })
    verbs: [{ verb: "demo.open-file", description: "Open a file", positional: ["path"],
              parameters: { path: { type: "string", required: true, source: "demo.files" } } },
            { verb: "demo.files", rows: ["main.go", "go.mod"] },
            { verb: "demo.add-base", aliases: [{ alias: "demo.base.add", since: "v0.27.0", removeAfter: "v0.28.0" }] }],
                                     // demo verbs with parameters (palette stories); a run logs
                                     // "demo.open-file path=main.go" (writes) and returns rows;
                                     // aliases are ActionAlias rows (action is the verb), and a
                                     // run through one logs " via=demo.base.add" after the arguments
    nouns: [{ noun: "json.doc", verbs: ["rows"],
              aliases: [{ alias: "doc" }, { alias: "json-doc", since: "v0.57.0" }] }],
                                     // nouns besides demo, as a host mounts an extension's (json.doc);
                                     // a run logs "json.doc.rows" (writes); aliases are NounAlias rows
                                     // (Runtime.AddNounAlias; since/removeAfter: deprecated) (palette/noun-*)
    store: { props: [{ name: "id", key: true }, { name: "title", role: "label", fallback: "summary" }],
             things: [{ id: "a1", title: "", summary: "first line" }] },
                                     // the demo noun's props and an in-memory store (PropSpec rows):
                                     // demo.get, list, create, set, delete, archive and edit are
                                     // then standard verbs (set and edit address the label, owner
                                     // and archive props by role: @label, @owner, @archive); each call
                                     // logs "list", "get KEY", "set KEY p=v", "create KEY p=v" or
                                     // "delete KEY" (writes); rows stand for things by id;
                                     // noun: "chore" puts it on another noun; generate: { count: 20000,
                                     // thing: { id: "c{{n}}" } } adds many things; paged: true gives it a
                                     // Query (each call logs "query page=P per=N"); token: true a
                                     // ChangeToken that moves on every write
    detail: { lines: 50, width: 0 }, // a scrollable detail (AddDetail) on the right:
                                     // "detail 1–11 / 50", then "<row> line N" about the
                                     // focused list row (width 0 = half the page)
    markdown: { id: "markdown", text: "- [ ] milk\n", external: "…", height: 0 },
                                     // a markdown.Doc drawn in the page's order (its id in order);
                                     // toggles log "toggle LINE [x] TEXT" (writes); external is
                                     // what the source holds from the first toggle on
    timeline: { id: "timeline", followTail: false, headerLines: 0, noSpill: false, height: 0,
                parentStop: "",      // where h on a closed card goes (a list id: its selected row)
                cards: [{ id: "t1", header: "prompt", meta: "09:12 · done", body: "answer", open: false,
                          active: false, muted: false, group: "session 1", event: false,
                          sections: [{ title: "thinking", summary: "3 lines", text: "…", role: "muted", folded: true }] }],
                append: [{ id: "t2", header: "next prompt" }] },
                                     // an AddTimeline of story-card cards in the page's order;
                                     // a appends the next queued card, s streams a line into the last;
                                     // event draws a card as an event (Card.Event)
    texts: [{ id: "settings", text: "turns.style cards" }],  // fixed lines, in the order by id
                                     // (inputs may be in the order too), else after the inputs
    readout: false,                  // the key readout (bottom-right, or "readout" in the order):
                                     // the mode, the focus path innermost first, the last key a
                                     // row answered → its request, the mode, and its target if any
    commands: {                      // instead of the above: cli's Commands page
      entries: [{ name: "greet", kind: "inline", summary: "say hello", source: "main.go:12", help: "usage: …" },
                { name: "web", kind: "external", source: "/bin/hello-web" }],
      help: { web: "usage: hello web [PORT]" },  // what command help fetches (a job)
    },                               // rows command:<name>; a run opens a fake pane, its terminal page1/run/N
    catalog: {                       // instead of the above: a catalog data page (cli.MountPage)
      nouns: [{ name: "skill", columns: [{ title: "Name" }], verbs: [{ name: "list", rows: "skill" }] }],
      state: "",                     // the catalog's state source, NOUN.VERB
      page: { layout: "split", lists: [{ id: "ai/skills", noun: "skill" }], detail: { fields: ["path"] },
              widgets: [{ id: "sync", kind: "status", source: "sync.status" }] },  // kv | status | table
      answers: { "skill.list": [{ id: "s1", name: "architect", path: "~/x" }] },  // stdout per NOUN.VERB (a string is the text: JSON Lines)
      pending: false,                // true leaves the loads to the script: { job: "catalog-load", state: "done" }
      files: { "/p/one.md": "# One" },  // a markdown detail's documents (detail: { kind: "markdown", pathField: "path" });
      author: "owner", today: "2026-10-27",  // its writes log as "PATH write LINE -OLD +NEW"; notes signed and dated
    },                               // rows <list>/<id>; the page id is the story page's
    chooser: {                       // a chooser (OpenChooser) open over the focus at the start
      kind: "dirs", title: "Directory", keep: [1],  // keep: the cell columns cut last
      rows: [{ id: "m", label: "matomeru", cells: ["matomeru", "~/proj/matomeru", "main"],
               preview: "text" }],    // any row with a preview gives the chooser a Preview
      sources: [{ name: "one", rows: [...] }, { name: "two", live: true, rows: [...] }],
                                     // a chooser with sources (ctrl+o and alt+o cycle them; rows unused); a live
                                     // source answers "typed: QUERY" then the rows holding the query;
                                     // a pick writes "pick ID"; demo.choose opens the chooser again
    },                               // enter picks and closes
    defaultHidden: true,             // the page is hidden by default (pages.hidden); a hidden first page gives way to the first in the tabs
    nounsPage: true,                 // register the Nouns page (EnsureNounsPage) after the pages and About, as Model.Init does
  },
  script: ["j", "j", { type: "ping" }, { paste: "text" },
           { job: "demo.build", state: "done" },
           { tick: "linger" },    // tick: linger | binding | terminal | output | debounce
           { restart: true }],    // quit and build the story again (same state directory)
  expect: { focus: "b1", visible: true },
  // or per step: [{ after: 2, focus: "b1" }, { ticker: [] }]  (after N = after the Nth event)
}
```

Expect fields: `focus`, `mode`, `modal` (top modal id, `""` none), `page`,
`shift` (the current page's pane divider, `Runtime.PaneShift`),
`badge` (the mode badge on the title row, whole: `"NORMAL G"` fails on a `NORMAL` frame; full chrome only; R2.10),
`visible` (the focused label is on screen), `highlighted` (rows with
full-strength reverse video), `dimmed` (rows whose only reverse video is
under faint: an unfocused page list's selected row, a highlight beneath a
modal; read in the mono theme), `highlightedText` (text every highlighted row holds), `cell` (the text of a `cellSelect` table's current cell, drawn reverse and underlined; `" "` for a blank one, `""` for none), `contains`, `lacks`, `order` (substrings in this order),
`ticker`/`noTicker`, `notices`/`noNotices`, `styled` (text → `#RRGGBB`, or `!#RRGGBB`, for the styled run the text starts: cell-exact where `colored` is row-wide), `attrs` (text → the attributes at its first cell, followed along the row's SGR state: a comma list of `bold`, `faint`, `reverse`, `underline`, `fg` (any foreground), `bg` (any background), `-name` for absent; `"bold,-fg"` reads a mono tone), `writes` (every change an
editable tree wrote, `TreeChange.String`, in order; `[]` none), `quit` (`Runtime.Quit`: app.quit ran). A story's
`checks` (`[{verb, name, ask, refuse, dirty}]`, verb default `app.quit`) hang verb
checks (`AddVerbCheck`) that ask or refuse, with `dirty` only once the story has
written something. A foreground program (`Foreground`, an actions.json
row's `foreground`) runs headless (no input, its output dropped); a `job`
event naming its verb waits for its exit. A story with no final expect is
compared with `<name>.golden`: the frame in the mono theme, each row behind a
gutter that reads `R|` when the row has full-strength reverse video, `r|`
when its only reverse video is dimmed, `C|` (`c|` dimmed) when the
highlighted row holds a `cellSelect` table's current cell, and ` |` otherwise.

Chrome: `full` is the whole shell at width x height. `none` cuts the page
body out of a shell made exactly as much larger as its chrome, so a
one-component story shows that component and nothing else. `ticker-only`
draws the ticker alone: the rule line above the footer, where the ticker
rides, cut out of a full shell (give the story height 1).

### The page builder and the demo noun

One builder (the demo app's `pages` fixture, `demo/build.go`) makes every
page; the only application noun is `demo`. Pages and rows are of it, so its rows answer everywhere on a story
page: `q` quits (leaves the story), `r` resets it, `enter` opens a row
(attaches the fake pane on the terminal's attach row), `n` adds a notice,
`x` deletes the row behind a confirm (`Delete {{label}}?`), and each job
spec adds `demo.<name>` with its keys. `demo.ping` is for the palette. A `commands`
page is the exception: it is `cli.AddCommands` itself, its rows of the
`command` noun (`enter` runs, `i` help, `x` stop), its runs fake panes.
A `catalog` page is the other: it is `cli.MountPage` itself, its rows of
the catalog's nouns (no verbs unless a list lists them; `q` on the page quits), its loads served
from `answers`.

### The fakes

- **Pane**: a `TerminalStream` that echoes what it receives and prints
  `output N` per output tick (the TUI ticks once a second). Every change
  queues one frame and the pane hands them out in order, so frames depend on
  event order only.
- **Job**: `demo.<name>` runs until its `finishOnKey` (or `failOnKey`) is
  pressed, so ticker stories need no daemon; `label` is its `Result.Label`.
- **Completion**: an input's `completions` triggers answer the `items`
  holding the query (ignoring case), in order, or fail with `fail`; the
  search is the `completion.search` job, after the `debounce` tick (tab on a
  token asks without one).

## The TUI

`o story` lists stories by area. Enter opens one in the real chrome, drawn
in a box of the story's own size centred on your terminal (a 40x12 story is
visibly 40x12). Press keys as in any app. `r` resets the story; `q` or
`ctrl+c` returns to the index. Inside an attached fake pane `ctrl+c` goes to
the pane (the passthrough rule), so leave with `ctrl+a d` then `q`.

`p` on a story plays its script instead: the script runs one event at a
time, headless, with the story's frame boxed on the right and the script
as a column on the left. Each row is the event and, once played, what it
ran (`collection.move`) or what the script asserts after it, `✓` green when it
holds and `✗` red when it does not; a row with nothing to assert shows no
mark. The last row is the end: the final expect, else `frame = golden`.
The row the player stands on is marked `▸`, and the current position's
failures are listed under the column.

| key | does |
|---|---|
| `space` `l` `right` | play the next event |
| `h` `left` | back one step (the session is rebuilt and replayed) |
| `r` | start over |
| `f` | play everything that is left now |
| `a` | play on at the set speed (400ms); again pauses |
| `+` `-` | halve or double the delay (40ms to 4s) |
| `i` | record from here |
| `s` | write the script so far as a recording and ask for a name |
| `q` `esc` | back to the index |

Recording (`i`) makes the current step the starting state: the unplayed
rest of the script is dropped, and every key you press goes to the story
and appears in the column as it happens, as do the jobs that deliver and
the fake pane's output lines. `ctrl+g` stops recording (the play keys are
the story's while it runs); `h` and `r` then replay through the recorded
events like any script, and `s` saves them (the base's events first) to
the history ring, with the usual `save as:` prompt.

## Recording

Every TUI session in a story is recorded:

```js
{ story: "spill: j at the last row", started: "2026-10-24T...",
  initial: { ...the story... },
  events: [{ t: 812, key: "j" }, { t: 1630, job: "demo.build", state: "done" }, { t: 5630, tick: "linger" }] }
```

On leaving a story a prompt asks `save as:`. The recording is always written
first to `$XDG_DATA_HOME/o-story/history/` (a ring of the last 5; the oldest
is dropped); a name moves it to `$XDG_DATA_HOME/o-story/saved/<name>.json`,
which the ring never touches. Esc or an empty name leaves it in the ring.

```sh
o story history                 # 1 = newest, and the saved names
o story keep 2 spill-bug        # promote ring entry 2
o story replay spill-bug        # live, with the recorded timing
o story run --from spill-bug [--expect '{"focus":"b1"}']   # headless; asserts only an expect
```

## Reproducing and retaining a bug

1. Reproduce it in the TUI on the nearest story; leave and name the recording.
2. `o story trim NAME --at K` writes `trimmed/NAME-I-J.json`: events
   `[K-3 .. K+2]` as the script, `base: "recording:NAME"`, `start: I`. The
   runner replays the base's events before `I` headless, then runs the
   window. `--snapshot` instead bakes the before-state (lists, rows,
   selection, marks, focus, page offset) into the story's page so it stands
   alone. A window is never a single frame.
3. The expect `trim` writes is the state it observed; edit it to the correct
   state, so the story fails until the fix lands.
4. Extend the area's composite story, or write an ordinary test; do not add
   a new story file without the owner asking.

Keep the recording and trimming tools for diagnosis. A trimmed recording
does not need to become another committed story file. Prefer assertions on
state and short frame regions; keep at most one or two full-frame goldens
per area composite.

## Explore

`o story explore` runs the state explorer (`explore.Run`, the package doc
has the details) over every story-built application: one run per distinct
application (stories that differ only in script and expect share it;
trimmed stories with a base are left out). Each replay builds the story
afresh with its fakes and closes it after. It presses every key that
answers, plus esc, enter, tab, shift+tab, backspace, ctrl+c, a wait and
resizes, breadth first, and checks every state it reaches: panics, the
focus invariants, render fits, honest footer hints, a way home, jobs
settle, operation errors, modes no row leaves.

```sh
o story explore                        # every application, depth 3, 2000 steps each
o story explore composites/lists --depth 6 --steps 20000
o story explore --out /tmp/explore     # NAME.json reports and NAME/NN-CHECK.json gap stories
o story explore --compare-full         # also count states keyed with the screen
```

Each report lists findings by check with the shortest key path (shrunk),
then coverage: noun/mode pairs reached, modes never entered, operations
never run, rows never answered, guards never true or never false. The
last block groups the findings of every application. A gap story is the
application's story with the finding's keys as its script and
`"skip": "gap: CHECK: SUMMARY"`; waits and resizes have no story event,
so they are named in the skip. They are written to `--out`, never to
`stories/`. Retain a confirmed regression by extending the area's composite
or writing an ordinary test; do not add a new story file without the owner asking.

`o story explore --dedupe DIR` explores nothing: it reads every report under
DIR (`--out`'s `NAME.json`, and the examples' `OCHAZUKE_EXPLORE_OUT`
reports), groups the findings by check and normalized summary
(`explore.Dedupe`: ids, numbers and the `app.` namespace stripped), keeps per
group the shortest path, then the smallest story application, then the name,
and prints one line per group; `--reproduce` replays each group's best story
path on this build (`explore.Shows`), `--out` writes `groups.json` and the gap
story of each group. The same reports give the same bytes. Retain a confirmed
bug by extending the area's composite or writing an ordinary regression
test, not by adding another `stories/gaps/` file. Example-only gaps remain
`explore.Gap` tests (`TestExploreGaps`) beside the example's `TestExplore`
(behind the `explore` build tag: `make explore-quick`). Expected refusals are quieted by
`Subject.Expected`: `explore.FrameworkRefusal` for the framework's own, and
here `storyFake` for the stories' fakes.

### Watching a run

`o story explore … --watch` plays the same run in a full-screen TUI (an
ochazuke model of its own): on the left the application (n/N) and the
run's status, the states visited, the steps played of the budget and
left, the depth, the key path that reached the state shown, and a table
of the findings as they turn up; on the right that state's frame, boxed
at the story's size beside a step counter. `--delay D` (default `50ms`; `0`
is full speed) is the pause after each step. Watch mode is a view, not
another walk: the run calls `Budget.Observe` after every step and the TUI
holds it there, so the same budget gives the same states, findings and
report bytes with or without `--watch`; only the elapsed time differs.
The TUI draws on `/dev/tty`, so `o story explore --watch > report.txt`
keeps the report in the file.

| keys | verb | what it does |
|---|---|---|
| `a` | `watch.pause` | pause, or run on at the set speed |
| `l`, `right`, `space` | `watch.step` | pause and play exactly one more step |
| `+`, `=` / `-` | `watch.faster` / `watch.slower` | halve (under 1ms is full speed) / double the delay |
| `q` | `watch.stop` | stop watching: the run finishes unwatched and the report prints as without `--watch` |
| `enter` | `finding.replay` | replay the selected finding's path frame by frame (pauses the run) |

On the replay page (`explore.Frames` draws the frame before the path and
after each key) `l`/`right`/`space` step, `h`/`left` step back, `r` and
`f` go to the start and the end, `a` plays on, `+`/`-` change its speed,
`q`/`esc` go back to the run, still paused. While the run goes on the
table holds each finding's path as found; once an application's run ends
it holds the report's findings, paths shrunk. Several applications are
watched one after another; after `q` the rest run unwatched and the
reports print in order.

## Catalog

`o story catalog` answers like every family tool (ochazuke/cli, schema 1):
data nouns `story` (list, show, run, copy, fixture, recordings, state),
`recording` (list, show, delete), `fixture` (list, show, stories),
`application` (list, show, stories, fixtures: the host's apps; ochazuke's
runtime owns the noun `app`), `verb` and `edge` (list, the rows of
`o story verbs`). The relations are one handler on both nouns:
`story list --app A --fixture F` is `application stories` and
`fixture stories`, `fixture list --app A` is `application fixtures`,
`recording list --story NAME` is `story recordings`. Every list returns
`{id, label, data}` rows; `story state` prints all of them as JSON Lines.
The `story` noun declares its table (`storyColumns`: story, area, size,
steps, gap) and its rows carry `cells`, so `--format table` and nabe show
the same columns the TUI index draws (each area a table without the area
column, size then steps dropping first as the terminal narrows).

```
o story story list               # id AREA/NAME, data {area, app, fixture, size, chrome, steps, skipped, expect?}, cells
o story story copy --target lists/lone-list --as "my lone list"   # into $XDG_CONFIG_HOME/o-story/stories, with its golden
o story recording delete --target recording/1
o story fixture list             # fixture/demo/pages
o story application show --target application/demo
o story story run --target lists/lone-list
o story recording show --target recording/1
o story story state              # {"noun":"verb","id":"verb/normal/list.move",...} per line
```

