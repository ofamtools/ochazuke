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
		fmt.Fprintln(flags.Output(), "Usage:\n  full-project [--config DIR] [--socket PATH] [run [--headless]]\n  full-project [--config DIR] [--socket PATH] NOUN VERB [--json '{...}'] [--target INSTANCE]\n  full-project [--config DIR] --smoke\n\nNo command opens the TUI. Operations without --socket use a fresh local runtime; with --socket they affect the live app.")
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
		target := callFlags.String("target", "", "Instance ID (input, list, row, page) or focused-input")
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
	if err := reportDeprecations(m.Runtime); err != nil {
		return err
	}
	if *smoke {
		if err := smokeExample(m); err != nil {
			return err
		}
		fmt.Println("full-project smoke: actions, noun and instance rows, review mode, page/modal restoration, picker filters, palette, appearance, editing, history, registers, macros, list focus/refresh, row nouns, confirm, fan-out and the console terminal: PASS")
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
		if r.Input(id) == nil {
			return fmt.Errorf("smoke requires example input %q", id)
		}
	}
	for _, id := range []string{"overview", "paths", "help", "console"} {
		if r.PageOf(id) == nil {
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
			failure = fmt.Errorf("smoke %s.%s: %w", noun, verb, err)
		}
		if smokeStep != nil && failure == nil {
			failure = smokeStep(noun + "." + verb)
		}
		return value
	}
	// pending holds the commands Update returned, as a Bubble Tea program
	// would run them: the pane's frame reads among them. frames runs them and
	// delivers what they produce.
	var pending []tea.Cmd
	update := func(msg tea.Msg) {
		if _, cmd := m.Update(msg); cmd != nil {
			pending = append(pending, cmd)
		}
	}
	frames := func() {
		cmds := append(pending, r.PendingCommands())
		pending = nil
		for _, msg := range runCommands(tea.Batch(cmds...)) {
			update(msg)
		}
	}
	key := func(message tea.KeyPressMsg) {
		if failure != nil {
			return
		}
		update(message)
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
		special := map[string]rune{"esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace, "enter": tea.KeyEnter}
		if code, ok := special[name]; ok {
			msg := tea.KeyPressMsg{Code: code}
			if name == "space" {
				msg.Text = " "
			}
			key(msg)
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
		call("replace", "input", id, ochazuke.Arguments{"start": 0, "end": len(r.Input(id).Text), "text": text})
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
	expect("configured override", pageState("overview")["lastAction"], "editor saved")
	binding("g", "s")
	expect("multi-key binding", pageState("overview")["summary"], "2 words")
	binding("g", "h")
	expect("configured greeting sequence", pageState("overview")["greeting"], "Hello, Ochazuke!")
	binding("ctrl+t")
	expect("targeted edit", string(r.Input("title").Text), " [reviewed]Release")
	binding("ctrl+r")
	expect("custom review mode", r.Mode.Name, "review")
	binding("s")
	expect("review binding inspection", pageState("overview")["inspection"], map[string]any{"page": "overview", "path": []string{"app", "overview", "workspace", "editor"}, "input": "notes", "mode": "review"})
	binding("esc")
	expect("mode restoration", r.Mode.Name, "normal")
	call("move", "input", "", ochazuke.Arguments{"motion": "line-start"})
	call("move", "input", "", ochazuke.Arguments{"motion": "word-forward"})
	expect("editor motion", r.Input("notes").Cursor, 6)
	pageState("overview")["selectedItem"] = "release-42"
	call("open", "page", "", ochazuke.Arguments{"id": "paths"})
	call("open", "page", "", ochazuke.Arguments{"id": "overview"})
	expect("page focus restoration", r.Context().InputID, "notes")
	binding("tab")
	expect("configured field cycling", r.Context().InputID, "title")
	binding("tab")
	expect("configured field cycling wrap", r.Context().InputID, "notes")
	expect("page cursor restoration", r.Input("notes").Cursor, 6)
	expect("page state restoration", pageState("overview")["selectedItem"], "release-42")
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
	expect("modal parent text", string(r.Input("confirm-note").Text), "outer")
	expect("modal parent cursor", r.Input("confirm-note").Cursor, 2)
	press("esc")
	expect("modal page restoration", r.Context().InputID, "notes")
	expect("modal cursor restoration", r.Input("notes").Cursor, 6)
	call("undo", "input", "details-note", nil)
	expect("isolated page history", string(r.Input("notes").Text), "alpha beta")
	call("open", "workspace", "", ochazuke.Arguments{"id": "help"})
	expect("help modal renderer", strings.Contains(plain(m.View().Content), "Workspace help"), true)
	press("esc")
	expect("help modal restores page", r.Context().InputID, "notes")
	press("ctrl+j")
	expect("Ctrl-J next page", r.Page, "paths")
	press("ctrl+k")
	expect("Ctrl-K previous page", r.Page, "overview")
	press("qq:")
	expect("coalesced macro recording", r.Registers().Macro.Recording, "q")
	expect("coalesced colon palette", m.Palette.IsOpen(), true)
	call("close", "palette", "", nil)
	binding("q")
	expect("keyboard macro stop", r.Registers().Macro.Recording, "")
	expect("macro records colon only", r.Registers().Named["q"].Keys, []ochazuke.MacroKey{{Key: ":"}})
	binding(":")
	call("set", "palette", "", ochazuke.Arguments{"text": "page paths"})
	call("run", "palette", "", nil)
	expect("noun-first page jump", r.Page, "paths")
	expect("noun-first palette closes", m.Palette.IsOpen(), false)
	binding(":")
	call("set", "palette", "", ochazuke.Arguments{"text": "page overview"})
	call("run", "palette", "", nil)
	expect("noun-first return page", r.Context().InputID, "notes")
	binding(":")
	call("set", "palette", "", ochazuke.Arguments{"text": "input title"})
	call("run", "palette", "", nil)
	expect("noun-first input focus", r.Context().InputID, "title")
	call("focus", "input", "", ochazuke.Arguments{"id": "notes"})
	call("open", "palette", "", nil)
	call("set", "palette", "", ochazuke.Arguments{"text": "greet workspace Lin"})
	call("run", "palette", "", nil)
	expect("palette action", pageState("overview")["greeting"], "Hello, Lin!")
	expect("palette restores page", r.Context().InputID, "notes")
	call("set", "appearance", "", ochazuke.Arguments{"theme": "mono", "density": "compact"})
	expect("live appearance", call("get", "appearance", "", nil), ochazuke.Appearance{Theme: "mono", Density: "compact", Dark: true})
	replace("notes", "one two")
	call("move", "input", "", ochazuke.Arguments{"motion": "first-line"})
	call("select", "input", "", ochazuke.Arguments{"shape": "char"})
	call("move", "input", "", ochazuke.Arguments{"motion": "right", "steps": 2})
	call("yank", "input", "", ochazuke.Arguments{"register": "a"})
	expect("visual yank register", r.Registers().Named["a"].Yank.Text, "one")
	replace("title", "")
	call("focus", "input", "", ochazuke.Arguments{"id": "title"})
	call("paste", "input", "", ochazuke.Arguments{"register": "a", "literal": true})
	expect("shared register paste", string(r.Input("title").Text), "one")
	call("undo", "input", "", nil)
	expect("undo", string(r.Input("title").Text), "")
	call("redo", "input", "", nil)
	expect("redo", string(r.Input("title").Text), "one")
	call("open", "yank-history", "", nil)
	call("select", "yank-history", "", ochazuke.Arguments{"index": 0})
	expect("yank history restores input", r.Context().InputID, "title")
	expect("yank history selection", r.Registers().Named["\""].Yank.Text, "one")
	replace("title", "")
	call("start", "macro", "", ochazuke.Arguments{"register": "b"})
	press("i")
	press("Z")
	press("esc")
	call("stop", "macro", "", nil)
	call("replay", "macro", "", ochazuke.Arguments{"register": "b"})
	expect("macro keyboard replay", string(r.Input("title").Text), "ZZ")
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
	picker := r.Instances["picker"].Component.(*ochazuke.Picker)
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
		r.Deliver(msg)
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
		expect("picker committed "+item.input, string(r.Input(item.input).Text), filepath.Join(root, item.path))
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
	expect("initial list row", r.Context().Path, []string{"app", "items", "item-list", "repository"})
	expect("row is an instance of its noun", r.Instances["repository"].Noun, "repo")
	binding("j")
	expect("list row replaces leaf", r.Context().Path, []string{"app", "items", "item-list", "main"})
	binding("r")
	selected, _ := r.List("item-list").Selected()
	expect("refresh retains item ID", selected.ID, "main")
	binding("tab", "j", "k", "x")
	expect("list releases input focus", r.Context().Path, []string{"app", "items", "item-editor"})
	expect("editor receives list keys", string(r.Input("item-note").Text), "jkx")
	call("move", "list", "", ochazuke.Arguments{"id": "item-list", "delta": 1})
	expect("background move keeps editor", r.Context().InputID, "item-note")
	binding("tab")
	expect("list refocus current row", r.Context().Path, []string{"app", "items", "item-list", "picker-lab"})
	binding("x")
	selected, _ = r.List("item-list").Selected()
	expect("removed row falls back", selected.ID, "release")
	expect("removed row instance dropped", r.Instances["picker-lab"] == nil, true)
	// Mixed nouns in one list: each row answers to its noun's verbs and keys.
	binding("o")
	expect("worktree verb by key", pageState("items")["lastAction"], "open release")
	call("select", "list", "", ochazuke.Arguments{"item": "repository"})
	binding("f")
	expect("repo verb by key", pageState("items")["lastAction"], "fetch repository")
	call("select", "list", "", ochazuke.Arguments{"item": "hotfix"})
	binding("c")
	expect("branch verb by key", pageState("items")["lastAction"], "checkout hotfix")
	if _, err := r.Execute(ochazuke.Request{Verb: "open", Noun: "worktree"}); err == nil {
		failure = fmt.Errorf("smoke noun guard: worktree.open accepted a branch row")
	}
	expect("row verb by explicit target", call("open", "worktree", "main", nil), map[string]any{"verb": "open", "noun": "worktree", "row": "main"})
	keysFor := func(target string) []string {
		var keys []string
		rows, _ := call("list", "bindings", target, nil).([]ochazuke.Binding)
		for _, b := range rows {
			if b.Request.Noun == "repo" || b.Request.Noun == "worktree" || b.Request.Noun == "branch" {
				keys = append(keys, b.Keys+"="+b.Request.Key())
			}
		}
		return keys
	}
	expect("bindings of a worktree row", keysFor("main"), []string{"d=worktree.remove", "enter=worktree.open", "o=worktree.open"})
	expect("bindings of a branch row", keysFor("hotfix"), []string{"c=branch.checkout", "enter=branch.checkout"})
	expect("bindings of the repo row", keysFor("repository"), []string{"f=repo.fetch"})
	binding("?")
	expect("help names the focused noun", strings.Contains(plain(m.View().Content), "noun: branch"), true)
	expect("help lists the row's verbs", strings.Contains(plain(m.View().Content), "branch.checkout"), true)
	press("esc")
	call("select", "context", "", ochazuke.Arguments{"instance": "main"})
	expect("context.select by instance", r.Context().Path, []string{"app", "items", "item-list", "main"})
	binding("d")
	expect("worktree.remove asks first", r.TopModal(), "confirm")
	expect("confirm names the row", strings.Contains(plain(m.View().Content), "Remove worktree main?"), true)
	binding("n")
	expect("n keeps the row", r.Instances["main"] != nil && len(r.Modals()) == 0, true)
	binding("d", "y")
	expect("worktree.remove drops the row after y", r.Instances["main"] == nil, true)
	expect("confirm closed", len(r.Modals()), 0)
	binding(":")
	call("set", "palette", "", ochazuke.Arguments{"text": "worktree docs"})
	call("run", "palette", "", nil)
	expect("noun-first row action", pageState("items")["lastAction"], "open docs")
	// Fan-out: mark two rows of different nouns and apply one verb; the row
	// whose noun lacks the verb is reported, not run.
	call("select", "list", "", ochazuke.Arguments{"item": "docs"})
	binding("space", "j", "j", "space")
	report, _ := call("apply", "list", "", ochazuke.Arguments{"verb": "open"}).(map[string]any)
	expect("fan-out applied", report["applied"], []string{"docs", "maintenance"})
	call("select", "list", "", ochazuke.Arguments{"item": "hotfix"})
	binding("space")
	report, _ = call("apply", "list", "", ochazuke.Arguments{"verb": "open"}).(map[string]any)
	expect("fan-out skips unsupported nouns", report["skipped"], []string{"hotfix: branch.open is not registered"})
	expect("fan-out keeps marks", len(r.List("item-list").Marked()), 3)
	if failure != nil {
		return failure
	}
	// The console page: a terminal instance under the page, fed by the
	// example's echo stream, driven only through the public API and rows.
	call("open", "page", "", ochazuke.Arguments{"id": "console"})
	expect("console starts on its pane", r.Focus, "pane")
	expect("pane keys are terminal mode", r.Mode.Name, "terminal")
	term := r.Terminal("pane")
	frames()
	expect("first frame delivered", len(term.Frame().Lines) > 0, true)
	binding("l", "s", "enter")
	frames()
	paneText := func() string { return strings.Join(term.Frame().Lines, "\n") }
	expect("pane echoes what was sent", strings.Contains(paneText(), "echo: ls"), true)
	binding("ctrl+a", "x")
	frames()
	expect("unbound leader key forwards leader and key", strings.HasSuffix(paneText(), "$ <ctrl+a>x"), true)
	expect("back in terminal mode", r.Mode.Name, "terminal")
	binding("ctrl+a", ":")
	expect("leader-bound key opens the palette", m.Palette.IsOpen(), true)
	call("close", "palette", "", nil)
	expect("palette returns to the pane", r.Focus, "pane")
	terminalKeys := func() []string {
		var keys []string
		rows, _ := call("list", "bindings", "pane", nil).([]ochazuke.Binding)
		for _, b := range rows {
			keys = append(keys, b.Keys+b.Pattern+"="+b.Request.Verb)
		}
		return keys
	}
	expect("terminal mode lists the app capture row and the terminal table", terminalKeys(), []string{"ctrl+a=enter", "ctrl+c=quit", "any=send", "paste=paste"})
	binding("ctrl+a", "d")
	expect("leader d detaches to the page", r.Context().Path, []string{"app", "console"})
	binding("x")
	expect("detached pane takes no keys", strings.HasSuffix(paneText(), "$ <ctrl+a>x"), true)
	binding("enter")
	expect("enter re-attaches", r.Focus, "pane")
	update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// Rendering sizes the pane to its panel, which the stream answers with a
	// frame; a read with nothing changed would wait, as it does in a program.
	m.View()
	frames()
	rendered := m.View()
	expect("pane renders inside the page", strings.Contains(plain(rendered.Content), "echo: ls"), true)
	expect("pane cursor is the real cursor", rendered.Cursor != nil && rendered.Cursor.Shape == tea.CursorBar, true)
	if failure != nil {
		return failure
	}
	return nil
}

// runCommands executes a command tree synchronously and returns its messages.
func runCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, runCommands(child)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// reportDeprecations prints, once at startup, every binding row that still
// names its operation by the pre-noun.verb spelling; the row was rewritten.
func reportDeprecations(r *ochazuke.Runtime) error {
	warnings, err := r.ValidateBindings()
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "full-project: deprecated:", warning)
	}
	return nil
}
