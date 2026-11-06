package ochazuke

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// refresh: a copy noun's on-demand comparison with its source (copy
// stores, copy_store.go). refresh {key?} reads the source (one thing, or
// every one when no key is given; a remote source in a job, never on the
// loop) and compares each copy's props with the source's, prop by prop:
// only the props the source holds, never the key, the base times (the
// copy's own), a secret or a derived prop. What differs opens one modal: a
// plain diff (- the copy's value, + the source's) above a choice per prop,
// local or source. A pick writes through set, so it is undone like any
// set; leaving the modal picks nothing. Differences never block anything:
// at most a mark on the thing's list rows (copies.marks) while its copy
// differs from what the last refresh read and no pick kept it.
//
// What the last refresh read is kept in memory for the marks only, never
// stored: no original values are remembered (the owner's trade-off), so a
// prop changed here on purpose shows again on the next refresh.

// copyDiff is one prop of one thing whose copy differs from its source.
type copyDiff struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Prop   string `json:"prop"`
	Local  any    `json:"local"`
	Source any    `json:"source"`
}

// copyMarks is what a noun's last refresh read: by key, the source's
// values of the props it compared, and the props a pick kept local.
type copyMarks struct {
	seen map[string]map[string]any
	kept map[string]map[string]bool
}

// copyRefreshFormID is the refresh modal's record editor.
const copyRefreshFormID = "copies-refresh"

// The two values a refresh pick takes.
const (
	copyPickLocal  = "local"
	copyPickSource = "source"
)

// copyMarkText is the mark a differing thing's list row ends with.
const copyMarkText = " ≠"

// needCopy is the refresh row's need: a copy store (CopyStore).
var needCopy = storeNeed{Doc: "a copy store (CopyStore)", Lacks: "its store is not a copy of a source",
	Has: func(_ *Runtime, n *Noun) bool { return n.Store.copy != nil }}

// marksOf is the noun's marks, made on first use.
func (r *Runtime) marksOf(noun string) *copyMarks {
	if r.copyMarks == nil {
		r.copyMarks = map[string]*copyMarks{}
	}
	m := r.copyMarks[noun]
	if m == nil {
		m = &copyMarks{seen: map[string]map[string]any{}, kept: map[string]map[string]bool{}}
		r.copyMarks[noun] = m
	}
	return m
}

// diffProps are the props a refresh compares.
func diffProps(n *Noun) []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if p.Key || p.Secret || p.Derived || p.Role == RoleCreated || p.Role == RoleUpdated {
			continue
		}
		out = append(out, p)
	}
	return out
}

