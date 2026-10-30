package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/keytest"
	"github.com/unixsuperhero/oden"
)

// demoHost is a host (as mtm and nabe are) that mounted the demo's
// catalog with cli.MountNouns and no code of its own per noun: each
// mounted verb runs the demo's verb in process, as a proxy would run
// the extension's binary.
func demoHost(t *testing.T) (host *ochazuke.Model, ext *ochazuke.Model) {
	t.Helper()
	ext = New()
	data, err := cli.Catalog(ext.Runtime, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	var doc cli.CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	host = ochazuke.NewModel(ochazuke.NewRuntime())
	host.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := cli.MountNouns(host.Runtime, doc.AppNouns(), cli.HostMount{
		Local: func(noun string) string { return "demo-" + noun },
		Proxy: func(noun string, v cli.CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				res, err := ext.Runtime.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args, Confirmed: true})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(res)
			}
		},
	})
	if len(got.Warnings) != 0 {
		t.Fatalf("mount warnings: %v", got.Warnings)
	}
	return host, ext
}

// The demo's task, mounted in a host, has its generated verbs there with
// no host code (working default 16): the host's CLI runs them, and a
// prop-shaped request writes through the forwarded set.
func TestMountedTaskHasItsStandardVerbsInTheHost(t *testing.T) {
	host, ext := demoHost(t)
	r := host.Runtime
	for _, verb := range []string{"get", "list", "create", "set", "delete", "rename", "move", "duplicate", "archive", "restore", "watch", "edit"} {
		if _, ok := r.Operations["demo-task."+verb]; !ok {
			t.Fatalf("demo-task.%s is not mounted", verb)
		}
	}
	if p, ok := r.PropOf("demo-task", "project"); !ok || p.Noun != "demo-project" {
		t.Fatalf("the reference names the host's project: %+v", p)
	}
	var out bytes.Buffer
	app := oden.New("host", []string{"demo-task", "set", "t1", "status", "done"}, oden.WithStdio(strings.NewReader(""), &out, io.Discard), oden.WithPath(t.TempDir()))
	cli.Mount(app, cli.New(r, "host", strings.NewReader(""), &out, io.Discard))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("host demo-task set: %v\n%s", err, out.String())
	}
	if got, _ := ext.Runtime.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t1", "prop": "status"}}); got != "done" {
		t.Fatalf("the demo's task after the host's set: %v", got)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "demo-task", Prop: "title", Value: "Fern", Args: ochazuke.Arguments{"id": "t1"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := ext.Runtime.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t1", "prop": "title"}}); got != "Fern" {
		t.Fatalf("a prop request in the host writes the demo's task: %v", got)
	}
}

// paletteVerbs types :NOUN SPACE in the host's palette and returns the
// frame.
func paletteFrame(m *ochazuke.Model, line string) string {
	keytest.Press(m, "esc")
	keytest.Type(m, ":"+line)
	frame := ansi.Strip(m.View().Content)
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	return frame
}

// In the host's palette the mounted generated verbs follow the host's
// palette.standard-verbs (lane S3): hidden keeps them out of the verb
// column, the noun's own verbs stay. The setting is read by name, so
// before S3 declares it the hidden half is skipped.
func TestMountedStandardVerbsFollowTheHostsPaletteSetting(t *testing.T) {
	host, _ := demoHost(t)
	frame := paletteFrame(host, "demo-task ")
	if !strings.Contains(frame, "edit") || !strings.Contains(frame, "rename") {
		t.Fatalf("the verb column lists the noun's verbs and its standard ones:\n%s", frame)
	}
	const setting = "palette.standard-verbs"
	if _, ok := host.Runtime.Settings().Value(setting); !ok {
		t.Skipf("%s is not declared yet (lane S3); the mounted verbs carry Operation.Standard for it", setting)
	}
	if err := host.Runtime.Settings().Set(setting, []string{"hidden"}); err != nil {
		t.Fatal(err)
	}
	frame = paletteFrame(host, "demo-task ")
	if !strings.Contains(frame, "edit") || strings.Contains(frame, "rename") {
		t.Fatalf("hidden keeps the mounted standard verbs out of the verb column:\n%s", frame)
	}
}
