package tutorial

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
)

// TapeOptions configures Charm VHS .tape generation.
type TapeOptions struct {
	OutputMP4   string
	OutputGIF   string
	FontSize    int
	TypingSpeed string
	Theme       string
	WidthPx     int
	HeightPx    int
	Shell       string
	Padding     int
	LaunchMs    int
}

// BundleOptions configures WriteBundle output generation.
type BundleOptions struct {
	HTML HTMLOptions
	Tape TapeOptions
}

// BundlePaths lists all artifact files written by WriteBundle.
type BundlePaths struct {
	Dir      string `json:"dir"`
	HTML     string `json:"html"`
	Tape     string `json:"tape"`
	SRT      string `json:"srt"`
	DataJSON string `json:"dataJson"`
}

// RenderTape emits a deterministic Charm VHS .tape script for spec using
// bindings to resolve step command keys.
func RenderTape(spec Spec, bindings []core.Binding, opts TapeOptions) string {
	var b strings.Builder
	b.Grow(4096)

	toolSlug := sanitizeSlug(spec.Tool)
	if opts.OutputMP4 != "" {
		fmt.Fprintf(&b, "Output %s\n", opts.OutputMP4)
	}
	if opts.OutputGIF != "" {
		fmt.Fprintf(&b, "Output %s\n", opts.OutputGIF)
	}
	if opts.OutputMP4 == "" && opts.OutputGIF == "" {
		fmt.Fprintf(&b, "Output %s.mp4\n", toolSlug)
	}

	shell := opts.Shell
	if shell == "" {
		shell = "bash"
	}
	widthPx := opts.WidthPx
	if widthPx <= 0 {
		widthPx = 1280
	}
	heightPx := opts.HeightPx
	if heightPx <= 0 {
		heightPx = 720
	}
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = 16
	}
	padding := opts.Padding
	if padding <= 0 {
		padding = 16
	}
	typingSpeed := opts.TypingSpeed
	if typingSpeed == "" {
		typingSpeed = "50ms"
	}
	theme := opts.Theme
	if theme == "" {
		theme = spec.Theme
	}
	launchMs := opts.LaunchMs
	if launchMs <= 0 {
		launchMs = 1500
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Set Shell %q\n", shell)
	fmt.Fprintf(&b, "Set Width %d\n", widthPx)
	fmt.Fprintf(&b, "Set Height %d\n", heightPx)
	fmt.Fprintf(&b, "Set FontSize %d\n", fontSize)
	fmt.Fprintf(&b, "Set Padding %d\n", padding)
	fmt.Fprintf(&b, "Set TypingSpeed %s\n", typingSpeed)
	if theme != "" {
		fmt.Fprintf(&b, "Set Theme %q\n", theme)
	}
	b.WriteString("\n")

	if spec.Command != "" {
		b.WriteString("Hide\n")
		if spec.WorkDir != "" {
			fmt.Fprintf(&b, "%s\n", vhsTypeLine("cd "+shellQuote(spec.WorkDir)))
			b.WriteString("Enter\n")
		}
		fmt.Fprintf(&b, "%s\n", vhsTypeLine(spec.Command))
		b.WriteString("Enter\n")
		fmt.Fprintf(&b, "Sleep %dms\n", launchMs)
		b.WriteString("Show\n\n")
	}

	for _, sec := range spec.Sections {
		fmt.Fprintf(&b, "# Section: %s\n", sec.Title)
		for _, step := range sec.Steps {
			if step.Caption != "" {
				fmt.Fprintf(&b, "# %s\n", step.Caption)
			}
			keys, _ := ResolveKeys(step, bindings)
			for _, k := range keys {
				if line := mapKeyToVHS(k); line != "" {
					b.WriteString(line)
					b.WriteByte('\n')
				}
			}
			fmt.Fprintf(&b, "Sleep %dms\n\n", effectivePauseMs(step.PauseMs))
		}
	}

	return b.String()
}

// mapKeyToVHS translates an Ochazuke key name or printable text string into a
// valid Charm VHS instruction line.
func mapKeyToVHS(key string) string {
	if key == "" {
		return ""
	}
	if key == " " {
		return "Space"
	}
	lower := strings.ToLower(key)
	if special, ok := vhsSpecialKey(lower); ok {
		return special
	}
	if strings.Contains(lower, "+") && len(key) > 1 {
		if chord, ok := vhsModifierChord(key); ok {
			return chord
		}
	}
	return vhsTypeLine(key)
}

