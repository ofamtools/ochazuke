// Command minimal is the smallest complete Ochazuke app: a core.Engine with
// three keys, a ui.ViewNode tree and the otea.Shell adapter.
//
//	go run ./examples/minimal           interactive (j next, space toggles, q quits)
//	go run ./examples/minimal --smoke   drive state and render headlessly, exit 0 on success
package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
)

var tasks = []string{"write a view", "bind three keys", "run the shell"}

func done(s core.State) map[string]bool { m, _ := s.Pages["main"].Data.(map[string]bool); return m }

func view(s core.State) ui.ViewNode {
	root := ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{
		{Kind: ui.KindText, Tone: "accent", Props: map[string]any{"text": "minimal · j next · space toggle · q quit"}},
	}}
	for _, t := range tasks {
		mark := "[ ] "
		if done(s)[t] {
			mark = "[x] "
		}
		root.Children = append(root.Children, ui.ViewNode{ID: t, Kind: ui.KindCard, Ref: &core.Ref{Kind: "task", ID: t}, Focusable: true, Title: mark + t})
	}
	return root
}

func newShell() *otea.Shell {
	km, err := core.NewKeymap([]core.Mode{{Name: "normal"}}, []core.Binding{
		{Mode: "normal", Keys: []string{"j"}, Command: core.CmdFocusNext},
		{Scope: "task", Mode: "normal", Keys: []string{"space"}, Command: "task.toggle"},
		{Mode: "normal", Keys: []string{"q"}, Command: core.CmdAppQuit},
	}, core.CommandSpec{ID: "task.toggle", Description: "Toggle the focused task"})
	if err != nil {
		panic(err)
	}
	eng := core.NewEngine(km)
	eng.Handle("task.toggle", func(s core.State, c core.Command) core.StepResult {
		f := s.Pages["main"]
		next := maps.Clone(done(s))
		next[c.Target.ID] = !next[c.Target.ID]
		f.Data = next
		return core.Next(s.WithActiveFrame(f))
	})
	start := core.NewState(core.Frame{ID: "main", FocusID: tasks[0], Data: map[string]bool{}})
	return otea.New(eng, start, view, ui.DefaultDarkTheme())
}

func main() {
	smoke := flag.Bool("smoke", false, "drive the app headlessly and exit")
	flag.Parse()
	sh := newShell()
	if !*smoke {
		if _, err := tea.NewProgram(sh).Run(); err != nil {
			fmt.Fprintln(os.Stderr, "minimal:", err)
			os.Exit(1)
		}
		return
	}
	sh.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	sh.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	sh.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	_, quit := sh.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	focus, frame := sh.State.ActiveFrame().FocusID, sh.View().Content
	ok := focus == tasks[1] && done(sh.State)[tasks[1]] && strings.Contains(frame, "[x] "+tasks[1]) && quit != nil
	fmt.Printf("focus=%q done=%v quit=%v renders=%d\n", focus, done(sh.State), quit != nil, sh.Renders)
	if !ok {
		fmt.Fprintln(os.Stderr, "minimal: smoke failed")
		os.Exit(1)
	}
	fmt.Println("smoke: all checks passed")
}
