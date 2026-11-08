package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// registersID is the single registers instance under app.
const registersID = "__registers"

// Registers is the component that owns the shared yanks, named registers,
// yank history, the register selected for the next operation and the macro
// state. It is one instance under app, reached through the register, macro
// and yank-history operations and Runtime.Registers(), never through
// Runtime fields. Its key rows (q, @, " and the pending modes they enter)
// sit on the app noun so a page or list focus can record and replay too.
type Registers struct {
	Named   map[string]Register
	History []Yank
	Active  string
	Macro   MacroState
	// Copy puts a yank's text on the system clipboard (the clipboard
	// register + and the clipboard setting); nil sends it through the
	// terminal as OSC 52 (tea.SetClipboard), which TUIOS and ssh pass on.
	// An app with a clipboard program of its own (pbcopy) sets it.
	Copy      func(text string) tea.Cmd
	selection int     // the yank-history modal's highlighted row
	clip      *string // the text the next PendingCommand copies
}

func registerRegisters(r *Runtime) {
	inst := r.Instance(registersID, Kind("ochazuke.register"))
	inst.Component = &Registers{Named: map[string]Register{}}
}

// registerThingProps are a register's props as the register noun's
// things answer them.
var registerThingProps = []PropSpec{
	{Name: "name", Key: true, Human: true, Parameter: Parameter{Description: "The register: a-z (A-Z names the same one; register.append adds to it), 0-9, \" (unnamed), - (small delete), + or * (the system clipboard), _ (discards)"}},
	{Name: "text", Parameter: Parameter{Description: "The text it holds"}},
	{Name: "shape", Parameter: Parameter{Enum: []string{"char", "line", "block"}, Description: "How it pastes: char, line or block"}},
	{Name: "width", Parameter: Parameter{Type: SettingInteger, Description: "A block's width in cells (computed from the text when larger)"}},
	{Name: "keys", ReadOnly: true, Parameter: Parameter{Type: PropObject, Description: "The keys of a macro recorded into it"}},
}

// registerRegisterThings declares the register noun: the named registers
// are things keyed by name; set writes one (an uppercase name is the same
// register, _ discards) and history-entry.undo puts its old text back;
// register.append adds text to one, as an uppercase yank does; get and
// list read them; register.select picks the one the next yank, delete or
// paste uses.
func registerRegisterThings(r *Runtime) {
	thing := func(key string, reg Register) Thing {
		t := Thing{"name": key, "text": reg.Yank.Text, "shape": reg.Yank.Shape, "width": reg.Yank.Width}
		if len(reg.Keys) > 0 {
			t["keys"] = reg.Keys
		}
		return t
	}
	r.Define("ochazuke.register").Unpersisted().Props(registerThingProps...).Store(Store{
		Kind: KindCollection, Stamps: true, Undo: true,
		Get: func(c Ctx, name string) (Thing, error) {
			key, _, err := registerName(name)
			if err != nil {
				return nil, err
			}
			reg, ok := c.Runtime.Registers().Named[key]
			if !ok {
				return Thing{"name": key, "text": "", "shape": "char", "width": 0}, nil
			}
			return thing(key, reg), nil
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			named := c.Runtime.Registers().Named
			out := []Thing{}
			for _, key := range slices.Sorted(maps.Keys(named)) {
				out = append(out, thing(key, named[key]))
			}
			return out, nil
		},
		Patch: func(c Ctx, name string, changes Thing) (Thing, error) {
			key, _, err := registerName(name)
			if err != nil {
				return nil, err
			}
			current := c.Runtime.Registers().Named[key].Yank
			y := Yank{Text: current.Text, Shape: current.Shape, Width: current.Width}
			if v, ok := changes["text"].(string); ok {
				y.Text = v
			}
			if v, ok := changes["shape"].(string); ok {
				y.Shape = v
			}
			if v, ok := changes["width"]; ok {
				y.Width = IntArg(Arguments{"width": v}, "width", 0)
			}
			if y.Shape == "" {
				y.Shape = "char"
			}
			if y.Width < 0 {
				return nil, errors.New("width must be nonnegative")
			}
			if y.Shape == "line" && !strings.HasSuffix(y.Text, "\n") {
				y.Text += "\n"
			}
			if y.Shape == "block" {
				for _, line := range strings.Split(y.Text, "\n") {
					y.Width = max(y.Width, cellWidth(line))
				}
			}
			// The lowercase name: a set replaces, so undo can write the old
			// values back; appending is register.append.
			if err := c.Runtime.storeYank(y, key, false); err != nil {
				return nil, err
			}
			return thing(key, c.Runtime.Registers().Named[key]), nil
		},
	})
	r.Define("ochazuke.register").Verb("append", func(r *Runtime, c Context, a Arguments) (any, error) {
		key, _, err := registerName(StringArg(a, "name", ""))
		if err != nil {
			return nil, err
		}
		shape := StringArg(a, "shape", "char")
		if err := r.storeYank(Yank{Text: StringArg(a, "text", ""), Shape: shape}, strings.ToUpper(key), false); err != nil {
			return nil, err
		}
		return thing(key, r.Registers().Named[key]), nil
	}, "Add text to the end of a register, as an uppercase register name does for a yank (A-Z)")
	op := r.Operations.At(Key("ochazuke.register.append"))
	op.Parameters = map[string]Parameter{
		"name":  {Type: "string", Required: true, Noun: "ochazuke.register", Description: "The register to add to: a-z"},
		"text":  {Type: "string", Required: true, Description: "The text to add"},
		"shape": {Type: "string", Enum: []string{"char", "line", "block"}, Description: "How the added text pastes (default char)"},
	}
	op.Positional = []string{"name", "text"}
	r.Operations.Set(op)
}

