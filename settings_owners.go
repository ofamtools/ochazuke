package ochazuke

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Owners and sections: where the Settings page files each setting. A
// setting's owner (Setting.Owner, resolved by Settings.OwnerOf) is a tab of
// the page; its section (Setting.SectionName) is a row of the tab's section
// list; its group (Setting.Group) a heading inside the section. Which owner,
// section and group a setting has comes only from these fields: no code
// looks at a setting's name beyond the first-dot rule of SectionName.
//
// SettingOwner and SettingSection declarations give an owner or a section
// its title, a one-line description and its place in the order; without
// one the title is the ID and the order is the order the owner's or the
// section's first setting was declared. The framework's own declarations
// are the embedded settings/sections.json; an application adds its own with
// Runtime.DeclareSettingOwner and Runtime.DeclareSettingSection.
const (
	// OwnerFramework owns the framework's settings: the embedded rows and
	// what a framework feature declares (inside Runtime.Builtin).
	OwnerFramework = FrameworkNamespace
	// OwnerApp owns a setting that names no owner: the declaring
	// application's. Its title, unless declared, is Config.App.
	OwnerApp = AliasApp
	// SettingsGeneralSection is the section of a setting whose name has
	// no dot and that names no section.
	SettingsGeneralSection = "general"

	// settingLayoutFile is the embedded file of the framework's owner and
	// section declarations (settings/sections.json).
	settingLayoutFile = "sections.json"
)

// SettingOwner declares an owner of settings: a tab of the Settings page.
// Owners are drawn by Order (lowest first), then in the order they were
// declared, then the owners nobody declared in the order of their first
// setting. The framework declares the application (OwnerApp, order 0) and
// itself (OwnerFramework, order 100), so an application's owners (one
// for its extensions) fall between them unless they give an Order.
type SettingOwner struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Order       int    `json:"order,omitempty"`
}

