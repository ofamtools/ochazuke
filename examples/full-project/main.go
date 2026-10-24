package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"ochazuke"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "full-project:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("full-project", flag.ContinueOnError)
	dir := flags.String("config", "", "Directory containing all seven JSON config files; empty uses embedded defaults")
	socket := flags.String("socket", "", "Unix socket: serve this path with run, or send an operation to the running app")
	smoke := flags.Bool("smoke", false, "Exercise the example's configured runtime headlessly and exit")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage:\n  full-project [--config DIR] [--socket PATH] [run [--headless]]\n  full-project [--config DIR] [--socket PATH] NOUN VERB [--json '{...}'] [--target INPUT]\n  full-project [--config DIR] --smoke\n\nNo command opens the TUI. Operations without --socket use a fresh local runtime; with --socket they affect the live app.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	rest := flags.Args()
	if *smoke && (len(rest) != 0 || *socket != "") {
		return fmt.Errorf("--smoke cannot be combined with a command or --socket")
	}
	if len(rest) > 0 && rest[0] != "run" {
		if len(rest) < 2 {
			return fmt.Errorf("expected NOUN VERB; use operations list to discover actions")
		}
		callFlags := flag.NewFlagSet(rest[0]+" "+rest[1], flag.ContinueOnError)
		raw := callFlags.String("json", "{}", "Operation arguments as JSON")
		target := callFlags.String("target", "", "Input ID or focused-input")
		if err := callFlags.Parse(rest[2:]); err != nil {
			return err
		}
		if callFlags.NArg() != 0 {
			return fmt.Errorf("unexpected operation arguments")
		}
		arguments, err := ochazuke.ParseArguments(*raw)
		if err != nil {
			return err
		}
		q := ochazuke.Request{Noun: rest[0], Verb: rest[1], Target: *target, Args: arguments}
		var result any
		if *socket != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err = ochazuke.Call(ctx, *socket, q)
		} else {
			cfg, loadErr := LoadConfig(*dir)
			if loadErr != nil {
				return loadErr
			}
			m, buildErr := Build(cfg)
			if buildErr != nil {
				return buildErr
			}
			result, err = m.Runtime.Execute(q)
		}
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	cfg, err := LoadConfig(*dir)
	if err != nil {
		return err
	}
	m, err := Build(cfg)
	if err != nil {
		return err
	}
	if *smoke {
		if err := smokeExample(m); err != nil {
			return err
		}
		fmt.Println("full-project smoke: actions, layered bindings, review mode, page/modal restoration, picker filters, palette, appearance, editing, history, registers, macros and list focus/refresh: PASS")
		return nil
	}
	runFlags := flag.NewFlagSet("run", flag.ContinueOnError)
	headless := runFlags.Bool("headless", false, "Serve the same event loop without terminal rendering")
	if len(rest) > 0 {
		if err := runFlags.Parse(rest[1:]); err != nil {
			return err
		}
		if runFlags.NArg() != 0 {
			return fmt.Errorf("unexpected run arguments")
		}
	}
	options := []tea.ProgramOption{}
	if *headless {
		options = []tea.ProgramOption{tea.WithInput(nil), tea.WithoutRenderer(), tea.WithoutSignalHandler()}
	}
	program := tea.NewProgram(m, options...)
	if *socket == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		*socket = filepath.Join(cache, "ochazuke-full-project", "app.sock")
	}
	server, err := ochazuke.Listen(*socket, func(msg ochazuke.CallMsg) { program.Send(msg) })
	if err != nil {
		return err
	}
	defer server.Close()
	if *headless {
		fmt.Fprintln(os.Stderr, "Listening:", *socket)
	}
	_, err = program.Run()
	return err
}

// smokeStep, when set by a test, runs after every smoke operation and key.
var smokeStep func(label string) error

