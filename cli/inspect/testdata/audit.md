# Verb audit

Audited 2026-10-27 against the rules.

Tools read (bin on PATH, its repo's HEAD):

| tool | catalog version | repo commit |
|---|---|---|
| host (matomeru) | `v0.9.0` | `abc1234` 2026-10-27 |

## 1. Summary

| tool | nouns |
|---|---|
| host | 4 |

## 2. Per tool, per noun

Intro prose that is not parsed.

### host

#### host `pane`

Existing ([keys] = default keys; no bracket = no key): `attach` [enter], `associate` [m], `record`

*Acts on the thing*

- `pane.show` — one pane as JSON.
- `pane.kill` / `pane.rename {name}` — maybe: both.

*The other direction of a relation*

- `pane.associate {project}` — already there.

No suggestions (the set fits the thing, or it is UI plumbing): `app` (`about`); `field` (`cancel` [TUI], `commit` [TUI])

## 3. One-way relations

### Firm

| tool | verb on A | B (param) | how the param is typed today | reverse to add on B |
|---|---|---|---|---|
| host | `pane.associate` | project (`project`) | string, `Source: projects.list` | `project.associate {pane}` |
| host | `pane.associate` | project (`project`) | string | `pane.show` |

## 4. Parameters that should be noun-typed

Prose.