// SettingSection declares a section of an owner's settings: its title, the
// line the page shows while the section list has the focus, and its place:
// declared sections come first, in declaration order, then the rest in the
// order of their first setting. Owner "" declares the section for every
// owner (the framework's General); an owner's own declaration wins.
type SettingSection struct {
	Owner       string `json:"owner,omitempty"`
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// SettingOwnerInfo is an owner as settings.owners answers: the declaration
// filled in (the title resolved), with its section and setting counts and
// how many of its settings hold a session value.
type SettingOwnerInfo struct {
	SettingOwner
	Sections int `json:"sections"`
	Settings int `json:"settings"`
	Session  int `json:"session,omitempty"`
}

// SettingSectionInfo is a section as settings.sections answers: the
// declaration filled in (owner and title resolved) with its settings'
// names in page order (by group, each group in declaration order) and how
// many hold a session value.
type SettingSectionInfo struct {
	SettingSection
	Settings []string `json:"settings"`
	Session  int      `json:"session,omitempty"`
}

// settingLayout is the embedded file's shape.
type settingLayout struct {
	Owners   []SettingOwner   `json:"owners"`
	Sections []SettingSection `json:"sections"`
}

// frameworkLayout is settings/sections.json, read once. A file that does
// not parse is a build mistake the settings tests catch, so it panics.
var frameworkLayout = sync.OnceValue(func() settingLayout {
	data, err := settingFiles.ReadFile("settings/" + settingLayoutFile)
	if err != nil {
		panic(err)
	}
	var out settingLayout
	if err := DecodeJSONC(data, &out); err != nil {
		panic(fmt.Sprintf("settings/%s: %v", settingLayoutFile, err))
	}
	return out
})

// frameworkSettingLayout is a copy of the framework's owner and section
// declarations, for one runtime.
func frameworkSettingLayout() ([]SettingOwner, []SettingSection) {
	l := frameworkLayout()
	return slices.Clone(l.Owners), slices.Clone(l.Sections)
}

// DeclareSettingOwner declares owners of settings (tabs of the Settings
// page). Declaring an ID again replaces its title, description and order
// where it stands.
func (r *Runtime) DeclareSettingOwner(owners ...SettingOwner) error {
	s := r.Settings()
	for _, one := range owners {
		if !oneWord(one.ID) {
			return fmt.Errorf("setting owner %q must be one word", one.ID)
		}
		if i := slices.IndexFunc(s.owners, func(o SettingOwner) bool { return o.ID == one.ID }); i >= 0 {
			s.owners[i] = one
			continue
		}
		s.owners = append(s.owners, one)
	}
	s.relayer()
	return nil
}

// DeclareSettingSection declares sections of an owner's settings (rows of
// the Settings page's section list). Declaring an owner's section again
// replaces its title and description where it stands.
func (r *Runtime) DeclareSettingSection(sections ...SettingSection) error {
	s := r.Settings()
	for _, one := range sections {
		if !oneWord(one.ID) {
			return fmt.Errorf("setting section %q must be one word", one.ID)
		}
		if one.Owner != "" && !oneWord(one.Owner) {
			return fmt.Errorf("setting section %s: owner %q must be one word", one.ID, one.Owner)
		}
		if i := slices.IndexFunc(s.sections, func(d SettingSection) bool { return d.Owner == one.Owner && d.ID == one.ID }); i >= 0 {
			s.sections[i] = one
			continue
		}
		s.sections = append(s.sections, one)
	}
	s.relayer()
	return nil
}

func oneWord(word string) bool { return word != "" && !strings.ContainsAny(word, " \t\n") }

// ownerOf is the owner a declared setting files under.
func (s *Settings) ownerOf(one Setting) string { return cmp.Or(one.Owner, OwnerApp) }

// OwnerOf is the owner the named setting files under ("" for an unknown
// name).
func (s *Settings) OwnerOf(name string) string {
	if one := s.find(s.canonical(name)); one != nil {
		return s.ownerOf(*one)
	}
	return ""
}

// ownerTitle is an owner's title: its declared one, else, for the
// application's, the application's name, else the ID.
func (s *Settings) ownerTitle(o SettingOwner) string {
	if o.Title != "" {
		return o.Title
	}
	if o.ID == OwnerApp && s.config != nil && s.config.App != "" {
		return s.config.App
	}
	return o.ID
}

// sectionDecl is the declaration of an owner's section: the owner's own,
// else the one for every owner; its index in the declarations orders it
// (-1: none).
func (s *Settings) sectionDecl(owner, id string) (SettingSection, int) {
	at := slices.IndexFunc(s.sections, func(d SettingSection) bool { return d.Owner == owner && d.ID == id })
	if at < 0 {
		at = slices.IndexFunc(s.sections, func(d SettingSection) bool { return d.Owner == "" && d.ID == id })
	}
	if at < 0 {
		return SettingSection{Owner: owner, ID: id, Title: id}, -1
	}
	d := s.sections[at]
	d.Owner, d.Title = owner, cmp.Or(d.Title, id)
	return d, at
}

// sectionTitle is an owner's section's title.
func (s *Settings) sectionTitle(owner, id string) string {
	d, _ := s.sectionDecl(owner, id)
	return d.Title
}

// settingsOwnerTab is one owner with its sections in drawing order.
type settingsOwnerTab struct {
	Owner    SettingOwnerInfo
	Sections []SettingSectionInfo
}

// tabs is every owner holding a setting, in drawing order, each with its
// sections in order and each section's settings by group: the Settings
// page's layout and the answer of settings.owners and settings.sections.
func (s *Settings) tabs() []settingsOwnerTab {
	type place struct{ order, at int }
	var owners []string
	sections := map[string][]string{}
	members := map[string][]Setting{}
	for _, one := range s.declared {
		owner, section := s.ownerOf(one), one.SectionName()
		if !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
		key := owner + "\x00" + section
		if !slices.Contains(sections[owner], section) {
			sections[owner] = append(sections[owner], section)
		}
		members[key] = append(members[key], one)
	}
	ownerPlace := func(id string, first int) place {
		if i := slices.IndexFunc(s.owners, func(o SettingOwner) bool { return o.ID == id }); i >= 0 {
			return place{s.owners[i].Order, i}
		}
		return place{0, len(s.owners) + first}
	}
	firsts := map[string]int{}
	for i, id := range owners {
		firsts[id] = i
	}
	slices.SortStableFunc(owners, func(a, b string) int {
		pa, pb := ownerPlace(a, firsts[a]), ownerPlace(b, firsts[b])
		return cmp.Or(cmp.Compare(pa.order, pb.order), cmp.Compare(pa.at, pb.at))
	})
	out := make([]settingsOwnerTab, 0, len(owners))
	for _, id := range owners {
		decl := SettingOwner{ID: id}
		if i := slices.IndexFunc(s.owners, func(o SettingOwner) bool { return o.ID == id }); i >= 0 {
			decl = s.owners[i]
		}
		decl.Title = s.ownerTitle(decl)
		tab := settingsOwnerTab{Owner: SettingOwnerInfo{SettingOwner: decl}}
		ids := sections[id]
		at := map[string]int{}
		for i, sec := range ids {
			_, d := s.sectionDecl(id, sec)
			if d < 0 {
				d = len(s.sections) + i
			}
			at[sec] = d
		}
		slices.SortStableFunc(ids, func(a, b string) int { return cmp.Compare(at[a], at[b]) })
		for _, sec := range ids {
			d, _ := s.sectionDecl(id, sec)
			info := SettingSectionInfo{SettingSection: d}
			// Grouped settings sit together, the groups in the order of
			// their first member.
			// The settings with no group first, then each group.
			groups := []string{""}
			byGroup := map[string][]string{}
			for _, one := range members[id+"\x00"+sec] {
				if !slices.Contains(groups, one.Group) {
					groups = append(groups, one.Group)
				}
				byGroup[one.Group] = append(byGroup[one.Group], one.Name)
				if _, ok := s.session[one.Name]; ok {
					info.Session++
				}
			}
			for _, g := range groups {
				info.Settings = append(info.Settings, byGroup[g]...)
			}
			tab.Owner.Settings += len(info.Settings)
			tab.Owner.Session += info.Session
			tab.Sections = append(tab.Sections, info)
		}
		tab.Owner.Sections = len(tab.Sections)
		out = append(out, tab)
	}
	return out
}

// Owners is every owner holding a setting, in the Settings page's order.
func (s *Settings) Owners() []SettingOwnerInfo {
	out := []SettingOwnerInfo{}
	for _, tab := range s.tabs() {
		out = append(out, tab.Owner)
	}
	return out
}

// Sections is an owner's sections in the Settings page's order; owner ""
// is every owner's, owner by owner.
func (s *Settings) Sections(owner string) ([]SettingSectionInfo, error) {
	out := []SettingSectionInfo{}
	found := owner == ""
	for _, tab := range s.tabs() {
		if owner == "" || tab.Owner.ID == owner {
			found = true
			out = append(out, tab.Sections...)
		}
	}
	if !found {
		return nil, s.unknownOwner(owner)
	}
	return out, nil
}

func (s *Settings) unknownOwner(owner string) error {
	var ids []string
	for _, o := range s.Owners() {
		ids = append(ids, o.ID)
	}
	if len(ids) == 0 {
		return errors.New("this application declares no settings")
	}
	return fmt.Errorf("no settings owner %q: one of %s", owner, strings.Join(ids, ", "))
}

// registerSettingLayout registers settings.owners and settings.sections.
func registerSettingLayout(r *Runtime) {
	r.Register(Operation{Verb: "owners", Noun: "ochazuke.settings", Description: "List the owners of the settings (the Settings page's tabs) in order: id, title, description, and how many sections and settings each has", Result: "setting-owner[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().Owners(), nil
	}})
	r.Register(Operation{Verb: "sections", Noun: "ochazuke.settings", Description: "List an owner's sections of settings in the Settings page's order (every owner's without one): owner, id, title, description and the settings in each", Parameters: map[string]Parameter{
		"owner": {Type: "string", Description: "The owner (a tab of the Settings page): app, ochazuke, or one an application declares; default every owner"},
	}, Result: "setting-section[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Settings().Sections(StringArg(a, "owner", ""))
	}})
}
