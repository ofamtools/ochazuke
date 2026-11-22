package main

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

const vimText = "The quick brown fox\njumps over the lazy dog\nand naps in the sun"

// The vim page runs in the passthrough "vim" mode: keys the keymap does not
// claim (everything except tab, shift+tab and alt+1..5) reach widgets/input
// as vim.key, which owns counts, operators, registers, undo and repeat.
func (a *app) vimHandlers() {
	a.eng.HandleScope("tab.vim", "vim.key", func(s core.State, c core.Command) core.StepResult {
		a.vim.HandleInput(c.Arg("key"), c.Arg("text"))
		return core.Next(s)
	})
}

func (a *app) vimView() []ui.ViewNode {
	v := a.vim
	iv := v.View("vim")
	iv.Box = flex1
	lines := strings.Count(v.Value(), "\n") + 1
	return []ui.ViewNode{
		text(" A modal editor. Try w, dw, ciw, dd, u, ., i ... esc, v, yy, p. Counts work: 2dw.", "muted"),
		iv,
		text(fmt.Sprintf(" %s · %d lines · cursor %d", v.Mode(), lines, v.Cursor()), "accent"),
	}
}