// samePropValue reports whether two values of prop p are the same: compared in
// the prop's own form when both read as it, else as JSON.
func samePropValue(p PropSpec, a, b any) bool {
	if emptyProp(a) && emptyProp(b) {
		return true
	}
	if ta, err := propValue(p, a); err == nil {
		if tb, err := propValue(p, b); err == nil {
			a, b = ta, tb
		}
	}
	ja, errA := json.Marshal(fileForm(a))
	jb, errB := json.Marshal(fileForm(b))
	if errA != nil || errB != nil {
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
	return string(ja) == string(jb)
}

// copyRefresh is what one refresh read: the source's values of the
// compared props by key, and the differences, in order.
type copyRefresh struct {
	all   bool
	seen  map[string]map[string]any
	diffs []copyDiff
}

// readRefresh compares the copies with the source: key's alone, or every
// copy's when key is "". A thing with no copy yet is copied in, with
// nothing to compare.
func (r *Runtime) readRefresh(n *Noun, c Ctx, key string) (copyRefresh, error) {
	cs := n.Store.copy
	out := copyRefresh{all: key == "", seen: map[string]map[string]any{}}
	kp := n.keyProp()
	var pairs [][2]Thing
	if key != "" {
		local, had, err := cs.local.Get(key)
		if err != nil {
			return out, err
		}
		if !had || tombstoned(local) {
			_, err := cs.get(c, key) // copied in (or unknown): nothing to compare
			return out, err
		}
		if cs.source.Get == nil {
			return out, fmt.Errorf("%s: the source reads no single thing", r.DisplayNoun(n.Name))
		}
		src, err := cs.source.Get(c, key)
		if err != nil {
			return out, err
		}
		pairs = append(pairs, [2]Thing{local, src})
	} else {
		srcs, err := cs.sourceList(c, ListQuery{Archived: true})
		if err != nil {
			return out, err
		}
		locals, err := cs.local.List()
		if err != nil {
			return out, err
		}
		byKey := map[string]Thing{}
		for _, t := range locals {
			if k := thingKey(kp, t); k != "" && !tombstoned(t) {
				byKey[k] = t
			}
		}
		for _, src := range srcs {
			if local, ok := byKey[thingKey(kp, src)]; ok {
				pairs = append(pairs, [2]Thing{local, src})
				delete(byKey, thingKey(kp, src))
			}
		}
	}
	props := diffProps(n)
	for _, pair := range pairs {
		local, src := pair[0], pair[1]
		k := thingKey(kp, local)
		seen := map[string]any{}
		label := n.displayLabel(func(name string) any { return local[name] })
		if label == "" {
			label = k
		}
		for _, p := range props {
			sv, held := src[p.Name]
			if !held {
				continue // a prop the source does not hold is the copy's own
			}
			seen[p.Name] = sv
			if !samePropValue(p, local[p.Name], sv) {
				out.diffs = append(out.diffs, copyDiff{Key: k, Label: label, Prop: p.Name, Local: local[p.Name], Source: sv})
			}
		}
		out.seen[k] = seen
	}
	return out, nil
}

// noteRefresh keeps what a refresh read for the marks: a refresh of every
// thing replaces all of it, of one thing that thing's; picks kept before
// are forgotten for what it read.
func (r *Runtime) noteRefresh(n *Noun, read copyRefresh) {
	m := r.marksOf(n.Name)
	if read.all {
		m.seen, m.kept = map[string]map[string]any{}, map[string]map[string]bool{}
	}
	for k, seen := range read.seen {
		m.seen[k] = seen
		delete(m.kept, k)
	}
	r.refillFrom(n.Name)
}

// copyMarked reports whether a thing's rows carry the mark: copies.marks
// is on and a prop the last refresh read differs from its copy, with no
// pick that kept it.
func (r *Runtime) copyMarked(n *Noun, t Thing) bool {
	if n.Store == nil || n.Store.copy == nil || r.copyMarks[n.Name] == nil || !r.Settings().Bool(CopiesMarksSetting) {
		return false
	}
	m := r.copyMarks[n.Name]
	k := thingKey(n.keyProp(), t)
	for name, sv := range m.seen[k] {
		p := n.prop(name)
		if p == nil || m.kept[k][name] {
			continue
		}
		if !samePropValue(*p, t[name], sv) {
			return true
		}
	}
	return false
}

// refillFrom fills every list of the noun's things again (From), for a
// change that is not one of its things' (a mark).
func (r *Runtime) refillFrom(noun string) {
	if r.watches == nil || r.watches.from == nil {
		return
	}
	if h := r.watches.from[noun]; h != nil && h.watch != nil {
		h.dirty = true
		h.flush()
	}
}

// refreshHandler is refresh on noun: read (a job for a remote source),
// then on the loop note the marks and show the differences.
func (r *Runtime) refreshHandler(noun string) AppHandler {
	return func(c Ctx) Result {
		n := r.Nouns[noun]
		if n == nil || n.Store == nil || n.Store.copy == nil {
			return Failed(fmt.Errorf("%s: its store is not a copy of a source", noun))
		}
		named := false
		if kp := n.keyProp(); kp != nil {
			v, ok := c.Args[kp.Name]
			named = ok && !emptyProp(v)
		}
		var read copyRefresh
		body := func(c Ctx) (any, error) {
			key := ""
			if named {
				k, err := r.subjectKey(n, c)
				if err != nil {
					return nil, err
				}
				key = k
			}
			got, err := r.readRefresh(n, c, key)
			read = got
			return nil, err
		}
		focus, page, modal := r.Focus, r.Page, r.TopModal()
		finish := func(_ any, err error) (any, error) {
			if err != nil {
				return nil, err
			}
			r.noteRefresh(n, read)
			here := r.Focus == focus && r.Page == page && r.TopModal() == modal
			return r.showRefresh(n, c, read.diffs, here)
		}
		return r.runSteppedBody(c, noun+".refresh", body, finish)
	}
}

// showRefresh says what a refresh found: nothing differs, or the diff and
// its picks in a modal (here: the focus has not moved since the request;
// otherwise a notice, and the marks show which rows differ).
func (r *Runtime) showRefresh(n *Noun, c Ctx, diffs []copyDiff, here bool) (any, error) {
	name := r.DisplayNoun(n.Name)
	answer := map[string]any{"differences": diffs}
	if diffs == nil {
		answer["differences"] = []copyDiff{}
	}
	switch {
	case len(diffs) == 0:
		c.Info(name + ": no differences from the source")
		return answer, nil
	case !here:
		c.Info(fmt.Sprintf("%s: %d differences from the source; refresh again here to pick", name, len(diffs)))
		return answer, nil
	}
	if e := r.RecordEditor(copyRefreshFormID); e != nil {
		if err := e.Leave(); err != nil {
			return nil, err
		}
	}
	one := true
	for _, d := range diffs {
		one = one && d.Key == diffs[0].Key
	}
	fields := make([]RecordField, len(diffs))
	byField := map[string]copyDiff{}
	for i, d := range diffs {
		label := d.Prop
		if !one {
			label = d.Label + " · " + d.Prop
		}
		f := RecordField{Name: fmt.Sprintf("d%d", i+1), Label: label, Placeholder: "local or source",
			Parameter: Parameter{Type: SettingString, Enum: []string{copyPickLocal, copyPickSource},
				Description: "Keep the copy's value (local) or take the source's (source)"}}
		fields[i] = f
		byField[f.Name] = d
	}
	title := fmt.Sprintf("Refresh %s: %d differences", name, len(diffs))
	if one {
		title = fmt.Sprintf("Refresh %s %s: %d differences", name, diffs[0].Label, len(diffs))
	}
	text := copyDiffText(diffs)
	var editor *RecordEditor
	spec := RecordSpec{Title: title, Fields: fields, FieldWrites: true,
		Load: func() (Record, error) { return Record{}, nil },
		Save: func(w RecordWrite) Result {
			name := w.Changed[0]
			return r.pickCopy(n, byField[name], w.Values[name])
		},
		Render: func(s Styles, width, height int) Block {
			diff := r.renderDiff(s, text, width, max(1, min(height/2, diffHeight(text))))
			top := strings.Count(diff, "\n") + 2
			b := editor.Block(width, max(1, height-top))
			return Block{Text: diff + "\n\n" + b.Text, Cursor: b.Offset(0, top).Cursor}
		},
	}
	var err error
	if editor, err = r.OpenRecordEditor(copyRefreshFormID, spec); err != nil {
		return nil, err
	}
	return answer, nil
}

// pickCopy applies one pick: source writes the source's value to the copy
// (set), local writes the copy's value back if it changed since and keeps
// it (no mark); an empty pick keeps nothing.
func (r *Runtime) pickCopy(n *Noun, d copyDiff, pick string) Result {
	m := r.marksOf(n.Name)
	value := d.Source
	switch pick {
	case copyPickSource:
		delete(m.kept[d.Key], d.Prop)
	case copyPickLocal:
		value = d.Local
		if m.kept[d.Key] == nil {
			m.kept[d.Key] = map[string]bool{}
		}
		m.kept[d.Key][d.Prop] = true
	default:
		delete(m.kept[d.Key], d.Prop)
		r.refillFrom(n.Name)
		return Done(nil)
	}
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.contextOf("app")}
	if cur, err := n.Store.Get(c, d.Key); err == nil && samePropValue(*n.prop(d.Prop), cur[d.Prop], value) {
		r.refillFrom(n.Name)
		return Done(nil)
	}
	kp := n.keyProp()
	args := Arguments{kp.Name: d.Key, "props": map[string]any{d.Prop: value}}
	v, err := r.ExecuteAt(r.contextOf("app"), Request{Noun: n.Name, Verb: "set", Args: args})
	if err != nil {
		return Failed(err)
	}
	r.refillFrom(n.Name)
	return Done(v)
}

