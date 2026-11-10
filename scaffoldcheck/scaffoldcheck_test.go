package scaffoldcheck_test

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/scaffoldcheck"
)

// A noun declared with props (key, label, archive, a path) over a DocStore
// passes every check; a planted noun lacking a label and an archive prop
// fails exactly those, and its delete may not be an agent's.
func TestDeclarationAndDocStorePass(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("chore").Props(
		ochazuke.PropSpec{Name: "id", Key: true, CreateOnly: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "file", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath}},
		ochazuke.PropSpec{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean}},
	).Store(ochazuke.DocStore(ochazuke.NewMemDocTable(r, "chore"), "chore"))
	r.Define("bare").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "note"}).Store(ochazuke.DocStore(ochazuke.NewMemDocTable(r, "bare"), "bare"))
	var got []string
	for _, p := range scaffoldcheck.Problems(scaffoldcheck.Config{Runtime: r}) {
		got = append(got, p.Row())
	}
	want := []string{"bare\tlabel", "bare\tarchive", "bare\trestore"}
	if !slices.Equal(got, want) {
		t.Fatalf("problems %q, want %q", got, want)
	}
}

// ochazuke's own record nouns, held to the baseline that only shrinks.
func TestFrameworkNounsScaffold(t *testing.T) {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	scaffoldcheck.Check(t, scaffoldcheck.Config{Runtime: r, Baseline: "testdata/scaffoldcheck.txt", Keep: func(*ochazuke.Noun) bool { return true }})
}

// The scaffold's verbs, generated keys and show-form sections are checked
// too; a noun that declines the generated keys passes keys; a delete an
// agent runs only with the owner's confirmation (agents.rights confirm) is
// not an agent delete, a plain allow is.
func TestScaffoldVerbsKeysAndDeleteRights(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	props := []ochazuke.PropSpec{
		{Name: "id", Key: true, CreateOnly: true},
		{Name: "title", Role: ochazuke.RoleLabel},
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean}},
	}
	r.Define("pane").Props(props...).Store(ochazuke.DocStore(ochazuke.NewMemDocTable(r, "pane"), "pane"))
	r.Define("quiet").DefaultKeys(false).Props(props...).Store(ochazuke.DocStore(ochazuke.NewMemDocTable(r, "quiet"), "quiet"))
	r.Define("loose").Props(props...).Store(ochazuke.DocStore(ochazuke.NewMemDocTable(r, "loose"), "loose"))
	if err := r.Settings().Set(ochazuke.AgentRightsSetting, `[{"match":"pane.delete","right":"confirm"},{"match":"loose.delete","right":"allow"}]`); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range scaffoldcheck.Problems(scaffoldcheck.Config{Runtime: r}) {
		got = append(got, p.Row())
	}
	if want := []string{"loose\tdelete"}; !slices.Equal(got, want) {
		t.Fatalf("problems %q, want %q", got, want)
	}
}
