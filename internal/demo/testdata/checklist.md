# Release checklist

Each box is a task item: **j** and **k** move between them, **x** toggles the
selected one, **u** and **ctrl+r** undo and redo. Only the box character is
ever rewritten.

## Before the tag

- [x] Write the changelog section
- [ ] Run the gate with `GOWORK=off`, the build a fresh clone gets
  - [x] gofmt and go vet
  - [ ] go test -race
  - [ ] the o-story stories
- [ ] Bump the consumers

## After

1. [ ] Check the module proxy
2. [ ] Announce it

```sh
- [ ] a line in a code block is not a task
```
