package tutorial

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
)

func boolPtr(v bool) *bool { return &v }

func TestParseAndLoadSpec(t *testing.T) {
	raw := []byte(`{
		"tool": "mtm",
		"title": "Matomeru Quickstart",
		"description": "Learn to filter tasks and create cards.",
		"command": "mtm --demo",
		"sections": [
			{
				"id": "filter",
				"title": "Filter Tasks",
				"goal": "Narrow the list to open items.",
				"steps": [
					{
						"caption": "Filter for open tasks",
						"command": "filter.open",
						"keys": ["status=open", "enter"],
						"expect": ["status=open"],
						"pauseMs": 900
					}
				]
			}
		]
	}`)

	spec, err := ParseSpec(raw)
	if err != nil {
		t.Fatalf("ParseSpec failed: %v", err)
	}
	if spec.Width != DefaultWidth || spec.Height != DefaultHeight {
		t.Fatalf("defaults = %dx%d, want %dx%d", spec.Width, spec.Height, DefaultWidth, DefaultHeight)
	}
	if !spec.Sections[0].Steps[0].ShouldCapture() {
		t.Fatalf("nil Capture should default to true")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec failed: %v", err)
	}
	if loaded.Tool != "mtm" || loaded.Title != "Matomeru Quickstart" || len(loaded.Sections) != 1 {
		t.Fatalf("unexpected loaded spec: %+v", loaded)
	}

	invalid := []struct {
		name string
		json string
	}{
		{"empty", ""},
		{"missing tool", `{"title":"T","sections":[{"id":"a","title":"A","steps":[{"caption":"c"}]}]}`},
		{"missing title", `{"tool":"x","sections":[{"id":"a","title":"A","steps":[{"caption":"c"}]}]}`},
		{"no sections", `{"tool":"x","title":"T","sections":[]}`},
		{"duplicate section id", `{"tool":"x","title":"T","sections":[{"id":"a","title":"A","steps":[{"caption":"1"}]},{"id":"a","title":"B","steps":[{"caption":"2"}]}]}`},
		{"empty steps", `{"tool":"x","title":"T","sections":[{"id":"a","title":"A","steps":[]}]}`},
		{"negative pause", `{"tool":"x","title":"T","sections":[{"id":"a","title":"A","steps":[{"caption":"1","pauseMs":-5}]}]}`},
	}
	for _, tc := range invalid {
		if _, err := ParseSpec([]byte(tc.json)); err == nil {
			t.Errorf("ParseSpec(%s): expected error, got nil", tc.name)
		}
	}
}

func TestResolveKeys(t *testing.T) {
	bindings := []core.Binding{
		{Mode: "visual", Keys: []string{"v", "f"}, Command: "filter.open"},
		{Mode: "normal", Keys: []string{"/"}, Command: "filter.open"},
		{Mode: "normal", Keys: []string{"x"}, Command: "filter.open", Unbind: true},
		{Mode: "normal", Keys: []string{"C"}, Command: "card.create"},
		{Mode: "normal", Keys: []string{"g", "g"}, Command: "motion.top"},
		{Mode: "leader", Keys: []string{"ctrl+a", "w"}, Command: "pane.switch"},
		{Mode: "normal", Class: core.ClassPrintable, Command: "text.insert"},
	}

	// Prefers "normal" when step.Mode is empty, prepending before explicit step.Keys.
	keys, badge := ResolveKeys(Step{
		Command: "filter.open",
		Keys:    []string{"status=open", "enter"},
	}, bindings)
	wantKeys := []string{"/", "status=open", "enter"}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
	if badge != "/ status=open Enter" {
		t.Fatalf("badge = %q, want %q", badge, "/ status=open Enter")
	}

	// Prefers explicit step.Mode when set.
	keys, badge = ResolveKeys(Step{
		Command: "filter.open",
		Mode:    "visual",
	}, bindings)
	if !slices.Equal(keys, []string{"v", "f"}) || badge != "v f" {
		t.Fatalf("visual mode resolve = (%v, %q), want ([v f], \"v f\")", keys, badge)
	}

	// Single uppercase key badge.
	keys, badge = ResolveKeys(Step{Command: "card.create"}, bindings)
	if !slices.Equal(keys, []string{"C"}) || badge != "C" {
		t.Fatalf("card.create = (%v, %q), want ([C], \"C\")", keys, badge)
	}

	// Falls back to first match when neither step.Mode nor "normal" matches.
	keys, badge = ResolveKeys(Step{Command: "pane.switch"}, bindings)
	if !slices.Equal(keys, []string{"ctrl+a", "w"}) || badge != "Ctrl+A w" {
		t.Fatalf("pane.switch = (%v, %q), want ([ctrl+a w], \"Ctrl+A w\")", keys, badge)
	}

	// Class-only binding has no explicit keys; falls back to step.Keys only.
	keys, badge = ResolveKeys(Step{Command: "text.insert", Keys: []string{"hello", "esc"}}, bindings)
	if !slices.Equal(keys, []string{"hello", "esc"}) || badge != "hello Esc" {
		t.Fatalf("text.insert = (%v, %q), want ([hello esc], \"hello Esc\")", keys, badge)
	}
}

