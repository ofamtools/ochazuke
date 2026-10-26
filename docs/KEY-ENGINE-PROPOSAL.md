# Key engine proposal: keys in, steps out

Status: proposal, not scheduled (2026-10-26). Nothing here is decided for ochazuke.

A prototype lives outside ochazuke in `~/proj/o-modal` (engine in `modal/`, a CSV app in `cmd/csv/`). What it covers so far:

- **Built:** a pure `Feed` returning a list of steps; prefixes, fallbacks and counts; capture rows; key classes; guards; `Step.Replay`; the host-side confirm; the copy-and-run preview (`App.Simulate`, `Sheet.Fork`).
- **Designed, not built yet:** `mode.set` as an event with the host as only writer (the prototype's engine still applies `modal.mode` rows itself); macros and dot-repeat in the host (the prototype's engine still records macros); the round-trip rule; the branching preview with `preview.possibleModes`.

## Background

- "Events out, not just keys in" (2026-10-24, item 6 in `MIGRATION-PLAN.md`) is about change notifications the runtime emits *after* something happened. That's a different idea from this one. This proposal is about the key handler returning what to do instead of doing it.
- Today `Runtime.Dispatch` resolves a key and runs the row at once (`runRow` → `Execute`). It returns only `(claimed bool, error)`.

## The design

1. **The engine has no side effects.** `Feed(state, env, key) → (state, steps)`. It always returns a list of steps, usually one.
2. **The engine reads the mode and never writes it.** The mode is input (`Env.Mode`), like guards. A row that changes the mode emits a `mode.set` event, and the host decides whether to apply it. The host is the only writer of the mode.
3. **The engine's memory is key-level only:** a pending sequence prefix, its fallback, and a count.
4. **The engine still replays keys itself in one case.** When a sequence breaks off (`y j`), the fallback runs and the key is offered again (`Step.Replay`). The host feeds it back after running the steps before it, so the key sees their effects.
5. **Macros and dot-repeat belong to the host, as replayed keys.** The host owns the input loop, so it records keys before handing them over and replays them by pressing them again. Dot-repeat uses a round-trip rule:

   > A change is a round trip that starts and ends idle in normal mode (no pending sequence, no count) and ran at least one repeatable step along the way.

   The same round trip groups undo: one snapshot at the start of a round trip.
6. **Confirm is the host's concern.** `confirm` is an ordinary mode in the keymap (rows for `y`, `n`, `esc`). The host's `Run` decides to ask, parks the request and enters the mode. The engine has no idea what a confirm is.
7. **Rows stay atomic:** one row carries one request.
8. **There are two previews:**
   - **Copy-and-run:** the host forks its state and drives the copy with a second machine. Every step really runs, on the copy. Effects outside memory, such as files, are only described ("would save …"). It's exact, and it follows one path.
   - **Branching:** a walk over `Feed` with copied engine state, which runs no host code. It forks wherever an operation may change the mode. There's no separate pure preview; it's the `NoForks` policy.

### Branching API (sketch)

```go
type Fork struct {
	Key   int      // index of the key that caused it
	Step  Step     // the step whose outcome is unknown
	Modes []string // from the operation's preview.possibleModes
}

type Choose func(Fork) []string // which modes to follow at a fork

var FollowAll, NoForks Choose
func Pick(choices ...string) Choose // Pick("row.delete=confirm")

type Path struct {
	Choices []string // its address, e.g. ["row.delete=confirm"]
	Steps   []Step
	State   State
	Mode    string
}

paths, more := km.Explore(st, env, keys, ops, chooser, limit)
paths.Where("row.delete=unchanged")

w := km.Walk(st, env, ops) // interactive: Press, Forks, Take
```

- **The limit:** past it, the walker stops forking, keeps going down the paths it already has, and reports how many it dropped (`more`). Paths that reach the same mode and pending state are merged first.
- **Each path has a stable address** made of its choices, such as `row.delete=confirm`. Tests use it with `Pick` and `Where`.

### Preview declarations

These are preview-only data on operations. The host's own code makes the real change.

```json
"operations": {
  "row.delete":  {"preview": {"possibleModes": ["confirm", "unchanged"]}},
  "cell.edit":   {"preview": {"possibleModes": ["insert"]}},
  "confirm.yes": {"preview": {"possibleModes": ["normal"]}}
}
```

- One entry is a certain change: no fork.
- Several entries fork into exactly those branches.
- No entry means the operation never changes the mode.
- `unchanged` is reserved and can't be a mode name.
- A test checks the declarations against reality: run copy-and-run on the same keys, and assert that its path is among the branches.

