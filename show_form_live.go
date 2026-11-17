package ochazuke

import (
	"slices"
)

// The live view of a show page (run plan 5.6, spine S9): a noun declares a
// prop drawn as a view, {"name": "live", "view": {"kind": "terminal",
// "hook": NAME}}, and the show form of one of its things draws the thing's
// terminal in its place (a show form that asks for it, ShowFormSpec.Live:
// a catalog record-form detail does). ochazuke.detail.attach opens it on the current
// page's show form (a key row names it with when detail-live, so the key
// goes on to its next row where nothing can go live): the hook opens the thing's stream, a
// terminal under the show form's row stands in for the form, focused, in
// terminal mode (keys go to the pane; the leader's rows as on any
// terminal). The leader's d detaches: the terminal closes, its stream is
// released and the focus goes back where the live view was opened from
// (the row). A selection of another thing closes it too. Nothing is
// opened until asked: moving over rows opens no stream. Setting
// detail.live-view off makes detail-live false, so the key falls through.

// PropView is how a prop is drawn on the show page when it is a view of
// its own (PropSpec.View): Kind names the view (PropViewTerminal), Hook
// the Go hook it reads (Runtime.AddHook; for a terminal a TerminalHook).
type PropView struct {
	Kind string `json:"kind"`
	Hook string `json:"hook,omitempty"`
}

// PropViewTerminal is the view kind of a thing's live terminal.
const PropViewTerminal = "terminal"

// TerminalHook opens the terminal stream of the thing key of a noun: the
// hook a terminal view names (Runtime.AddHook).
type TerminalHook = func(c Ctx, key string) (TerminalStream, error)

// DetailLiveSetting turns the show page's live view on (default) or off.
const DetailLiveSetting = "detail.live-view"

// liveID is the id of the show form's live terminal.
func (sf *ShowForm) liveID() string { return sf.id + "/live" }

// Live is the show form's open live terminal, nil when none is open.
func (sf *ShowForm) Live() *Terminal { return sf.r.Terminal(sf.liveID()) }

// liveHook is the terminal hook of the thing the show form holds: its
// noun's first prop with a terminal view whose hook is registered.
func (sf *ShowForm) liveHook() TerminalHook {
	if !sf.spec.Live || sf.liveKey == "" || !sf.r.Settings().Bool(DetailLiveSetting) {
		return nil
	}
	return sf.r.terminalHook(sf.liveNoun)
}

// followLive keeps the live view on the shown thing: a selection of
// another thing (or none) closes it.
func (sf *ShowForm) followLive() {
	noun, key := sf.spec.Target()
	if noun != sf.liveNoun || key != sf.liveKey {
		sf.closeLive()
		sf.liveNoun, sf.liveKey = noun, key
	}
}

// terminalHook is noun's terminal hook, nil when it declares none.
func (r *Runtime) terminalHook(noun string) TerminalHook {
	n := r.Noun(noun)
	if n == nil {
		return nil
	}
	for _, p := range n.Props {
		if p.View == nil {
			continue
		}
		// The hook's type says what it opens: a TerminalHook is a terminal.
		if named, ok := r.Named(NamedHook, p.View.Hook); ok {
			if hook, ok := named.Value.(TerminalHook); ok {
				return hook
			}
		}
	}
	return nil
}

// openLive opens the live view and focuses it; back is where the leader's
// d returns.
func (sf *ShowForm) openLive(c Ctx, hook TerminalHook, back string) (*Terminal, error) {
	r := sf.r
	if t := sf.Live(); t != nil {
		return t, r.SetFocus(t.id)
	}
	stream, err := hook(c, sf.liveKey)
	if err != nil {
		return nil, err
	}
	parent := sf.spec.Row
	if parent == "" || r.Instances[parent] == nil {
		parent = r.Instances[sf.id].Parent
	}
	t, err := r.AddTerminal(sf.liveID(), stream, Parent(parent), CloseOnDetach(), FocusOnClose(back))
	if err != nil {
		if rel, ok := stream.(TerminalReleaser); ok {
			_ = rel.Release() // never added, so no Terminal.Close releases it
		}
		return nil, err
	}
	return t, r.SetFocus(t.id)
}

// closeLive removes the live terminal (its Close releases the stream).
func (sf *ShowForm) closeLive() {
	if sf.r.Instances[sf.liveID()] != nil {
		_ = sf.r.RemoveInstance(sf.liveID())
	}
}

// liveForm is the current page's show form that can go live, nil when none.
func (r *Runtime) liveForm() (*ShowForm, TerminalHook) {
	for _, inst := range r.Children(r.Page) {
		if sf, ok := inst.Component.(*ShowForm); ok {
			if hook := sf.liveHook(); hook != nil {
				return sf, hook
			}
		}
	}
	return nil, nil
}

func registerDetailLive(r *Runtime) {
	r.DefineGuard("detail-live", func(r *Runtime, ev KeyEvent) bool {
		sf, _ := r.liveForm()
		// From the row the show page follows, or from inside the show page.
		return sf != nil && !slices.Contains(r.InstancePath(ev.Focus), sf.liveID())
	})
	r.Define(detailNoun).Verb("attach", func(r *Runtime, c Context, _ Arguments) (any, error) {
		sf, hook := r.liveForm()
		if sf == nil {
			return nil, nil // nothing here goes live: the key fell through to it
		}
		t, err := sf.openLive(Ctx{Runtime: r, Context: c}, hook, focusOf(c))
		if err != nil {
			return nil, err
		}
		return map[string]any{"noun": sf.liveNoun, "key": sf.liveKey, "terminal": t.id}, nil
	}, "Show the selected thing's terminal live in its show page and focus it: keys go to it until the leader's d comes back to the row")
}
