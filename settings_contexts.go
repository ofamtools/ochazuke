package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Context points (in-context settings, plan L1 and L3): where a setting
// applies, as data on its row (Setting.Contexts) and on its section's
// (SettingSection.Contexts). Each point is a reference {noun, key} whose
// noun says what the key names; one table (contextKinds) judges a point
// against a level of the focus walk (HelpWalkAt) and says whether its key
// names something the runtime knows. Nothing here reads a setting's name.

// The nouns a context point's reference may name.
const (
	// ContextNoun: any walk level whose noun is the key ("app" is the
	// app level every walk ends at).
	ContextNoun = FrameworkNamespace + ".noun"
	// ContextPage: the page instance whose id is the key.
	ContextPage = FrameworkNamespace + ".page"
	// ContextInstance: the instance whose id is the key.
	ContextInstance = FrameworkNamespace + ".instance"
	// ContextMode: the focus level while the base mode is the key.
	ContextMode = FrameworkNamespace + ".mode"
)

// Where a setting's point comes from (SettingPoint.From).
const (
	PointFromRow     = "row"
	PointFromSection = "section"
)

// SettingPoint is one context point of a setting: the reference and where
// it was declared (the setting's own row or its section's).
type SettingPoint struct {
	Ref
	From string `json:"from"`
}

// contextKind is how points of one reference noun are judged: at reports
// whether a walk level matches the key, known whether the key names
// something the runtime has.
type contextKind struct {
	at    func(r *Runtime, w HelpWalk, l HelpLevel, key string) bool
	known func(r *Runtime, key string) bool
}

// contextKinds is the one table of point nouns.
var contextKinds = map[string]contextKind{
	ContextNoun: {
		at:    func(r *Runtime, _ HelpWalk, l HelpLevel, key string) bool { return l.Noun == r.CanonicalNoun(key) },
		known: func(r *Runtime, key string) bool { return r.Noun(key) != nil },
	},
	ContextPage: {
		at:    func(r *Runtime, _ HelpWalk, l HelpLevel, key string) bool { return l.At == key && r.PageOf(key) != nil },
		known: func(r *Runtime, key string) bool { return r.PageOf(key) != nil },
	},
	ContextInstance: {
		at:    func(_ *Runtime, _ HelpWalk, l HelpLevel, key string) bool { return l.At == key },
		known: func(r *Runtime, key string) bool { return r.Instances[key] != nil },
	},
	ContextMode: {
		at: func(_ *Runtime, w HelpWalk, l HelpLevel, key string) bool { return l.Index == 0 && w.Mode == key },
		known: func(r *Runtime, key string) bool {
			_, ok := r.modes[key]
			return ok
		},
	},
}

// contextNouns are the point nouns in a fixed order, for messages.
func contextNouns() []string {
	out := make([]string, 0, len(contextKinds))
	for noun := range contextKinds {
		out = append(out, noun)
	}
	slices.Sort(out)
	return out
}

// sectionContexts are the points of an owner's section: its own
// declaration's, else those of the section declared for every owner.
func (s *Settings) sectionContexts(owner, id string) []Ref {
	if d, at := s.sectionDecl(owner, id); at >= 0 && len(d.Contexts) > 0 {
		return d.Contexts
	}
	if at := slices.IndexFunc(s.sections, func(d SettingSection) bool { return d.Owner == "" && d.ID == id }); at >= 0 {
		return s.sections[at].Contexts
	}
	return nil
}

// pointsOf are a declared setting's context points: its row's, then its
// section's, each reference once.
func (s *Settings) pointsOf(one Setting) []SettingPoint {
	var out []SettingPoint
	add := func(refs []Ref, from string) {
		for _, ref := range refs {
			if !slices.ContainsFunc(out, func(p SettingPoint) bool { return p.Ref == ref }) {
				out = append(out, SettingPoint{Ref: ref, From: from})
			}
		}
	}
	add(one.Contexts, PointFromRow)
	add(s.sectionContexts(s.ownerOf(one), one.SectionName()), PointFromSection)
	return out
}

// Points are the named setting's context points (its row's, then its
// section's); nil for an unknown name.
func (s *Settings) Points(name string) []SettingPoint {
	if one := s.find(s.canonical(name)); one != nil {
		return s.pointsOf(*one)
	}
	return nil
}