## How it would apply to ochazuke

### Who plays "the host"

| o-modal | ochazuke |
|---|---|
| Engine (`modal`) | A new pure package pulled out of `resolve.go` (already stdlib-only) |
| "The host decides" | Mostly the **Runtime**: confirm, history, macros, dot-repeat. Sometimes an application's handler |
| CSV `Sheet.Run` | `Runtime.ExecuteAt` and the handlers it calls |

### What maps over cleanly

- **Steps out:** `dispatchIn` becomes a pure function that returns steps, and `Dispatch` becomes "resolve, then run". `Binding.Replay` and the re-dispatch when a sequence breaks off both become `Step.Replay`.
- **`mode.set` as an event:** the operations already exist (`mode.enter`, `mode.leave`, `mode.set`). What changes is a single writer. Today the mode is written by `setMode`, `SetMode`, `leaveMode`, `enterPrefix`, and `runRow` leaving a pending mode before and after.
- **Atomic rows:** unchanged. `TestBindingRowsAreAtomic` stays.
- **`ochazuke bindings explain "d d"`:** the branching preview with `NoForks` or `FollowAll`. The `?` help and the pending-key hints in the footer could use it too.
- **The sequence timer:** stays in the host (`BindingTimeoutCommand`).

### What it would overturn

1. **Pending conditions stop being modes.** `AGENTS.md`: "Every pending condition is an exclusive mode with a typed `Payload` on `Runtime.Mode`" (`count_pending`, `g_pending`, `register_pending`, `"normal g"`, the `macro_recording` shadow). Here, prefix and count are engine memory, and `Runtime.Mode` holds only modes a user would recognize. Everything that reads pending modes moves to engine state: the footer hints, `bindings.list` tables per pending mode, and `terminal.go`'s `leader_pending` check.
2. **Dot-repeat replays keys instead of requests.** `MIGRATION-PLAN.md`: `history.repeat` "re-executes the last repeatable request", and "macros stay key-based; dot is request-based". The round-trip rule reverses that. `Operation.Repeatable` survives as the "repeatable" flag. The "confirmations are not re-asked" behavior comes from the replayed `y`, not from a stored `confirmed: true`.
3. **Macros move out of `Dispatch`.** Recording is a shadow mode inside `Dispatch` today. It would become a recorder at the entry point (`Model.Update`, or a wrapper around `Dispatch`) that sees only typed keys.

### Where ochazuke differs from the CSV prototype

- **Confirm isn't just a mode.** It opens the `confirm` noun's modal: focus moves into it, and the stack walk changes (the top modal is a boundary). A branch can open a modal, move focus or change page, so `preview.possibleModes` would grow into `preview.possibleOutcomes`, or gain a sibling `possibleModals`.
- **Confirm declarations can be automatic.** Every verb with `Operation.Confirm` opens the confirm modal, so the framework can add its preview entry itself. That removes the largest source of stale declarations.
- **Spatial motions and guards read the last frame** (`Runtime.Placed`). The engine's input must carry the drawing order. Branches share the current one, which goes stale once a branch opens a modal.

### The copy-and-run preview

This one is hard to bring over in general:

- **The Runtime itself is easy to fork.** Nouns, instances, focus, mode and registers are data.
- **Application state isn't.** It lives inside handler closures (the CSV grid, list rows), and some handlers reach outside the process: `actions.json` commands, jobs, files, the socket.

So it would be opt-in:

- An application implements a fork hook for its own state.
- Every outside effect goes through a channel the copy can turn into "would do X". The v0.4.0 UI sinks already do this for notices and status, but nothing does it for commands, jobs or files yet.
- Without the hook, `explain` uses the branching preview only. That works for every app, because it runs no application code.

## Staging, if adopted

1. Pull the pure resolver out of `resolve.go`, with `Dispatch` as "resolve, then run" and no behavior change. The keytest suite is the check.
2. Add `bindings explain` on the branching preview, with automatic entries for verbs that confirm.
3. Make the Runtime the only mode writer, and move pending conditions out of `Runtime.Mode`. This rewrites the `AGENTS.md` invariant.
4. Switch macros and `.` to replayed keys. This revisits the dot-repeat decision.
5. Add the opt-in fork hook and the effect channels, only if a real app needs exact previews.

Steps 1 and 2 are low-risk and useful on their own. Steps 3 and 4 carry the real cost and the decisions.

## Open questions

