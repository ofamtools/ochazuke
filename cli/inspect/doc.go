// Package inspect merges ochazuke tool catalogs (the JSON BIN catalog
// prints) into one model and writes it as a self-contained HTML page for
// browsing nouns, verbs, key rows, user actions, relations and, when
// annotations are given, the verbs someone suggested adding (the Gaps
// view). cli mounts it as BIN catalog inspect; it imports only the
// standard library, so it adds nothing to cli's dependencies.
//
// Merge folds the sources: framework nouns (ochazuke.X, or the bare names
// an older, un-namespaced catalog carries, other than app, whose verbs are
// all builtin) are kept once under a pseudo-tool named ochazuke, whichever
// catalogs carry them; the definition from the newest ochazuke wins (a
// namespaced catalog is newer than a bare one whatever its build info
// says). Each verb lists the tools whose catalogs carry it, so a verb only
// older tools have shows as such. A tool's own verb on a framework noun
// keeps the tool as its owner. A noun named with another source's short
// name and a dash or dot (Namespaces: mtm's ai-agent) groups as that
// extension's mirror and links to the extension's noun. User actions
// (actions.json) are the host's verbs; one the host's catalog already
// lists (installed) is marked, not repeated. Key rows stay per tool: the
// host's are its effective rows (what config export writes), every other
// tool's its catalog's defaults.
//
// Write renders the page: the data inlined as JSON, plain JS, no network,
// works from file://. Press ? in the page for its keys (1-4 views, j/k,
// h/l, / search, enter drills in).
//
// Annotations are the Gaps view's input (see the type for the JSON);
// AuditAnnotations reads the o-family's verb-audit.md format into them, the
// format catalog inspect --audit takes.
//
// smoke.mjs in this directory is the page's headless check: it loads a
// generated page in headless Chrome over the DevTools protocol, fails on a
// console error or exception, drives every view by keyboard and writes
// screenshots:
//
//	node cli/inspect/smoke.mjs PAGE.html [SCREENSHOT-DIR]
//
// For a page to run it on, OCHAZUKE_INSPECT_OUT=/tmp/demo.html go test
// ./examples/demo -run TestCatalogInspect writes the demo's.
package inspect
