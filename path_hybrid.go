package ochazuke

import (
	"context"
	"strings"
)

// The hybrid style of the directory picker (setting pickers.directory.style):
// one list in sections that are all on show at once, with no source to
// cycle. Recent (the application's recent places, frecent first when it
// says how often and when), then a section for each application source that
// declares one (PickerSource.Section: a project's folders, the bases), then
// Browse, the folder being looked at. The answer is on screen before
// anything is typed; typing filters every section; ] and [ jump between
// sections and headings never take the highlight. The Browse section is the
// browse source's navigation (browseNav), so going up reaches "/" from
// wherever the picker started, and the other sections are starting points.

// PathSourceHybrid names the hybrid style's source.
const PathSourceHybrid = "hybrid"

// PathSectionLimitSetting is how many rows a section shows while the query
// is empty.
const PathSectionLimitSetting = "pickers.directory.section-limit"

const (
	pathSectionRecent = "recent"
	pathSectionBrowse = "browse"
	// pathSectionCap bounds a section's rows while a query is typed (the
	// chooser's match narrows them).
	pathSectionCap = 200
)

// sectionHeader is a section's heading row.
func sectionHeader(section, title string) ChooserRow {
	return ChooserRow{ID: "section:" + section, Header: true, Section: section, Label: title,
		Tones: []ToneSpan{{Cell: 0, Tone: ToneHeader}}}
}

// rowsOfResult runs a source's load to its rows, on the calling goroutine:
// pending work and the rest of a streamed batch run here, up to limit rows.
func rowsOfResult(ctx context.Context, res Result, limit int) ([]ChooserRow, error) {
	var out []ChooserRow
	for {
		value, err := res.value, res.err
		if res.work != nil {
			value, err = res.work(ctx)
		}
		if err != nil {
			return out, err
		}
		rows, more, _ := chooserRowsOf(value)
		out = append(out, rows...)
		if more.work == nil && more.value == nil && more.err == nil || len(out) >= limit || ctx.Err() != nil {
			return out, ctx.Err()
		}
		res = more
	}
}

// hybridSource is the hybrid style's source; sections are the application
// sources that declare a Section, in the order they were added.
func (r *Runtime) hybridSource(pick PathPick, st *pathState, use func(string) Result, sections []ChooserSource) ChooserSource {
	nav := r.newBrowseNav(pick.Start)
	src := ChooserSource{
		Name:    PathSourceHybrid,
		Stands:  []string{PathSourceBrowse, PathSourceRecent},
		Live:    true,
		Matched: true,
		Empty:   "nothing here",
		Label:   func() string { return PathSourceHybrid + " " + displayPath(nav.dir) },
	}
	src.Load = func(query string) Result {
		at, asked := nav.dir, nav.asked
		hidden, caseMode, enterPicks := st.hidden, r.SearchCase(), st.enterPicks
		limit := r.settingInt(PathSectionLimitSetting, 5)
		listable := asked || r.autoListable(at)
		recent := r.recentList(pick)
		recentDirs := r.settingOn(PathRecentDirsSetting)
		prefixes := r.noAutoListPrefixes()
		typed := strings.TrimSpace(query) != ""
		recentCap := limit
		if typed {
			recentCap = pathSectionCap
		}
		loads := make([]func() Result, len(sections))
		for i, s := range sections {
			loads[i] = func() Result { return s.Load(query) }
		}
		return Pending(func(ctx context.Context) (any, error) {
			var rows []ChooserRow
			add := func(section, title string, list []ChooserRow, cap int) {
				if !typed && len(list) > cap {
					list = list[:cap]
				}
				if len(list) > pathSectionCap {
					list = list[:pathSectionCap]
				}
				if len(list) == 0 {
					return
				}
				rows = append(rows, sectionHeader(section, title))
				for _, row := range list {
					row.Section = section
					// A place can sit in two sections: the row's id is its
					// section's.
					row.ID = section + ":" + row.ID
					if row.Opens && !pick.Files() {
						row.Opens = false
					}
					rows = append(rows, row)
				}
			}
			add(pathSectionRecent, "Recent", recentPathRows(recent, pick, recentDirs, prefixes, recentCap), limit)
			for i, s := range sections {
				list, err := rowsOfResult(ctx, loads[i](), pathSectionCap+1)
				if err != nil && len(list) == 0 {
					continue
				}
				add("app:"+s.Name, s.Section, list, firstPositive(s.SectionLimit, limit))
			}
			rows = append(rows, sectionHeader(pathSectionBrowse, "Browse "+displayPath(at)+hiddenNote(hidden)))
			switch {
			case !listable:
				rows = append(rows, ChooserRow{ID: "browse:unlisted", Section: pathSectionBrowse, Label: "not listed on its own (path.no-auto-list): open it or type a path", NoPick: true, Refuse: "not listed on its own (path.no-auto-list): open it or type a path"})
			default:
				listed, err := browseListing(at, pick, hidden, caseMode, enterPicks)
				if err != nil {
					rows = append(rows, ChooserRow{ID: "browse:error", Section: pathSectionBrowse, Label: err.Error(), NoPick: true, Refuse: err.Error()})
				}
				for _, row := range listed {
					row.Section = pathSectionBrowse
					rows = append(rows, row)
				}
			}
			if pick.Dir() {
				rows = append(rows, ChooserRow{ID: at, Label: "use " + displayPath(at), Data: at, Fixed: true})
			}
			return rows, nil
		}).Label("hybrid " + displayPath(at))
	}
	src.Open = func(row ChooserRow) (string, bool) {
		path, ok := row.Data.(string)
		if !ok {
			return "", false
		}
		nav.open(expandPath(path, pick.Start))
		return "", true
	}
	src.CanOpen = func(row ChooserRow) bool { return folderRow(pick, row) }
	src.UpFrom = func(row ChooserRow, _ string) (string, bool) {
		// A row listed in the folder shown (its id is its path) goes up
		// from that folder; a row of another section is a place of its
		// own (its id carries the section), and up goes above it.
		if path, ok := row.Data.(string); ok && row.ID != path && !row.Fixed {
			return "", nav.upFrom(expandPath(path, pick.Start))
		}
		return "", nav.up()
	}
	src.Goto = func(to string) (string, bool) { return "", nav.goTo(to) }
	src.Landing = func() string { return nav.landing(pathSectionBrowse) }
	if pick.Dir() {
		src.Current = func() (ChooserRow, bool) {
			return ChooserRow{ID: nav.dir, Label: "use " + displayPath(nav.dir), Data: nav.dir}, true
		}
	}
	src.Make = nav.make
	src.MakeLabel = func() string { return "new folder in " + displayPath(nav.dir) }
	if pick.Parameter.New && !pick.Dir() {
		src.Create = func(query string) Result { return use(expandPath(query, nav.dir)) }
		src.CreateLabel = func(query string) string { return "new " + displayPath(expandPath(query, nav.dir)) }
	}
	return src
}

func hiddenNote(hidden bool) string {
	if hidden {
		return "  (hidden shown)"
	}
	return ""
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