- Does `confirm.yes` return to the mode the confirm opened from? That needs a value such as `"@previous"`, or a mode stack in the host.
- With replayed keys, should `3 .` replace the original count (Vim) or multiply it?
- Should replaying a change answer its confirm automatically, or should destructive verbs drop keys typed in confirm mode so they ask again?

## Follow-up (2026-10-26)

This section supersedes two parts of the text above:
- "What it would overturn", item 1: pending conditions can stay modes;
- `possibleOutcomes` / `possibleModals`: those are side effects, stubbed at the boundary.

### Fork binds

A confirm belongs to the operation: `row.delete` asks whether it came from a key, the palette or the CLI. So mark the verb, and every binding that points at it becomes a fork bind automatically:

```go
Define("row").Verb("delete", del).Confirm("Delete row?") // a confirm verb forks
Define("row").Verb("archive", arc).Forks("confirm")      // an explicit fork, same mechanism
// Bind("d d", Do("row.delete")) is then a fork bind, with nothing extra on the row
```

### Pending conditions stay modes

Moving pending conditions out of `Runtime.Mode` was a choice made for o-modal, not something the design requires. Instead:

- The engine emits `mode.set "normal g"` when a sequence starts, and `mode.set normal` when it finishes or times out.
- The Runtime applies them like any other mode change.
- Pending conditions stay modes with a payload, as `AGENTS.md` says.
- `bindings.list` per pending mode, the footer hints and `terminal.go`'s `leader_pending` keep working.

The costs: more `mode.set` events (one per prefix key), and a pending mode the host could refuse, which is meaningless, so the host accepts them. That's cheaper than rewriting the invariant.

### Dot-repeat as keys

Agreed, and contained. `Operation.Repeatable` already marks changes, and history already records `source: key:…`. It needs a recorder at the entry to `Dispatch` using the round-trip rule.

Open question: what does `.` do after a change made from the palette or CLI, which has no keys? Vim doesn't repeat `:` commands with `.`, so "palette and CLI changes don't count" is a reasonable answer.

### Macros

Still open, to discuss separately: where recording happens (`Model.Update`, or a wrapper around `Dispatch`), and whether macros leave the library entirely.

### Side effects in previews: stub at the boundary

One rule for both previews:

> A preview models only what changes how the **next key** resolves: the mode, and in ochazuke the focus. Everything else is a side effect, stubbed at the boundary and recorded: "called `row.delete` → would ask for confirmation", "would run `git fetch`", "would write `people.csv`".

So `possibleModes` stays, because the mode changes what `y` means. Possible modals and outcomes are just side effects. A test does the same thing at the same boundary: stub it, then assert it was called.

### A sandbox for live previews

The step up from stubbing files: a stub says "would write `people.csv`"; a sandbox actually writes a copy, so the result can be shown.

- **Copy on first write:** the preview's file layer reads real files. The first write to a path copies it into a temp directory, and later reads and writes of that path use the copy. At the end, the preview shows a diff against the real files and deletes the temp directory.
- **This needs a file layer:** the app reaches files through an interface (open, write, rename) instead of `os` directly, so the preview can swap in the overlay.
- **External commands are the limit.** `git push`, network calls and other processes can't be sandboxed this way. A command run in the temp directory can still touch absolute paths or the network. Those stay stubbed unless the operation is declared safe to run in the sandbox.

### How Haskell handles side effects

In Haskell, a function isn't allowed to *do* anything. It can only compute a value from its inputs.

- **It returns a recipe instead of doing the thing.** A function that "writes a file" returns a value that *describes* writing the file: a note saying "write this text to this path". Nothing happens when the function runs.
- **The runtime is the only cook.** The program hands one big recipe to the Haskell runtime, and only the runtime carries it out.
- **A monad is the "and then" glue.** Recipes need chaining: "read the file, *and then* with what you read, write a summary". The second step depends on the first one's result, which doesn't exist until the recipe runs. A monad is the standard way to say "do this, then pass its result to the next step" while still only describing the work. `do` notation is a readable way of writing that chain.
- **One recipe can be carried out more than one way.** Advanced Haskell code uses one recipe with different interpreters:
  - a **real** one writes files;
  - a **test** one records "write was called with these arguments" (the stub at the boundary);
  - a **preview** one writes into a temp directory (the sandbox).

This design is the same idea:
- `Feed` returning steps is "return a recipe instead of acting";
- the host's `Run` is the cook;
- the copy, the stubs and the sandbox are different interpreters of the same recipe.

Bubble Tea's `tea.Cmd` is a small version of it: `Update` returns a command describing work, and the Bubble Tea runtime runs it.
