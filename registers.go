package ochazuke

import (
	_ "embed"
	"errors"
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
	Named     map[string]Register
	History   []Yank
	Active    string
	Macro     MacroState
	selection int // the yank-history modal's highlighted row
}

// registersSpec is the app noun's register and macro rows as data, in the
// same shape as editor_bindings.json.
//
//go:embed registers_bindings.json
var registersSpec []byte

func registerRegisters(r *Runtime) {
	inst := r.Instance(registersID, Kind("ochazuke.registers"))
	inst.Component = &Registers{Named: map[string]Register{}}
}

// Registers returns the registers component.
func (r *Runtime) Registers() *Registers {
	regs, _ := r.Instances[registersID].Component.(*Registers)
	return regs
}

// HandleMessage: the registers do no off-loop work.
func (g *Registers) HandleMessage(tea.Msg) bool { return false }

// PendingCommand: nothing is ever scheduled.
func (g *Registers) PendingCommand() tea.Cmd { return nil }

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
	if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '"' || ch == '_' || ch == '-') {
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