type fakeDriver struct {
	sent    []string
	ansi    string
	plain   string
	failKey string
}

func (d *fakeDriver) SendKey(key string) error {
	if d.failKey != "" && key == d.failKey {
		return errors.New("boom")
	}
	d.sent = append(d.sent, key)
	switch key {
	case "/":
		d.plain = "Filter: "
		d.ansi = "\x1b[36mFilter:\x1b[0m "
	case "status=open":
		d.plain = "Filter: status=open"
		d.ansi = "\x1b[36mFilter:\x1b[0m \x1b[1mstatus=open\x1b[0m"
	case "enter":
		d.plain = "Tasks [status=open]: 2 items"
		d.ansi = "\x1b[32mTasks [status=open]: 2 items\x1b[0m"
	case "esc":
		d.plain = "Tasks [all]: 5 items"
		d.ansi = "\x1b[37mTasks [all]: 5 items\x1b[0m"
	}
	return nil
}

func (d *fakeDriver) RenderANSI() string  { return d.ansi }
func (d *fakeDriver) RenderPlain() string { return d.plain }

func TestRunHappyPathAndAssertionFailure(t *testing.T) {
	bindings := []core.Binding{
		{Mode: "normal", Keys: []string{"/"}, Command: "filter.open"},
	}
	spec := Spec{
		Tool:  "mtm",
		Title: "Demo",
		Sections: []Section{
			{
				ID:    "filter",
				Title: "Filter",
				Goal:  "Filter open items.",
				Steps: []Step{
					{
						Caption: "Apply open filter",
						Command: "filter.open",
						Keys:    []string{"status=open", "enter"},
						Expect:  []string{"status=open", "2 items"},
						PauseMs: 800,
					},
					{
						Caption: "Reset filter silently",
						Keys:    []string{"esc"},
						Expect:  []string{"5 items"},
						Capture: boolPtr(false),
					},
				},
			},
		},
	}

	drv := &fakeDriver{ansi: "\x1b[1mWelcome\x1b[0m", plain: "Welcome"}
	res, err := Run(spec, drv, bindings)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.InitialPlain != "Welcome" || res.InitialANSI != "\x1b[1mWelcome\x1b[0m" {
		t.Fatalf("initial frame = (%q, %q)", res.InitialPlain, res.InitialANSI)
	}
	wantSent := []string{"/", "status=open", "enter", "esc"}
	if !slices.Equal(drv.sent, wantSent) {
		t.Fatalf("sent keys = %v, want %v", drv.sent, wantSent)
	}
	if len(res.Sections) != 1 || len(res.Sections[0].Steps) != 2 {
		t.Fatalf("unexpected captured sections: %+v", res.Sections)
	}
	s0 := res.Sections[0].Steps[0]
	if s0.Index != 1 || s0.KeyBadge != "/ status=open Enter" || !s0.Captured {
		t.Fatalf("step 0 mismatch: %+v", s0)
	}
	s1 := res.Sections[0].Steps[1]
	if s1.Index != 2 || s1.Captured {
		t.Fatalf("step 1 should have Index=2 and Captured=false: %+v", s1)
	}

	// Failing Expect assertion names section, step, caption, and missing substring.
	badSpec := spec
	badSpec.Sections = []Section{
		{
			ID:    "broken",
			Title: "Broken",
			Steps: []Step{
				{
					Caption: "Expect missing banner",
					Keys:    []string{"esc"},
					Expect:  []string{"DOES_NOT_EXIST"},
				},
			},
		},
	}
	_, err = Run(badSpec, &fakeDriver{}, bindings)
	if err == nil {
		t.Fatal("expected assertion failure error, got nil")
	}
	for _, want := range []string{"broken", "step 1", "Expect missing banner", "DOES_NOT_EXIST"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}

	// Nil driver error.
	if _, err := Run(spec, nil, bindings); err == nil {
		t.Fatal("expected nil driver error")
	}

	// SendKey error propagation.
	if _, err := Run(spec, &fakeDriver{failKey: "enter"}, bindings); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected propagated SendKey error, got %v", err)
	}
}

