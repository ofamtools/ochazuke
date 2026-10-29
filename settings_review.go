package ochazuke

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The review of this session's settings (settings.review): a modal over
// whatever is open, one row per setting whose session value differs from
// the persisted one (settings.diff), with the saved value and where it is
// saved, the session value and where it came from side by side. Under the
// rows the selected setting's description, its default and what writing
// or reverting it changes. The rows are the setting-change noun; their
// keys run the setting noun's own verbs with the row's name
// (bindings/settings-editor.json: setting.write, setting.revert, a skip
// that moves on), and the modal's run the all-at-once verbs
// (settings.write, settings.revert). A row that is written or reverted
// drops out as the layers move (the same refresh the Settings page uses);
// when none is left the review closes and says so.
//
// It is a modal, not a page: it is a short decision over values that
// belong to wherever the user was (opened from the palette, a key, the
// Settings page), and closing it returns to that focus and mode, as the
// confirm and results modals do. Vim has no counterpart: its :set values
// are never compared with the vimrc; :mkvimrc writes them all at once.
const (
	settingsReviewID     = "settings-review"
	settingsReviewNoun   = "ochazuke.settings-review"
	settingsReviewListID = "__settings-review/list"
	settingChangeNoun    = "ochazuke.setting-change"
	settingChangePrefix  = "__settings-review/row/"
)

// noSessionReview is settings.review's answer in a runtime without a
// session (a one-shot CLI process).
const noSessionReview = "no session to review: a CLI process holds no session values, so every write already went to " + SettingsFile + ". In a running app, settings review (or settings diff) shows the session values."

// registerSettingsReview registers settings.review, the review's nouns
// and the refresh that drops reviewed rows.
func registerSettingsReview(r *Runtime) {
	r.Define(settingsReviewNoun)
	r.Define(settingChangeNoun)
	r.Register(Operation{Verb: "review", Noun: "ochazuke.settings", Description: "Review this session's settings: each session value beside its saved one, to write or revert one at a time or all at once", Result: "setting-diff[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if !s.HasSession() {
			return noSessionReview, nil
		}
		diff := s.Diff()
		if len(diff) == 0 {
			r.UI().Info("No session changes to review")
			return nil, nil
		}
		if r.Instances[settingsReviewID] == nil {
			if _, err := r.OpenModal(settingsReviewID, Payload{}); err != nil {
				return nil, err
			}
			if _, err := r.AddList(settingsReviewListID, nil, Parent(settingsReviewID), NoMarks(), Columns(settingsReviewColumns...)); err != nil {
				_ = r.Pop()
				return nil, err
			}
		}
		list := r.List(settingsReviewListID)
		if err := list.SetItems(settingsReviewItems(s, diff)); err != nil {
			return nil, err
		}
		if err := list.Focus(); err != nil {
			return nil, err
		}
		return diff, nil
	}})
	r.Settings().Observe("", func(string, any) { refreshSettingsReview(r) })
}

// settingsReviewColumns are the review's table: the name, the saved value
// and where it is saved, the session value, and its origin, which gives
// way first on a narrow screen (the detail repeats it).
var settingsReviewColumns = []Column{
	{Title: "Setting", Min: 8, Max: 30},
	{Title: "Saved", Min: 8, Max: 24, Priority: 1},
	{Title: "Session", Min: 8, Max: 24, Priority: 1},
	{Title: "Origin", Min: 10, Priority: 2},
}

// settingsReviewItems are the review's rows, one per diff entry; a row's
// Data is the entry (its name feeds the row keys' name argument), and the
// session value is drawn in the Settings page's session tone.
func settingsReviewItems(s *Settings, diff []SettingDiff) []ListItem {
	items := make([]ListItem, len(diff))
	for i, d := range diff {
		saved := reviewCell(d.Saved) + " (" + d.SavedIn + ")"
		session := reviewCell(d.Session)
		items[i] = ListItem{
			ID: settingChangePrefix + d.Name, Label: d.Name, Noun: settingChangeNoun, Data: d,
			Cells: []string{d.Name, saved, session, d.Origin.String()},
			Tones: []ToneSpan{SpanTone(2, 0, len([]rune(session)), ToneSession)},
		}
	}
	return items
}

