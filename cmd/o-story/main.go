// Command o-story is ochazuke's story tester: the host
// (ofam.dev/monogatari/host) of ochazuke's demo app, whose
// stories are JSON files describing a synthetic page, a script of events
// and what must hold afterwards. A family tool (o-<tool>), so o story ARGS
// runs it.
//
//	o-story                         the index; enter opens a story, q leaves it
//	o-story run [NAME|AREA...] [--update] [--dir DIR] [-v]
//	o-story run --from REF [--expect JSON]
//	o-story list
//	o-story history
//	o-story keep N NAME
//	o-story replay REF
//	o-story trim REF [--from I] [--to J] [--at K] [--as NAME] [--snapshot]
//	o-story verbs
//	o-story explore [NAME|AREA...] [--dir DIR] [--depth N] [--steps N] [--out DIR] [--compare-full]
//	o-story catalog                 the nouns (story, recording, fixture, application, verb, edge) as JSON
//	o-story NOUN VERB [--target ID] story list|show|run|copy|fixture|recordings|state, recording list|show|delete, ...
//
// REF is a history number (1 newest) or a saved recording's name.
// Stories come from the embedded stories/ (or --dir) and from
// $XDG_CONFIG_HOME/o-story/stories; recordings live under
// $XDG_DATA_HOME/o-story (history/ ring of 5, saved/).
package main

import (
	"embed"
	"io/fs"
	"os"

	"ofam.dev/monogatari"
	"ofam.dev/monogatari/host"
	"ofam.dev/ochazuke/cmd/o-story/demo"
	"ofam.dev/oden"
)

//go:embed stories
var embedded embed.FS

// config is o-story's host: the demo is its first app, so a story that
// names no app is a demo story. Its name, o-story, keeps its user stories
// and recordings where they have always been, whatever command runs it
// (o story, mtm story).
func config(prog string) host.Config {
	stories, err := fs.Sub(embedded, "stories")
	if err != nil {
		panic(err) // the embed always has stories/
	}
	return host.Config{Name: "o-story", Prog: prog, Apps: []monogatari.App{demo.App()}, Stories: stories}
}

func main() {
	prog := "o-story"
	if p := os.Getenv("ODEN_PARENT"); p != "" {
		prog = p + " story"
	}
	os.Exit(oden.Report(os.Stderr, prog, host.Main(config(prog), os.Args[1:], os.Stdout, os.Stderr)))
}
