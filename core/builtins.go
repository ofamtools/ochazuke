package core

import (
	"maps"
	"slices"
	"sort"

	"ofam.dev/ochazuke/layout"
)

func (e *Engine) registerBuiltins() {
	dir := func(d layout.Dir) Handler {
		return func(s State, c Command) StepResult { return Next(focusDir(s, d, c.Times())) }
	}
	e.global = map[string]Handler{
		CmdFocusNext:  func(s State, c Command) StepResult { return Next(focusCycle(s, c.Times())) },
		CmdFocusPrev:  func(s State, c Command) StepResult { return Next(focusCycle(s, -c.Times())) },
		CmdFocusLeft:  dir(layout.Left),
		CmdFocusRight: dir(layout.Right),
		CmdFocusUp:    dir(layout.Up),
		CmdFocusDown:  dir(layout.Down),
		CmdFocusTo: func(s State, c Command) StepResult {
			if id := c.Arg("id"); id != "" {
				s = setFocus(s, id)
			}
			return Next(s)
		},
		CmdModeSet: func(s State, c Command) StepResult {
			f := s.ActiveFrame()
			f.Mode = c.Arg("mode")
			return Next(s.WithActiveFrame(f))
		},
		CmdPageOpen: func(s State, c Command) StepResult { return Next(openPage(s, c)) },
		CmdPageNext: func(s State, c Command) StepResult { return Next(cyclePage(s, c.Times())) },
		CmdPagePrev: func(s State, c Command) StepResult { return Next(cyclePage(s, -c.Times())) },
		CmdModalOpen: func(s State, c Command) StepResult {
			f := Frame{ID: c.Arg("id"), Kind: FrameModal, Mode: c.Arg("mode"), FocusID: c.Arg("focus"), Data: c.Args["data"]}
			if f.ID == "" {
				f.ID = "modal"
			}
			s.Modals = append(slices.Clone(s.Modals), f)
			return Next(s)
		},
		CmdModalClose: func(s State, c Command) StepResult {
			i := len(s.Modals) - 1
			if id := c.Arg("id"); id != "" {
				i = slices.IndexFunc(s.Modals, func(f Frame) bool { return f.ID == id })
			}
			if i >= 0 {
				s.Modals = slices.Delete(slices.Clone(s.Modals), i, i+1)
			}
			return Next(s)
		},
		CmdLayerPush: func(s State, c Command) StepResult {
			switch l := c.Args["layer"].(type) {
			case Layer:
				s.Input.Layer = &l
			case *Layer:
				if l != nil {
					cp := *l
					s.Input.Layer = &cp
				}
			}
			return Next(s)
		},
		CmdLayerPop: func(s State, _ Command) StepResult {
			s.Input.Layer = nil
			return Next(s)
		},
		CmdCountPush: func(s State, c Command) StepResult {
			d, ok := intArg(c.Args["digit"])
			if !ok || d < 0 || d > 9 || (d == 0 && c.Count == 0) {
				return Next(s)
			}
			n := c.Count*10 + d
			s.Input.Count = &n
			return Next(s)
		},
		CmdAppQuit: func(s State, _ Command) StepResult { return Next(s, QuitEffect{}) },
	}
}

func setFocus(s State, id string) State {
	f := s.ActiveFrame()
	f.FocusID = id
	return s.WithActiveFrame(f)
}

// focusables returns Layout.FocusOrder, restricted to nodes inside the
// active frame's rect when the layout has one for the frame's ID.
func focusables(s State) []string {
	order := s.Layout.FocusOrder
	fr, ok := s.Layout.Rects[s.ActiveFrame().ID]
	if !ok {
		return order
	}
	var out []string
	for _, id := range order {
		r := s.Layout.Rects[id]
		if r.X >= fr.X && r.Y >= fr.Y && r.X+r.W <= fr.X+fr.W && r.Y+r.H <= fr.Y+fr.H {
			out = append(out, id)
		}
	}
	return out
}

func focusCycle(s State, delta int) State {
	order := focusables(s)
	n := len(order)
	if n == 0 {
		return s
	}
	i := slices.Index(order, s.ActiveFrame().FocusID)
	if i < 0 && delta < 0 {
		i = 0
	}
	return setFocus(s, order[((i+delta)%n+n)%n])
}

func focusDir(s State, d layout.Dir, times int) State {
	allowed := focusables(s)
	cur := s.ActiveFrame().FocusID
	for range times {
		next, ok := s.Layout.Neighbor(cur, d)
		if !ok || !slices.Contains(allowed, next) {
			break
		}
		cur = next
	}
	return setFocus(s, cur)
}

func pageOrder(s State) []string {
	var order []string
	for _, id := range s.PageOrder {
		if _, ok := s.Pages[id]; ok && !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	var rest []string
	for id := range s.Pages {
		if !slices.Contains(order, id) {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(order, rest...)
}

func cyclePage(s State, delta int) State {
	order := pageOrder(s)
	if n := len(order); n > 0 {
		i := max(slices.Index(order, s.ActivePage), 0)
		s.ActivePage = order[((i+delta)%n+n)%n]
	}
	return s
}

func openPage(s State, c Command) State {
	id := c.Arg("id")
	if id == "" {
		return s
	}
	if _, ok := s.Pages[id]; !ok {
		s.Pages = maps.Clone(s.Pages)
		if s.Pages == nil {
			s.Pages = map[string]Frame{}
		}
		s.Pages[id] = Frame{ID: id, Kind: FramePage, Mode: c.Arg("mode"), FocusID: c.Arg("focus")}
	}
	if !slices.Contains(s.PageOrder, id) {
		s.PageOrder = append(slices.Clone(s.PageOrder), id)
	}
	s.ActivePage = id
	return s
}
