# o-story

A storybook-style sandbox for ochazuke. A **story** is a scenario as data: a
synthetic page, a script of events, and what must hold afterwards. The same
file opens by hand in the real chrome (header tabs, footer hints, ticker,
notices, `?` help) and runs headless as a test.

Stories never read production state: every row, label, pane line and job is
in the file. Labels may look real (`"labels": ["tuios-deck", ...]`); they are
still data.

```sh
o-story                      # index of stories by area; enter opens one
o-story run [NAME|AREA...]   # headless; non-zero exit on failure
o-story run --update --dir cmd/o-story/stories   # rewrite goldens on disk
o-story list | history | keep N NAME | replay REF | trim REF ... | verbs
o story ...                  # the same, through the oden family root
```

`go test ./cmd/o-story/...` runs every story in `stories/`.

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
  page: {                         // or pages: [{ id, title, ... }, ...]
    lists: [{ id: "a", rows: 2 },                  // rows a1, a2
            { id: "b", rows: 3, noSpill: false },
            { id: "c", rows: 10, height: -1,       // 0 every row, -1 rest of the page
              labels: ["first project", "tuios-deck"], selected: 1, marked: [0] }],
    order: ["a", "b", "c"],       // drawing order (default: as listed)
    layout: "stack",              // or "row": lists side by side
    focus: "b1", offset: 0, noHeadings: false,
    inputs: [{ id: "name", label: "name", text: "hello" }],
    terminal: { fake: true, attach: "a1", height: 6 },
    jobs: [{ verb: "demo.build", key: "w", finishOnKey: "W", failOnKey: "X", quiet: false, start: 0 }],
    bind: [{ noun: "demo", mode: "normal", keys: "C", do: "notice.clear" }],
  },
  script: ["j", "j", { type: "ping" }, { paste: "text" },
           { job: "demo.build", state: "done" },
           { tick: "linger" }],   // tick: linger | binding | terminal | output
  expect: { focus: "b1", visible: true },
  // or per step: [{ after: 2, focus: "b1" }, { ticker: [] }]  (after N = after the Nth event)
}
```

Expect fields: `focus`, `mode`, `modal` (top modal id, `""` none), `page`,
`visible` (the focused label is on screen), `highlighted` (rows with reverse
video), `contains`, `lacks`, `order` (substrings in this order),
`ticker`/`noTicker`, `notices`/`noNotices`. A story with no final expect is
compared with `<name>.golden`: the frame in the mono theme, each row behind a
gutter that reads `R|` when the row has reverse video and ` |` otherwise.

Chrome: `full` is the whole shell at width x height. `none` cuts the page
body out of a shell made exactly as much larger as its chrome, so a
one-component story shows that component and nothing else. `ticker-only`
draws the model's ticker rows alone (the ticker is the model's chrome, so
this is the simplest honest strip).

### The page builder and the demo noun

One builder (`story.Build`) makes every page; the only application noun is
`demo`. Pages and rows are of it, so its rows answer everywhere on a story
page: `q` quits (leaves the story), `r` resets it, `enter` opens a row
(attaches the fake pane on the terminal's attach row), `n` adds a notice,
`x` deletes the row behind a confirm (`Delete {{label}}?`), and each job
spec adds `demo.<name>` with its keys. `demo.ping` is for the palette.

### The fakes

- **Pane**: a `TerminalStream` that echoes what it receives and prints
  `output N` per output tick (the TUI ticks once a second). Every change
  queues one frame and the pane hands them out in order, so frames depend on
  event order only.
- **Job**: `demo.<name>` runs until its `finishOnKey` (or `failOnKey`) is
  pressed, so ticker stories need no daemon.

## The TUI

`o-story` lists stories by area. Enter opens one in the real chrome, drawn
in a box of the story's own size centred on your terminal (a 40x12 story is
visibly 40x12). Press keys as in any app. `r` resets the story; `q` or
`ctrl+c` returns to the index. Inside an attached fake pane `ctrl+c` goes to
the pane (the passthrough rule), so leave with `ctrl+a d` then `q`.

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
o-story history                 # 1 = newest, and the saved names
o-story keep 2 spill-bug        # promote ring entry 2
o-story replay spill-bug        # live, with the recorded timing
o-story run --from spill-bug [--expect '{"focus":"b1"}']   # headless; asserts only an expect
```

## How a bug becomes a story

1. Reproduce it in the TUI on the nearest story; leave and name the recording.
2. `o-story trim NAME --at K` writes `trimmed/NAME-I-J.json`: events
   `[K-3 .. K+2]` as the script, `base: "recording:NAME"`, `start: I`. The
   runner replays the base's events before `I` headless, then runs the
   window. `--snapshot` instead bakes the before-state (lists, rows,
   selection, marks, focus, page offset) into the story's page so it stands
   alone. A window is never a single frame.
3. The expect `trim` writes is the state it observed; edit it to the correct
   state, so the story fails until the fix lands.
4. Move it into `stories/<area>/` when it is a rule of the framework.

Every chrome or motion rule has a story here; a UI bug is reproduced as a
story before it is fixed.

## Minimum verbs per component

Every key resolves to a `noun.verb`, so the verbs the core stories exercise
are the minimum a component needs to be full-featured; which keys reach them
is the app's choice. The table is derived from the stories (per mode the key
was resolved in) by `o-story verbs`, and `go test` fails when it drifts.
The edges are the mode changes the stories make. `demo.*` rows are the
sandbox's own noun.

Conformance:

- An app's bindings must reach every verb in this set. `ValidateBindings`
  covers the static half (every row atomic and registered).
- Every mode that can be entered has an exit edge reachable from inside it
  (terminal needs detach, insert needs esc or cancel, a pending mode a
  timeout or esc); `o-story verbs` fails if a story enters a mode that no
  story leaves.
- The core stories must pass when replayed through the app's bindings:
  `o-story run --bindings FILE`, a run mode planned for later (not built).

<!-- verbs:begin (generated by o-story verbs --readme README.md; do not edit) -->

| mode | noun | verbs |
|---|---|---|
| insert | input | `input.insert` |
| insert | mode | `mode.set` |
| insert | palette | `palette.move` `palette.run` |
| normal | bindings | `bindings.help` |
| normal | confirm | `confirm.accept` |
| normal | demo | `demo.build` `demo.delete` `demo.finish` `demo.notify` `demo.open` `demo.reload` `demo.ship` |
| normal | focus | `focus.next` `focus.previous` |
| normal | input | `input.begin` |
| normal | list | `list.move` |
| normal | modal | `modal.pop` |
| normal | notice | `notice.clear` |
| normal | page | `page.next` `page.previous` |
| normal | palette | `palette.open` |
| normal | surface | `surface.scroll` |
| terminal | mode | `mode.enter` |
| terminal | terminal | `terminal.detach` `terminal.send` |

Mode edges (from → to via):

- `insert` → `normal` via `mode.set`
- `insert` → `normal` via `palette.run`
- `leader_pending` → `normal` via `terminal.detach`
- `normal` → `insert` via `input.begin`
- `normal` → `insert` via `palette.open`
- `normal` → `terminal` via `demo.open`
- `terminal` → `leader_pending` via `mode.enter`

<!-- verbs:end -->