// Registers returns the registers component.
func (r *Runtime) Registers() *Registers {
	regs, _ := r.Instances[registersID].Component.(*Registers)
	return regs
}

// HandleMessage: the registers do no off-loop work.
func (g *Registers) HandleMessage(tea.Msg) bool { return false }

// PendingCommand copies the last yank bound for the system clipboard.
func (g *Registers) PendingCommand() tea.Cmd {
	if g.clip == nil {
		return nil
	}
	text := *g.clip
	g.clip = nil
	if g.Copy != nil {
		return g.Copy(text)
	}
	return tea.SetClipboard(text)
}

// Close abandons a recording in progress; the registers themselves stay.
func (g *Registers) Close() { g.Macro.Recording, g.Macro.Keys = "", nil }

// Yank retains the geometry needed to paste independently of the source field.
type Yank struct {
	Text  string `json:"text"`
	Shape string `json:"shape"`
	Width int    `json:"width,omitempty"`
}

// Register holds a yank and, when a macro was recorded into it, its keys.
type Register struct {
	Yank Yank       `json:"yank"`
	Keys []MacroKey `json:"keys,omitempty"`
}

func registerName(name string) (string, bool, error) {
	if name == "" {
		name = "\""
	}
	if len(name) != 1 {
		return "", false, errors.New("register must be one character")
	}
	ch := name[0]
	appendMode := ch >= 'A' && ch <= 'Z'
	if appendMode {
		ch += 'a' - 'A'
	}
	if ch == '*' {
		ch = '+' // one system clipboard: Vim's selection and clipboard registers are the same here
	}
	if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '"' || ch == '_' || ch == '-' || ch == '+') {
		return "", false, errors.New("unsupported register")
	}
	return string(ch), appendMode, nil
}
func appendYank(a, b Yank) Yank {
	if a.Text == "" {
		return b
	}
	if a.Shape == "block" && b.Shape == "block" {
		left, right := strings.Split(a.Text, "\n"), strings.Split(b.Text, "\n")
		rows := make([]string, max(len(left), len(right)))
		for i := range rows {
			l, rr := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				rr = right[i]
			}
			rows[i] = padCells(l, a.Width) + padCells(rr, b.Width)
		}
		return Yank{Text: strings.Join(rows, "\n"), Shape: "block", Width: a.Width + b.Width}
	}
	if a.Shape == "line" || b.Shape == "line" {
		return Yank{Text: strings.TrimSuffix(a.Text, "\n") + "\n" + strings.TrimSuffix(b.Text, "\n") + "\n", Shape: "line"}
	}
	return Yank{Text: a.Text + b.Text, Shape: "char"}
}
func (r *Runtime) storeYank(y Yank, name string, deletion bool) error {
	g := r.Registers()
	if name == "" {
		name = g.Active
	}
	key, appendMode, err := registerName(name)
	if err != nil {
		return err
	}
	g.Active = ""
	if isBlackHole(key) {
		return nil
	}
	if appendMode {
		y = appendYank(g.Named[key].Yank, y)
	}
	g.Named[key] = Register{Yank: y}
	g.Named["\""] = Register{Yank: y}
	if deletion {
		if y.Shape == "line" || strings.Contains(y.Text, "\n") {
			for n := byte('9'); n > '1'; n-- {
				g.Named[string(n)] = g.Named[string(n-1)]
			}
			g.Named["1"] = Register{Yank: y}
		} else {
			g.Named["-"] = Register{Yank: y}
		}
	} else {
		g.Named["0"] = Register{Yank: y}
	}
	if y.Text != "" && r.copiesToClipboard(key, deletion) {
		text := y.Text
		g.clip = &text
	}
	if y.Text != "" {
		g.History = append([]Yank{y}, g.History...)
		if len(g.History) > 100 {
			g.History = g.History[:100]
		}
	}
	return nil
}
func (r *Runtime) readYank(name string) (Yank, error) {
	g := r.Registers()
	if name == "" {
		name = g.Active
	}
	key, _, err := registerName(name)
	if err != nil {
		return Yank{}, err
	}
	if isBlackHole(key) {
		return Yank{Shape: "char"}, nil
	}
	return g.Named[key].Yank, nil
}

// ClipboardSetting says which yanks also go to the system clipboard
// (Registers.Copy): none (only the clipboard register "+), yanks (every
// yank, not a delete: the default) or unnamed (every yank and delete, as
// Vim's clipboard=unnamed).
const ClipboardSetting = "clipboard"

// YankReportSetting names the setting that has an editor's yank say what
// it copied on the status line (yankReport).
const YankReportSetting = "yank.report"

// yankReport is what an editor's yank says it copied: whole lines by
// count (a block's lines too), else characters.
func yankReport(y Yank) string {
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch y.Shape {
	case "line":
		return "copied " + plural(strings.Count(strings.TrimSuffix(y.Text, "\n"), "\n")+1, "line")
	case "block":
		return "copied " + plural(strings.Count(y.Text, "\n")+1, "line") + " (block)"
	}
	return "copied " + plural(len([]rune(y.Text)), "character")
}

// copiesToClipboard reports whether a yank stored in register key goes to
// the system clipboard: the clipboard register always, else as the
// clipboard setting says.
func (r *Runtime) copiesToClipboard(key string, deletion bool) bool {
	if key == "+" {
		return true
	}
	switch r.Settings().String(ClipboardSetting) {
	case "unnamed":
		return true
	case "yanks":
		return !deletion
	}
	return false
}