// smokeExample drives real operations and keyboard dispatch, never substitute handlers.
// It deliberately names the shipped fixture: edited configs must keep this contract
// to use --smoke, while ordinary TUI/CLI execution accepts any valid configuration.
func smokeExample(m *ochazuke.Model) error {
	r := m.Runtime
	for _, id := range []string{"title", "notes", "source", "destination", "attachments", "confirm-note", "details-note"} {
		if r.Inputs[id] == nil {
			return fmt.Errorf("smoke requires example input %q", id)
		}
	}
	for _, id := range []string{"overview", "paths", "help"} {
		if r.Pages[id] == nil {
			return fmt.Errorf("smoke requires example page %q", id)
		}
	}
	var failure error
	expect := func(label string, got, want any) {
		if failure == nil && !reflect.DeepEqual(got, want) {
			failure = fmt.Errorf("smoke %s: got %#v; want %#v", label, got, want)
		}
	}
	call := func(verb, noun, target string, args ochazuke.Arguments) any {
		if failure != nil {
			return nil
		}
		value, err := r.Execute(ochazuke.Request{Verb: verb, Noun: noun, Target: target, Args: args})
		if err != nil {
			failure = fmt.Errorf("smoke %s.%s: %w", verb, noun, err)
		}
		if smokeStep != nil && failure == nil {
			failure = smokeStep(verb + "." + noun)
		}
		return value
	}
	key := func(message tea.KeyPressMsg) {
		if failure != nil {
			return
		}
		m.Update(message)
		if m.Status != "" {
			failure = fmt.Errorf("smoke key %q: %s", message.String(), m.Status)
		}
		if smokeStep != nil && failure == nil {
			failure = smokeStep("key " + message.String())
		}
	}
	// press sends one named key ("ctrl+s", "esc", "tab") or types text rune by
	// rune, as a Bubble Tea v2 terminal delivers it.
	press := func(name string) {
		special := map[string]rune{"esc": tea.KeyEscape, "tab": tea.KeyTab}
		if code, ok := special[name]; ok {
			key(tea.KeyPressMsg{Code: code})
			return
		}
		if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
			key(tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl})
			return
		}
		for _, r := range name {
			key(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	binding := func(keys ...string) {
		for _, k := range keys {
			press(k)
		}
	}
	replace := func(id, text string) {
		call("replace", "input", id, ochazuke.Arguments{"start": 0, "end": len(r.Inputs[id].Text), "text": text})
	}
	call("open", "page", "", ochazuke.Arguments{"id": "overview"})
	call("focus", "input", "", ochazuke.Arguments{"id": "notes"})
	replace("notes", "alpha beta")
	replace("title", "Release")
	call("move", "input", "title", ochazuke.Arguments{"motion": "line-start"})
	expect("custom parameters", call("greet", "workspace", "", ochazuke.Arguments{"name": "Ada", "repeat": 2}), "Hello, Ada! Hello, Ada!")
	expect("editor override", call("save", "workspace", "", nil), "editor saved")
	expect("explicit target fallback", call("save", "workspace", "title", nil), "workspace saved")
	binding("ctrl+s")
	expect("configured override", r.Pages["overview"].Frame.State["lastAction"], "editor saved")
	binding("g", "s")
	expect("multi-key binding", r.Pages["overview"].Frame.State["summary"], "2 words")
	binding("g", "h")
	expect("configured greeting sequence", r.Pages["overview"].Frame.State["greeting"], "Hello, Ochazuke!")
	binding("ctrl+t")
	expect("targeted edit", string(r.Inputs["title"].Text), " [reviewed]Release")
	binding("ctrl+r")
	expect("custom review mode", r.Mode(r.Context()), "review")
	binding("s")
	expect("review binding inspection", r.Frames[0].State["inspection"], map[string]any{"page": "overview", "path": []string{"app", "overview", "workspace", "editor"}, "input": "notes", "mode": "review"})
	binding("esc")
	expect("mode restoration", r.Mode(r.Context()), "normal")
	call("move", "input", "", ochazuke.Arguments{"motion": "line-start"})
	call("move", "input", "", ochazuke.Arguments{"motion": "word-forward"})
	expect("editor motion", r.Inputs["notes"].Cursor, 6)
	r.Pages["overview"].Frame.State["selectedItem"] = "release-42"
	call("open", "page", "", ochazuke.Arguments{"id": "paths"})
	call("open", "page", "", ochazuke.Arguments{"id": "overview"})
	expect("page focus restoration", r.Context().InputID, "notes")
	binding("tab")
	expect("configured field cycling", r.Context().InputID, "title")
	binding("tab")
	expect("configured field cycling wrap", r.Context().InputID, "notes")
	expect("page cursor restoration", r.Inputs["notes"].Cursor, 6)
	expect("page state restoration", r.Frames[0].State["selectedItem"], "release-42")
	binding("ctrl+o")
	expect("editable modal", r.Context().InputID, "confirm-note")
	replace("confirm-note", "outer")
	call("move", "input", "", ochazuke.Arguments{"motion": "line-start"})
	call("move", "input", "", ochazuke.Arguments{"motion": "right", "steps": 2})
	binding("ctrl+n")
	expect("nested editable modal", r.Context().InputID, "details-note")
	replace("details-note", "inner")
	expect("nested modal renderer", strings.Contains(plain(m.View().Content), "inner"), true)
	press("esc")
	expect("modal parent focus", r.Context().InputID, "confirm-note")
	expect("modal parent text", string(r.Inputs["confirm-note"].Text), "outer")
	expect("modal parent cursor", r.Inputs["confirm-note"].Cursor, 2)
	press("esc")
	expect("modal page restoration", r.Context().InputID, "notes")
	expect("modal cursor restoration", r.Inputs["notes"].Cursor, 6)
	call("undo", "input", "details-note", nil)
	expect("isolated page history", string(r.Inputs["notes"].Text), "alpha beta")
	call("open", "workspace", "", ochazuke.Arguments{"id": "help"})
	expect("help modal renderer", strings.Contains(plain(m.View().Content), "Workspace help"), true)
	press("esc")
	expect("help modal restores page", r.Context().InputID, "notes")
	press("ctrl+j")
	expect("Ctrl-J next page", r.ActivePage, "paths")
	press("ctrl+k")
	expect("Ctrl-K previous page", r.ActivePage, "overview")
	press("qq:")
	expect("coalesced macro recording", r.Macro.Recording, "q")
	expect("coalesced macro register consumed", r.Macro.Pending, "")
	expect("coalesced colon palette", m.Palette.IsOpen(), true)
	call("close", "palette", "", nil)
	binding("q")
	expect("keyboard macro stop", r.Macro.Recording, "")
	expect("macro records colon only", r.Registers["q"].Keys, []ochazuke.MacroKey{{Key: ":"}})
	binding(":")
	call("select", "palette", "", ochazuke.Arguments{"value": "noun:page"})
	call("search", "palette", "", ochazuke.Arguments{"query": "paths"})
	call("select", "palette", "", ochazuke.Arguments{"value": "instance:paths"})
	expect("noun-first page jump", r.ActivePage, "paths")
	expect("noun-first palette closes", m.Palette.IsOpen(), false)
	binding(":")
	call("select", "palette", "", ochazuke.Arguments{"value": "noun:page"})
	call("select", "palette", "", ochazuke.Arguments{"value": "instance:overview"})
	expect("noun-first return page", r.Context().InputID, "notes")
	binding(":")
	call("select", "palette", "", ochazuke.Arguments{"value": "noun:input"})
	call("select", "palette", "", ochazuke.Arguments{"value": "instance:title"})
	expect("noun-first input focus", r.Context().InputID, "title")
	call("focus", "input", "", ochazuke.Arguments{"id": "notes"})
	call("open", "palette", "", nil)
	call("select", "palette", "", ochazuke.Arguments{"value": "verb:greet"})
	call("select", "palette", "", ochazuke.Arguments{"value": "workspace"})
	call("set", "palette-argument", "", ochazuke.Arguments{"name": "name", "value": "Lin"})
	call("execute", "palette", "", nil)
	expect("palette action", r.Frames[0].State["greeting"], "Hello, Lin!")
	expect("palette restores page", r.Context().InputID, "notes")
	call("set", "appearance", "", ochazuke.Arguments{"theme": "mono", "density": "compact"})
	expect("live appearance", call("get", "appearance", "", nil), ochazuke.Appearance{Theme: "mono", Density: "compact", Dark: true})
	replace("notes", "one two")
	call("move", "input", "", ochazuke.Arguments{"motion": "first-line"})
	call("select", "input", "", ochazuke.Arguments{"shape": "char"})
	call("move", "input", "", ochazuke.Arguments{"motion": "right", "steps": 2})
	call("yank", "input", "", ochazuke.Arguments{"register": "a"})
	expect("visual yank register", r.Registers["a"].Yank.Text, "one")
	replace("title", "")
	call("focus", "input", "", ochazuke.Arguments{"id": "title"})
	call("paste", "input", "", ochazuke.Arguments{"register": "a", "literal": true})
	expect("shared register paste", string(r.Inputs["title"].Text), "one")
	call("undo", "input", "", nil)
	expect("undo", string(r.Inputs["title"].Text), "")
	call("redo", "input", "", nil)
	expect("redo", string(r.Inputs["title"].Text), "one")
	call("open", "yank-history", "", nil)
	call("select", "yank-history", "", ochazuke.Arguments{"index": 0})
	expect("yank history restores input", r.Context().InputID, "title")
	expect("yank history selection", r.Registers["\""].Yank.Text, "one")
	replace("title", "")
	call("start", "macro", "", ochazuke.Arguments{"register": "b"})
	press("i")
	press("Z")
	press("esc")
	call("stop", "macro", "", nil)
	call("replay", "macro", "", ochazuke.Arguments{"register": "b"})
	expect("macro keyboard replay", string(r.Inputs["title"].Text), "ZZ")
	if failure != nil {
		return failure
	}
	root, err := os.MkdirTemp("", "ochazuke-full-project-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "release.txt"), []byte("release\n"), 0600); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(root, "output"), 0700); err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	call("open", "page", "", ochazuke.Arguments{"id": "paths"})
	picker := m.Pickers[0]
	var drain func(tea.Cmd)
	drain = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				drain(child)
			}
			return
		}
		picker.HandleMessage(msg)
		drain(picker.PendingCommand())
	}
	for _, item := range []struct{ input, filter, path string }{{"source", "file", "release.txt"}, {"destination", "dir", "output"}, {"attachments", "any", "release.txt"}} {
		call("focus", "input", "", ochazuke.Arguments{"id": item.input})
		replace(item.input, "")
		call("open", "picker", "", ochazuke.Arguments{"path": root + string(filepath.Separator)})
		expect("picker inferred filter", picker.Filter, item.filter)
		drain(picker.PendingCommand())
		index := -1
		for i, entry := range picker.Entries {
			if entry.Path == item.path {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("smoke picker: missing %q: %s", item.path, picker.Error)
		}
		call("move", "picker", "", ochazuke.Arguments{"delta": index - picker.Selected})
		call("choose", "picker", "", nil)
		expect("picker committed "+item.input, string(r.Inputs[item.input].Text), filepath.Join(root, item.path))
		expect("picker restores input", r.Context().InputID, item.input)
	}
	if failure != nil {
		return failure
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 42})
	view := plain(m.View().Content)
	if !strings.Contains(view, "Paths") || !strings.Contains(view, "release.txt") {
		return fmt.Errorf("smoke rendered page does not show selected paths")
	}
	call("open", "page", "", ochazuke.Arguments{"id": "items"})
	expect("initial list item", r.Context().Path, []string{"app", "items", "item-list", "repo-1"})
	binding("j")
	expect("list item replaces leaf", r.Context().Path, []string{"app", "items", "item-list", "worktree-1"})
	binding("r")
	selected, _ := r.Lists["item-list"].Selected()
	expect("refresh retains item ID", selected.ID, "main")
	binding("tab", "j", "k", "x")
	expect("list releases input focus", r.Context().Path, []string{"app", "items", "item-editor"})
	expect("editor receives list keys", string(r.Inputs["item-note"].Text), "jkx")
	call("move", "list", "", ochazuke.Arguments{"id": "item-list", "delta": 1})
	expect("background move keeps editor", r.Context().InputID, "item-note")
	binding("tab")
	expect("list refocus current item", r.Context().Path, []string{"app", "items", "item-list", "worktree-2"})
	binding("x")
	selected, _ = r.Lists["item-list"].Selected()
	expect("removed row falls back", selected.ID, "release")
	if failure != nil {
		return failure
	}
	return nil
}