// pointStrings are points in their text form, noun:key.
func pointStrings(points []SettingPoint) []string {
	out := make([]string, 0, len(points))
	for _, p := range points {
		out = append(out, p.String())
	}
	return out
}

// sectionKey names an owner's section; owner "" is the declaration for
// every owner.
type sectionKey struct{ owner, id string }

func (k sectionKey) String() string {
	return fmt.Sprintf("%s of %s", k.id, cmp.Or(k.owner, "every owner"))
}

// CheckSettingContexts reports, as warnings, every context point whose
// noun is not a point noun or whose key names nothing the runtime has,
// and each section whose settings have no point at all. A point that
// warns still loads: it matches nothing until its thing exists.
func (r *Runtime) CheckSettingContexts() []string {
	s := r.Settings()
	s.ensure()
	var out []string
	warned := map[string]bool{}
	warn := func(line string) {
		if !warned[line] {
			warned[line] = true
			out = append(out, "warning: "+line)
		}
	}
	judge := func(where string, refs []Ref) {
		for _, ref := range refs {
			kind, ok := contextKinds[r.CanonicalNoun(ref.Noun)]
			switch {
			case !ok:
				warn(fmt.Sprintf("%s: context %s: %s is not a context noun (%s)", where, ref, ref.Noun, strings.Join(contextNouns(), ", ")))
			case !kind.known(r, ref.Key):
				warn(fmt.Sprintf("%s: context %s names nothing here (kept)", where, ref))
			}
		}
	}
	// A section's points are judged where settings use it: a framework
	// section no feature of this program declares settings in (markdown
	// without the markdown package) names nothing it needs.
	used := map[sectionKey]bool{}
	for _, one := range s.declared {
		used[sectionKey{s.ownerOf(one), one.SectionName()}], used[sectionKey{"", one.SectionName()}] = true, true
	}
	for _, d := range s.sections {
		if key := (sectionKey{d.Owner, d.ID}); used[key] {
			judge("section "+key.String(), d.Contexts)
		}
	}
	bare := map[sectionKey][]string{}
	var order []sectionKey
	for _, one := range s.declared {
		judge("setting "+one.Name, one.Contexts)
		if len(s.pointsOf(one)) == 0 {
			key := sectionKey{s.ownerOf(one), one.SectionName()}
			if bare[key] == nil {
				order = append(order, key)
			}
			bare[key] = append(bare[key], one.Name)
		}
	}
	for _, key := range order {
		warn(fmt.Sprintf("section %s: %d settings have no context point, so they show at no level of a walk: %s", key, len(bare[key]), strings.Join(bare[key], ", ")))
	}
	return out
}

// SettingPlace is where a setting sits on a focus walk: the innermost
// level one of its points matches (-1: none, not on this walk), that
// level's label and role.
type SettingPlace struct {
	Level int    `json:"level"`
	Label string `json:"level_label"`
	Role  string `json:"level_role,omitempty"`
}

// settingPlaces places every declared setting on the walk: the innermost
// level any of its points matches, the way the first binding row wins.
func (r *Runtime) settingPlaces(w HelpWalk) map[string]SettingPlace {
	s := r.Settings()
	out := make(map[string]SettingPlace, len(s.declared))
	for _, one := range s.declared {
		place := SettingPlace{Level: -1, Label: w.LevelLabel(-1)}
		points := s.pointsOf(one)
	levels:
		for _, l := range w.Levels {
			for _, p := range points {
				if kind, ok := contextKinds[r.CanonicalNoun(p.Noun)]; ok && kind.at(r, w, l, p.Key) {
					place = SettingPlace{Level: l.Index, Label: w.LevelLabel(l.Index), Role: l.Role}
					break levels
				}
			}
		}
		out[one.Name] = place
	}
	return out
}

// SettingWalkAt is the walk setting.list {at} places settings on: from
// the instance at (the live focus, under the open modals, when at is ""),
// in the base mode.
func (r *Runtime) SettingWalkAt(at string) HelpWalk {
	mode := ModeState{Name: r.BaseMode()}
	if at == "" {
		return r.HelpWalkAt(r.Focus, mode, r.modalIDs())
	}
	return r.HelpWalkAt(at, mode, nil)
}

// SettingPlacesAt places every declared setting on the walk from at
// (SettingWalkAt), by name.
func (r *Runtime) SettingPlacesAt(at string) map[string]SettingPlace {
	return r.settingPlaces(r.SettingWalkAt(at))
}