// DiffText is a plain text diff: sections of lines, each line on the old
// side (Side -1), both (0) or the new side (+1). A refresh's is the copies
// (old) against the source (new).
type DiffText struct {
	Old, New string // what each side is: a refresh's local and source
	Sections []DiffSection
}

// DiffSection is one part of a DiffText under its header.
type DiffSection struct {
	Header string
	Lines  []DiffLine
}

// DiffLine is one line of a DiffSection.
type DiffLine struct {
	Side int
	Text string
}

// DiffRenderer draws a DiffText in width by height cells.
type DiffRenderer func(s Styles, d DiffText, width, height int) string

// SetDiffRenderer sets how the runtime draws a plain diff (a refresh's):
// the diffview package sets its own when registered (diffview.Register);
// nil draws plain - and + lines.
func (r *Runtime) SetDiffRenderer(fn DiffRenderer) { r.diffRenderer = fn }

// copyDiffText is a refresh's differences as a DiffText: a section per
// thing, a - and a + line per prop.
func copyDiffText(diffs []copyDiff) DiffText {
	d := DiffText{Old: copyPickLocal, New: copyPickSource}
	for _, x := range diffs {
		if len(d.Sections) == 0 || d.Sections[len(d.Sections)-1].Header != x.Label {
			d.Sections = append(d.Sections, DiffSection{Header: x.Label})
		}
		s := &d.Sections[len(d.Sections)-1]
		s.Lines = append(s.Lines,
			DiffLine{Side: -1, Text: x.Prop + ": " + diffValueText(x.Local)},
			DiffLine{Side: 1, Text: x.Prop + ": " + diffValueText(x.Source)})
	}
	return d
}

