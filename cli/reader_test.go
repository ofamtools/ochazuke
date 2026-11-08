package cli

import (
	"context"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/internal/testenv"
	"ofam.dev/oden"
)

// recorder is a verb handler that records its key and arguments.
func recorder(ran *[]string, key string) func(ochazuke.Ctx) ochazuke.Result {
	return func(c ochazuke.Ctx) ochazuke.Result {
		*ran = append(*ran, key+" "+argsText(c.Args))
		return ochazuke.Done("ok")
	}
}

// The owner's examples on the CLI: a pair of prefixes the rest of the
// line settles (t r up), two readings listed as noun verb pairs (t t), an
// alias read verb first (o-questions add is question ask).
func TestReaderOwnerExamplesOnTheCLI(t *testing.T) {
	testenv.ClearActor(t)
	var ran []string
	r := ochazuke.NewRuntime()
	name := map[string]ochazuke.Parameter{"name": {Type: "string", Enum: []string{"upper", "lower"}}}
	for _, noun := range []string{"row", "cell"} {
		key := ochazuke.KeyOf(noun, "transform")
		r.Register(ochazuke.Operation{Key: key, Parameters: name, Positional: []string{"name"}, Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
			ran = append(ran, key.Short()+" "+argsText(a))
			return "ok", nil
		}})
	}
	r.Define("text").Verb("tidy", recorder(&ran, "text.tidy"), "Tidy")
	r.Define("tape").Verb("toggle", recorder(&ran, "tape.toggle"), "Toggle")
	r.Define("question").Main().Verb("ask", recorder(&ran, "question.ask"), "Ask").Alias("add")
	for _, c := range []struct {
		args     []string
		ran, err string
	}{
		{args: []string{"t", "r", "upper"}, ran: "row.transform name=upper"},
		{args: []string{"t", "t"}, err: `"t t" is ambiguous: tape toggle, text tidy`},
		{args: []string{"add"}, ran: "question.ask "},
		{args: []string{"q", "add"}, ran: "question.ask "},
	} {
		ran = nil
		_, errOut, err := oXNamed(t, "o-questions", r, c.args...)
		got := strings.Join(ran, ";")
		if got != c.ran || c.err != "" && !strings.Contains(errOut, c.err) || (c.err == "") != (err == nil) {
			t.Errorf("o-questions %s: ran %q err %v\n%s", strings.Join(c.args, " "), got, err, errOut)
		}
	}
}

// A noun's default action (Noun.DefaultAction) runs when the noun's words
// are followed by no action word, or only flags: review --web is review
// page --web. Help still lists the noun's verbs.
func TestNounDefaultActionOnTheCLI(t *testing.T) {
	testenv.ClearActor(t)
	var ran []string
	r := ochazuke.NewRuntime()
	r.Define("review").DefaultAction("page").
		Verb("page", recorder(&ran, "review.page"), "Open the page").
		Verb("list", recorder(&ran, "review.list"), "List")
	op := r.Operations.At("review.page")
	op.Parameters = map[string]ochazuke.Parameter{"web": {Type: "boolean"}}
	r.Operations.Set(op)
	for _, c := range []struct {
		args []string
		ran  string
	}{
		{[]string{"review"}, "review.page "},
		{[]string{"review", "--web"}, "review.page web=true"},
		{[]string{"review", "list"}, "review.list "},
	} {
		ran = nil
		_, errOut, err := oXNamed(t, "o-x", r, c.args...)
		if err != nil || strings.Join(ran, ";") != c.ran {
			t.Errorf("o-x %s: ran %v err %v\n%s", strings.Join(c.args, " "), ran, err, errOut)
		}
	}
	ran = nil
	if out, _, _ := oXNamed(t, "o-x", r, "review", "--help"); len(ran) != 0 || !strings.Contains(out, "list") {
		t.Errorf("review --help ran %v\n%s", ran, out)
	}
}

// An application command named like a mounted noun no longer hides the
// noun's verbs silently: a command with children gains the noun's verbs it
// lacks; a command with none says on stderr that it hides them.
func TestAppCommandNamedLikeANoun(t *testing.T) {
	testenv.ClearActor(t)
	var ran []string
	r := ochazuke.NewRuntime()
	r.Define("review").Verb("list", recorder(&ran, "review.list"), "List").Verb("show", recorder(&ran, "review.show"), "Show")
	run := func(group oden.Command, args ...string) (string, error) {
		var out, errOut strings.Builder
		app := oden.New("o-x", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
		app.Add(group)
		Mount(app, New(r, "o-x", strings.NewReader(""), &out, &errOut))
		err := app.Run(context.Background())
		return errOut.String(), err
	}
	mine := oden.Command{Name: "show", Summary: "the app's own show", Run: func(context.Context, []string) error { ran = append(ran, "app.show"); return nil }}
	if _, err := run(oden.Command{Name: "review", Children: []oden.Command{mine}}, "review", "list"); err != nil || strings.Join(ran, ";") != "review.list " {
		t.Fatalf("merged review list: ran %v err %v", ran, err)
	}
	ran = nil
	if _, err := run(oden.Command{Name: "review", Children: []oden.Command{mine}}, "review", "show"); err != nil || strings.Join(ran, ";") != "app.show" {
		t.Fatalf("merged review show: the app's own child wins; ran %v err %v", ran, err)
	}
	ran = nil
	errOut, _ := run(oden.Command{Name: "review", Run: func(context.Context, []string) error { ran = append(ran, "app.review"); return nil }}, "review")
	if !strings.Contains(errOut, `the command "review" hides the noun review`) {
		t.Fatalf("leaf review: no warning\n%s", errOut)
	}
}