func TestANSIToHTML(t *testing.T) {
	input := strings.Join([]string{
		"\x1b[2J\x1b[H\x1b[?25l",                        // non-SGR CSI stripped
		"\x1b]0;Window Title\x07",                       // OSC BEL stripped
		"\x1b]8;;https://example.com\x1b\\",             // OSC ST stripped
		"\x1b[31;42mred-on-green\x1b[0m ",               // standard 16-color
		"\x1b[94;100mbright\x1b[39;49m ",                // bright colors + partial fg/bg reset
		"\x1b[38;5;208;48;5;236m256c\x1b[0m ",           // 256-color cube + grayscale
		"\x1b[38;2;18;52;86;48;2;255;0;128mrgb\x1b[0m ", // 24-bit RGB
		"\x1b[1;2;3;4mstyled\x1b[22;23;24mplain ",       // bold+dim+italic+underline + partial resets
		"\x1b[7mrev\x1b[27m ",                           // reverse + partial reset
		"<script>&\"test\"</script>",                    // HTML escaping
	}, "")

	got := ANSIToHTML(input)
	if !strings.HasPrefix(got, `<pre class="term">`) || !strings.HasSuffix(got, `</pre>`) {
		t.Fatalf("missing <pre class=\"term\"> wrapper: %s", got)
	}

	checks := []string{
		`color:#f85149;background-color:#3fb950">red-on-green</span>`,
		`color:#79c0ff;background-color:#6e7681">bright</span> `,
		`color:#ff8700;background-color:#303030">256c</span>`,
		`color:#123456;background-color:#ff0080">rgb</span>`,
		`font-weight:700;opacity:0.65;font-style:italic;text-decoration:underline">styled</span>plain `,
		`color:#0d1117;background-color:#e6edf3">rev</span> `,
		`&lt;script&gt;&amp;&#34;test&#34;&lt;/script&gt;`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("ANSIToHTML missing %q\ngot: %s", want, got)
		}
	}

	// Ensure non-SGR escapes were cleanly stripped.
	for _, forbidden := range []string{"Window Title", "https://example.com", "\x1b", "<script>"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("ANSIToHTML unexpectedly contains %q", forbidden)
		}
	}

	if plain := StripANSI("\x1b[31mhi\x1b[0m\x1b]0;t\x07!"); plain != "hi!" {
		t.Fatalf("StripANSI = %q, want %q", plain, "hi!")
	}
}

