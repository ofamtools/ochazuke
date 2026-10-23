package ochazuke

import (
	"errors"
	"strings"
)

// Yank retains the geometry needed to paste independently of the source field.
type Yank struct {
	Text  string `json:"text"`
	Shape string `json:"shape"`
	Width int    `json:"width,omitempty"`
}
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
	if name == "" {
		name = r.ActiveRegister
	}
	key, appendMode, err := registerName(name)
	if err != nil {
		return err
	}
	r.ActiveRegister = ""
	if key == "_" {
		return nil
	}
	if appendMode {
		y = appendYank(r.Registers[key].Yank, y)
	}
	r.Registers[key] = Register{Yank: y}
	r.Registers["\""] = Register{Yank: y}
	if deletion {
		if y.Shape == "line" || strings.Contains(y.Text, "\n") {
			for n := byte('9'); n > '1'; n-- {
				r.Registers[string(n)] = r.Registers[string(n-1)]
			}
			r.Registers["1"] = Register{Yank: y}
		} else {
			r.Registers["-"] = Register{Yank: y}
		}
	} else {
		r.Registers["0"] = Register{Yank: y}
	}
	if y.Text != "" {
		r.YankHistory = append([]Yank{y}, r.YankHistory...)
		if len(r.YankHistory) > 100 {
			r.YankHistory = r.YankHistory[:100]
		}
	}
	return nil
}
func (r *Runtime) readYank(name string) (Yank, error) {
	if name == "" {
		name = r.ActiveRegister
	}
	key, _, err := registerName(name)
	if err != nil {
		return Yank{}, err
	}
	if key == "_" {
		return Yank{Shape: "char"}, nil
	}
	return r.Registers[key].Yank, nil
}