// reviewCell is a value as the review shows it: a list's items
// comma-separated, (empty) for empty text, (none) for an empty list.
func reviewCell(v any) string {
	if list, ok := v.([]string); ok && len(list) == 0 {
		return "(none)"
	}
	return textCell(settingCell(v))
}

// refreshSettingsReview brings the open review up to date when a layer
// moves: a written or reverted row drops out, the selection staying at its
// place. With no row left the review closes (when it is the top modal; a
// confirm above it closes first) and says so.
func refreshSettingsReview(r *Runtime) {
	list := r.List(settingsReviewListID)
	if list == nil {
		return
	}
	diff := r.Settings().Diff()
	_ = list.SetItems(settingsReviewItems(r.Settings(), diff))
	if len(diff) == 0 && r.TopModal() == settingsReviewID {
		if err := r.Pop(); err == nil {
			r.UI().Info("No session changes left to review")
		}
	}
}

// settingsReviewView draws the review: a count line, the rows, and under
// them the selected setting's detail.
func settingsReviewView(r *Runtime, _ *Instance, width, height int) Block {
	list := r.List(settingsReviewListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	if list.Len() == 0 {
		return Text("No session changes left to review.")
	}
	head := fmt.Sprintf("%d session values differ from the saved ones", list.Len())
	if list.Len() == 1 {
		head = "1 session value differs from the saved one"
	}
	lines := []string{ansi.Truncate(head, width, "…")}
	var detail []string
	if item, ok := list.Selected(); ok {
		if d, ok := item.Data.(SettingDiff); ok {
			for _, line := range settingReviewDetail(r.Settings(), d) {
				detail = append(detail, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
			}
		}
	}
	// The rows take what they need (the header line included) and leave
	// the detail at least half the rest, a rule between them.
	rest := height - len(lines)
	rows := min(list.Len()+1, rest)
	if len(detail) > 0 {
		rows = min(rows, max(2, rest-1-min(len(detail), rest/2)))
	}
	if rows > 0 {
		lines = append(lines, strings.Split(list.View(width, rows), "\n")...)
	}
	if room := height - len(lines) - 1; room > 0 && len(detail) > 0 {
		lines = append(lines, strings.Repeat("─", width))
		lines = append(lines, detail[:min(room, len(detail))]...)
	}
	return Text(strings.Join(lines, "\n"))
}

// settingReviewDetail is the selected row's detail: the name and
// description, the default and the origin, and what writing or reverting
// changes.
func settingReviewDetail(s *Settings, d SettingDiff) []string {
	one := s.find(d.Name)
	if one == nil {
		return []string{d.Name}
	}
	title := d.Name
	if one.Description != "" {
		title += ": " + one.Description
	}
	def := reviewCell(fileForm(one.Default))
	saved, session := reviewCell(d.Saved), reviewCell(d.Session)
	origin := d.Origin.String()
	lines := []string{title, "Default " + def + ". " + strings.ToUpper(origin[:1]) + origin[1:] + "."}
	switch {
	case s.session[d.Name].reset:
		lines = append(lines, "Writing removes "+d.Name+" from "+SettingsFile+", so the default "+def+" applies; reverting shows "+saved+" again.")
	case d.SavedIn == OriginFile:
		lines = append(lines, "Writing puts "+session+" in "+SettingsFile+" in place of "+saved+"; reverting shows "+saved+" again.")
	default:
		lines = append(lines, "Writing adds "+session+" to "+SettingsFile+" (it holds no value, so the default applies now); reverting shows "+saved+" again.")
	}
	return lines
}