func TestRenderHTML(t *testing.T) {
	res := Result{
		Spec: Spec{
			Tool:        "mtm",
			Title:       "Matomeru <Guide>",
			Description: "Interactive workspace walkthrough.",
			Command:     "mtm --demo",
			Width:       120,
			Height:      36,
			Theme:       "dark",
		},
		Sections: []CapturedSection{
			{
				ID:    "getting-started",
				Title: "Getting Started",
				Goal:  "Navigate panes and filter tasks.",
				Steps: []CapturedStep{
					{
						SectionID: "getting-started",
						Index:     1,
						Caption:   "Open filter bar",
						Command:   "filter.open",
						Keys:      []string{"/", "status=open", "enter"},
						KeyBadge:  "/ status=open Enter",
						ANSI:      "\x1b[32mFiltered: status=open\x1b[0m",
						Plain:     "Filtered: status=open",
						Captured:  true,
					},
					{
						SectionID: "getting-started",
						Index:     2,
						Caption:   "Hidden setup step",
						Keys:      []string{"esc"},
						ANSI:      "hidden frame",
						Plain:     "hidden frame",
						Captured:  false,
					},
				},
			},
		},
	}

	withVideo := RenderHTML(res, HTMLOptions{VideoPath: "mtm.mp4", IncludeTOC: true})
	for _, want := range []string{
		"<!DOCTYPE html>",
		"Matomeru &lt;Guide&gt;",
		"mtm --demo",
		`<video controls playsinline preload="metadata">`,
		`<source src="mtm.mp4" type="video/mp4">`,
		`<nav class="toc"`,
		`href="#getting-started"`,
		"Navigate panes and filter tasks.",
		`<kbd>/</kbd><kbd>status=open</kbd><kbd>Enter</kbd>`,
		`<pre class="term"><span style="color:#3fb950">Filtered: status=open</span></pre>`,
	} {
		if !strings.Contains(withVideo, want) {
			t.Errorf("RenderHTML(withVideo) missing %q", want)
		}
	}
	if strings.Contains(withVideo, "Hidden setup step") || strings.Contains(withVideo, "hidden frame") {
		t.Errorf("RenderHTML should omit uncaptured steps")
	}
	if strings.Contains(withVideo, "http://") || strings.Contains(withVideo, "https://") {
		t.Errorf("RenderHTML must not reference external CDN URLs")
	}

	withoutVideo := RenderHTML(res, HTMLOptions{VideoPath: "", IncludeTOC: false})
	if strings.Contains(withoutVideo, "<video") {
		t.Errorf("RenderHTML(withoutVideo) should not contain <video>")
	}
	if strings.Contains(withoutVideo, `<nav class="toc"`) {
		t.Errorf("RenderHTML(IncludeTOC:false) should not contain TOC")
	}
}

func TestRenderTapeAndSRT(t *testing.T) {
	bindings := []core.Binding{
		{Mode: "normal", Keys: []string{"/"}, Command: "filter.open"},
	}
	spec := Spec{
		Tool:    "mtm",
		Title:   "Matomeru Tape",
		Command: "mtm --demo",
		WorkDir: "/tmp/demo workspace",
		Theme:   "Catppuccin Mocha",
		Sections: []Section{
			{
				ID:    "keys",
				Title: "Key Mapping",
				Steps: []Step{
					{
						Caption: "Filter and confirm",
						Command: "filter.open",
						Keys:    []string{"status=open", "enter"},
						PauseMs: 750,
					},
					{
						Caption: "Test navigation and modifiers",
						Keys: []string{
							"esc", "tab", "shift+tab", "backspace", "space",
							"up", "down", "left", "right", "ctrl+x", "ctrl+a",
							`say "hi"`,
						},
						PauseMs: 1500,
					},
					{
						Caption: "",
						Keys:    []string{"q"},
						PauseMs: 500,
					},
					{
						Caption: "Done",
						PauseMs: 0, // uses DefaultPauseMs
					},
				},
			},
		},
	}

	tape := RenderTape(spec, bindings, TapeOptions{
		OutputMP4:   "demo.mp4",
		OutputGIF:   "demo.gif",
		FontSize:    18,
		TypingSpeed: "40ms",
		WidthPx:     1440,
		HeightPx:    900,
	})

	wantTapeLines := []string{
		"Output demo.mp4",
		"Output demo.gif",
		`Set Shell "bash"`,
		"Set Width 1440",
		"Set Height 900",
		"Set FontSize 18",
		"Set Padding 16",
		"Set TypingSpeed 40ms",
		`Set Theme "Catppuccin Mocha"`,
		"Hide",
		`Type "cd '/tmp/demo workspace'"`,
		`Type "mtm --demo"`,
		"Show",
		`Type "/"`,
		`Type "status=open"`,
		"Enter",
		"Sleep 750ms",
		"Escape",
		"Tab",
		"Shift+Tab",
		"Backspace",
		"Space",
		"Up",
		"Down",
		"Left",
		"Right",
		"Ctrl+X",
		"Ctrl+A",
		`Type 'say "hi"'`,
		"Sleep 1500ms",
		"Sleep 1200ms",
	}
	for _, want := range wantTapeLines {
		if !strings.Contains(tape, want) {
			t.Errorf("RenderTape missing %q\ntape:\n%s", want, tape)
		}
	}

	// Verify Hide precedes command launch and Show follows it.
	hideIdx := strings.Index(tape, "Hide\n")
	cmdIdx := strings.Index(tape, `Type "mtm --demo"`)
	showIdx := strings.Index(tape, "Show\n")
	if !(hideIdx >= 0 && hideIdx < cmdIdx && cmdIdx < showIdx) {
		t.Errorf("expected Hide < command < Show, got indices %d, %d, %d", hideIdx, cmdIdx, showIdx)
	}

	srt := RenderSRT(spec)
	wantSRT := strings.Join([]string{
		"1",
		"00:00:00,000 --> 00:00:00,750",
		"Filter and confirm",
		"",
		"2",
		"00:00:00,750 --> 00:00:02,250",
		"Test navigation and modifiers",
		"",
		"3",
		"00:00:02,750 --> 00:00:03,950",
		"Done",
		"",
		"",
	}, "\n")
	if srt != wantSRT {
		t.Fatalf("RenderSRT mismatch:\ngot:\n%s\nwant:\n%s", srt, wantSRT)
	}
}

