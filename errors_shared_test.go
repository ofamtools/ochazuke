package ochazuke

import (
	"strings"
	"testing"
)

// One failure, one wording: a name that reaches no thing, an argument a
// verb does not take and a write to a read-only prop each answer with the
// shared error, did-you-mean included.
func TestSharedErrorWordings(t *testing.T) {
	r, _ := newWriteRuntime(t)
	for _, c := range []struct {
		q    Request
		want string
	}{
		// a key and a target (task show t9, CLI or MCP) say the same
		{Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t9"}}, `no task "t9": did you mean t1, t2, t3?; task list shows ids`},
		{Request{Noun: "task", Verb: "get", Target: "t9"}, `no task "t9": did you mean t1, t2, t3?; task list shows ids`},
		{Request{Noun: "task", Verb: "get", Target: "zzzz"}, `no task "zzzz"; task list shows ids`},
		{Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "prpo": "title"}}, `task.get has no parameter "prpo": did you mean prop?; it takes id, prop, props`},
		{Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "id", "value": "t9"}}, `task id is read-only: it is the key`},
	} {
		_, err := r.Execute(c.q)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.q, err, c.want)
		}
	}
}

// A verb's sub-object keeps its known keys and warns about the rest, with
// the close names; it never refuses them.
func TestDecodeKnownWarns(t *testing.T) {
	var lane struct {
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	warnings, err := DecodeKnown([]byte(`{"title": "x", "statsu": "done", "zzz": 1}`), &lane, "lane")
	if err != nil || lane.Title != "x" {
		t.Fatalf("%+v %v", lane, err)
	}
	want := []string{`unknown key "statsu" in lane: did you mean status?`, `unknown key "zzz" in lane`}
	if strings.Join(warnings, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings %q", warnings)
	}
}

// A noun alias an application declares is a keyword picker rows of the
// noun answer to (the help's place list finds the terminal by live),
// without changing the name it is drawn by (NoDisplay); picker.keywords
// off matches the drawn label only.
func TestPlaceRowsAnswerToNounAliases(t *testing.T) {
	r, _ := placedRowsFixture(t)
	if err := r.AddNounAlias(NounAlias{Alias: "qx", Noun: "tile", NoDisplay: true}); err != nil {
		t.Fatal(err)
	}
	if got := r.DisplayNoun("tile"); got != "tile" {
		t.Fatalf("drawn as %q", got)
	}
	var choices []Choice
	for _, row := range r.HelpPlaceRows("normal") {
		choices = append(choices, Choice{ID: row.Label, Label: row.Label, Keywords: row.Keywords, RowFlags: RowFlags{Bare: true}})
	}
	for _, on := range []bool{true, false} {
		if err := r.Settings().Set(PickerKeywordsSetting, on); err != nil {
			t.Fatal(err)
		}
		c, err := r.OpenChooser(ChooserSpec{Kind: "keywords-test", Rows: ChoiceRows(choices)})
		if err != nil {
			t.Fatal(err)
		}
		c.SetQuery("qx")
		found := false
		for _, row := range c.matches {
			found = found || strings.Contains(row.Label, "tile")
		}
		if found != on {
			t.Fatalf("picker.keywords %v: qx found the tile's place %v (%d rows)", on, found, len(c.matches))
		}
		if err := c.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Every unknown-setting path answers with the did-you-mean form, the set
// verb's missing value with its usage.
func TestUnknownSettingOneWording(t *testing.T) {
	r := NewRuntime()
	for _, c := range []struct {
		q    Request
		want string
	}{
		{Request{Noun: "setting", Verb: "set", Args: Arguments{"name": "modal-dimm", "value": "1"}}, "unknown setting modal-dimm: did you mean modal-dim?; setting list shows all"},
		{Request{Noun: "setting", Verb: "get", Args: Arguments{"name": "nosuch"}}, "unknown setting nosuch; setting list shows all"},
		{Request{Noun: "setting", Verb: "set", Args: Arguments{"name": "modal-dim"}}, "value is required: setting set NAME VALUE (or toggle a boolean)"},
	} {
		if _, err := r.Execute(c.q); err == nil || err.Error() != c.want {
			t.Errorf("%+v: %v, want %q", c.q.Args, err, c.want)
		}
	}
}