// diffHeight is the lines a DiffText takes drawn plainly.
func diffHeight(d DiffText) int {
	n := 1 // the line naming the sides
	for _, s := range d.Sections {
		n += 1 + len(s.Lines)
	}
	return n
}

// diffValueText is a value as a diff line shows it.
func diffValueText(v any) string {
	if emptyProp(v) {
		return "(empty)"
	}
	return cellValueText(v)
}

// renderDiff draws d with the runtime's DiffRenderer, else plainly.
func (r *Runtime) renderDiff(s Styles, d DiffText, width, height int) string {
	if r.diffRenderer != nil {
		return r.diffRenderer(s, d, width, height)
	}
	lines := []string{s.Muted.Render("- " + d.Old + "  + " + d.New)}
	for _, sec := range d.Sections {
		lines = append(lines, s.Title.Render(sec.Header))
		for _, l := range sec.Lines {
			sign := "  "
			switch l.Side {
			case -1:
				sign = "- "
			case 1:
				sign = "+ "
			}
			lines = append(lines, sign+l.Text)
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}

// copyRowMark is a row of a copy noun with its mark (copyMarked).
func (r *Runtime) copyRowMark(n *Noun, t Thing, item ListItem) ListItem {
	if !r.copyMarked(n, t) {
		return item
	}
	item.Label += copyMarkText
	if len(item.Cells) > 0 {
		item.Cells = append([]string(nil), item.Cells...)
		item.Cells[0] += copyMarkText
	}
	return item
}