func vhsSpecialKey(lower string) (string, bool) {
	switch lower {
	case "enter", "return":
		return "Enter", true
	case "esc", "escape":
		return "Escape", true
	case "tab":
		return "Tab", true
	case "backtab":
		return "Shift+Tab", true
	case "backspace":
		return "Backspace", true
	case "delete", "del":
		return "Delete", true
	case "space":
		return "Space", true
	case "up":
		return "Up", true
	case "down":
		return "Down", true
	case "left":
		return "Left", true
	case "right":
		return "Right", true
	case "home":
		return "Home", true
	case "end":
		return "End", true
	case "pgup", "pageup":
		return "PageUp", true
	case "pgdown", "pgdn", "pagedown":
		return "PageDown", true
	}
	return "", false
}

func vhsModifierChord(key string) (string, bool) {
	parts := strings.Split(key, "+")
	if len(parts) < 2 {
		return "", false
	}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mods = append(mods, "Ctrl")
		case "alt", "opt", "option":
			mods = append(mods, "Alt")
		case "shift":
			mods = append(mods, "Shift")
		default:
			return "", false
		}
	}
	last := parts[len(parts)-1]
	if last == "" {
		return "", false
	}
	if special, ok := vhsSpecialKey(strings.ToLower(last)); ok {
		last = special
	} else {
		r, size := utf8.DecodeRuneInString(last)
		if size == len(last) && unicode.IsLetter(r) {
			last = strings.ToUpper(last)
		}
	}
	return strings.Join(append(mods, last), "+"), true
}

func vhsTypeLine(text string) string {
	switch {
	case !strings.ContainsRune(text, '"'):
		return `Type "` + text + `"`
	case !strings.ContainsRune(text, '\''):
		return `Type '` + text + `'`
	case !strings.ContainsRune(text, '`'):
		return "Type `" + text + "`"
	default:
		escaped := strings.ReplaceAll(text, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `Type "` + escaped + `"`
	}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RenderSRT generates deterministic timed SubRip (.srt) subtitles from every
// step's Caption and PauseMs.
func RenderSRT(spec Spec) string {
	var b strings.Builder
	b.Grow(1024)

	cue := 0
	cursorMs := 0
	for _, sec := range spec.Sections {
		for _, step := range sec.Steps {
			dur := effectivePauseMs(step.PauseMs)
			start := cursorMs
			end := cursorMs + dur
			cursorMs = end

			caption := strings.TrimSpace(step.Caption)
			if caption == "" {
				continue
			}
			cue++
			fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
				cue, formatSRTTime(start), formatSRTTime(end), caption)
		}
	}
	return b.String()
}

func formatSRTTime(ms int) string {
	if ms < 0 {
		ms = 0
	}
	hours := ms / 3600000
	ms %= 3600000
	minutes := ms / 60000
	ms %= 60000
	seconds := ms / 1000
	millis := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millis)
}

// WriteBundle writes the full tutorial bundle (index.html, <tool>.tape,
// <tool>.srt, and tutorial-data.json) into outDir.
func WriteBundle(outDir string, res Result, bindings []core.Binding, opts BundleOptions) (BundlePaths, error) {
	if strings.TrimSpace(outDir) == "" {
		return BundlePaths{}, fmt.Errorf("write tutorial bundle: outDir is empty")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return BundlePaths{}, fmt.Errorf("create tutorial bundle dir %q: %w", outDir, err)
	}

	slug := sanitizeSlug(res.Spec.Tool)
	paths := BundlePaths{
		Dir:      outDir,
		HTML:     filepath.Join(outDir, "index.html"),
		Tape:     filepath.Join(outDir, slug+".tape"),
		SRT:      filepath.Join(outDir, slug+".srt"),
		DataJSON: filepath.Join(outDir, "tutorial-data.json"),
	}

	if err := os.WriteFile(paths.HTML, []byte(RenderHTML(res, opts.HTML)), 0o644); err != nil {
		return BundlePaths{}, fmt.Errorf("write %s: %w", paths.HTML, err)
	}
	if err := os.WriteFile(paths.Tape, []byte(RenderTape(res.Spec, bindings, opts.Tape)), 0o644); err != nil {
		return BundlePaths{}, fmt.Errorf("write %s: %w", paths.Tape, err)
	}
	if err := os.WriteFile(paths.SRT, []byte(RenderSRT(res.Spec)), 0o644); err != nil {
		return BundlePaths{}, fmt.Errorf("write %s: %w", paths.SRT, err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return BundlePaths{}, fmt.Errorf("marshal tutorial data: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(paths.DataJSON, data, 0o644); err != nil {
		return BundlePaths{}, fmt.Errorf("write %s: %w", paths.DataJSON, err)
	}

	return paths, nil
}

func sanitizeSlug(tool string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(tool)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '.':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		return "tutorial"
	}
	return slug
}
