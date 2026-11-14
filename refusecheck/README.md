# refusecheck

`ofam.dev/ochazuke/refusecheck` fails a Go test when an application adds a
refusal. It is the standing check of the no-refusals plan, shaped like
`namecheck`: an AST scan, a baseline keyed by file, function, class and
template (never a line), and a ratchet that only shrinks.

## Adopt it

One test in the application's root module, one baseline file:

```go
func TestNoNewRefusals(t *testing.T) {
	refusecheck.Check(t, ".", "testdata/refusecheck.txt")
}
```

`Check` scans the directory and everything below it (hidden directories,
`testdata`, `vendor` and nested modules are skipped); `CheckConfig(t,
refusecheck.Config{Root, Dirs}, baseline)` scans chosen packages. Run once
with `REFUSECHECK_UPDATE=seed` to write the baseline of what exists (every
row is debt, except the classes that stay by rule: K12 `rights`, K13
`declaration`, K14 `failure`). After that, `REFUSECHECK_UPDATE=1` removes rows
and lowers counts once a site is gone; it never adds one. Add `go test -run
TestNoNewRefusals` to the repo's gate `steps`.

## What counts as a refusal

A place that answers "no" to a request that could have run. The scan finds:

- error sites with a literal template: `fmt.Errorf`, `errors.New`, `Failed`,
  `Refuse`, `resultError`, `refused`, and the literals `ReadOnlyError{}`,
  `NotApplicableError{}`, `MissingParameterError{}` that carry a message;
- declarations that make a verb or value refuse: an `Unavailable:` or
  `Lacks:` field, a `When` guard on a verb with no `Fallback`, a `Confirm`
  without `Loses`. An `Enum` is not a site: since no-refusals N2 a value
  outside it is accepted (`Parameter.Accept`: keep, register, near).

`Fallback`, `Loses` and `Accept` may not exist in the framework yet; until
they do, such a declaration is a site in the baseline, and it leaves the
baseline when the field is added.

Each site gets a class K1 to K15 from `classes.json` (ordered message
patterns; declarations carry their class; a `%w` wrap is K14; the rest is
K15, which only shrinks as each lane classifies what it touches). A failure
names the class and what to use instead.

## The four enabling answers

Replace the refusal with one of these:

1. **Run.** Accept the value: `Accept` (register, keep or near) on the
   parameter, setting or prop; `Normalize` for input; create what is missing.
2. **Run instead.** Route to the verb that can: `PropSpec.WrittenBy`, a
   `Fallback` on a guarded verb, the nearest legal move for a conflict.
3. **Ask.** Open the form or picker: `Ctx.Need(noun)`, `MissingParameterError`
   through the args form, a Choice answer listing the candidates and the
   command for each (headless).
4. **Record.** Save it as unassociated or pending and keep the request: an
   untitled note, a pending job that runs when its need is met.

## Keep an exception

A refusal that is legitimate is a hand-added baseline row with a reason in
the last column; the reviewer reads that line. Reasons:

| Reason | Exception | Stays because |
|---|---|---|
| `rights` (E1) | agent rights | owner rule |
| `loses` (E2) | a confirm before losing what undo cannot restore | owner rule: no data loss without a confirm; declare `Loses` |
| `failure` (E3) | a real failure (disk, network, daemon) | the program tried; report with advice and keep the request |
| `declaration` (E4) | a developer error at registration | fails in the developer's tests; never for a user's config row |
| `cancel` (E5) | the person cancels | their choice |

A row with a reason stays when the count matches; its count still only goes
down. A row without a reason is debt.