func TestWriteBundle(t *testing.T) {
	bindings := []core.Binding{
		{Mode: "normal", Keys: []string{"/"}, Command: "filter.open"},
	}
	spec := Spec{
		Tool:    "mtm",
		Title:   "Matomeru Bundle Test",
		Command: "mtm --demo",
		Sections: []Section{
			{
				ID:    "intro",
				Title: "Intro",
				Goal:  "Filter open tasks.",
				Steps: []Step{
					{
						Caption: "Open filter",
						Command: "filter.open",
						Keys:    []string{"status=open", "enter"},
						Expect:  []string{"2 items"},
						PauseMs: 1000,
					},
				},
			},
		},
	}

	res, err := Run(spec, &fakeDriver{}, bindings)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	outDir := filepath.Join(t.TempDir(), "bundle")
	paths, err := WriteBundle(outDir, res, bindings, BundleOptions{
		HTML: HTMLOptions{VideoPath: "mtm.mp4", IncludeTOC: true},
		Tape: TapeOptions{OutputMP4: "mtm.mp4"},
	})
	if err != nil {
		t.Fatalf("WriteBundle failed: %v", err)
	}

	if filepath.Base(paths.HTML) != "index.html" ||
		filepath.Base(paths.Tape) != "mtm.tape" ||
		filepath.Base(paths.SRT) != "mtm.srt" ||
		filepath.Base(paths.DataJSON) != "tutorial-data.json" {
		t.Fatalf("unexpected bundle paths: %+v", paths)
	}

	for _, p := range []string{paths.HTML, paths.Tape, paths.SRT, paths.DataJSON} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected %s to be non-empty", p)
		}
	}

	rawJSON, err := os.ReadFile(paths.DataJSON)
	if err != nil {
		t.Fatalf("read tutorial-data.json: %v", err)
	}
	var roundTrip Result
	if err := json.Unmarshal(rawJSON, &roundTrip); err != nil {
		t.Fatalf("unmarshal tutorial-data.json: %v", err)
	}
	if roundTrip.Spec.Tool != "mtm" || len(roundTrip.Sections) != 1 || roundTrip.Sections[0].Steps[0].KeyBadge != "/ status=open Enter" {
		t.Fatalf("unexpected round-trip Result: %+v", roundTrip)
	}
}
