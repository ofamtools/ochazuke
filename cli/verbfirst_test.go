package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/internal/testenv"
	"ofam.dev/oden"
)

// verbFirstRuntime is the runtime of a bin o-x: nouns x (its main noun,
// Noun.Main) and agent. add and show are verbs of both; define, state
// and spawn of one; answer takes its subject as a parameter called target.
func verbFirstRuntime(t *testing.T) (*ochazuke.Runtime, *[]string) {
	t.Helper()
	testenv.ClearActor(t)
	r := ochazuke.NewRuntime()
	var ran []string
	say := func(label string) func(ochazuke.Ctx) ochazuke.Result {
		return func(c ochazuke.Ctx) ochazuke.Result {
			ran = append(ran, label+" "+jsonOf(c.Args))
			return ochazuke.Done(label)
		}
	}
	r.Define("x").Main().
		Verb("define", say("x.define"), "Define an x").
		Verb("state", say("x.state"), "State of x").
		Verb("add", say("x.add"), "Add an x").
		Verb("show", say("x.show"), "Show an x")
	r.Define("agent").
		Verb("add", say("agent.add"), "Add an agent").
		Verb("show", say("agent.show"), "Show an agent").
		Verb("spawn", say("agent.spawn"), "Spawn an agent")
	r.Register(ochazuke.Operation{Key: "x.answer", Description: "Answer an x",
		Parameters: map[string]ochazuke.Parameter{"target": {Type: "string"}, "answer": {Type: "string"}},
		Positional: []string{"target", "answer"},
		Run: func(_ *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			ran = append(ran, "x.answer "+jsonOf(args))
			return "ok", nil
		}})
	return r, &ran
}

func jsonOf(a ochazuke.Arguments) string {
	keys := make([]string, 0, len(a))
	for k, v := range a {
		keys = append(keys, k+"="+strings.TrimSpace(strings.ReplaceAll(fmtAny(v), " ", "_")))
	}
	return strings.Join(sortedStrings(keys), ",")
}

// oX runs one invocation of the bin o-x with builtins, as a real tool has.
func oX(t *testing.T, r *ochazuke.Runtime, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut strings.Builder
	app := oden.New("o-x", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
	sh := New(r, "o-x", strings.NewReader(""), &out, &errOut)
	Mount(app, sh)
	err := app.Run(context.Background())
	return out.String(), errOut.String(), err
}

func TestVerbFirstReadsAVerbAlone(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		ran  string // the handler that saw it, "" when none
		err  string // text expected on stderr or in the error
	}{
		{name: "unique verb", args: []string{"define"}, ran: "x.define"},
		{name: "unique verb prefix", args: []string{"spaw"}, ran: "agent.spawn"},
		{name: "shared verb, second word picks the noun", args: []string{"add", "agent"}, ran: "agent.add"},
		{name: "shared verb, noun prefix", args: []string{"show", "ag"}, ran: "agent.show"},
		{name: "shared verb, namespace tie-break", args: []string{"add"}, ran: "x.add"},
		{name: "a machine builtin gives way to the noun's verb", args: []string{"state"}, ran: "x.state"},
		{name: "the long spelling is unchanged", args: []string{"agent", "add"}, ran: "agent.add"},
		{name: "the long spelling of a builtin name", args: []string{"x", "state"}, ran: "x.state"},
		{name: "a bare word that is no verb", args: []string{"frobnicate"}, err: "not found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, ran := verbFirstRuntime(t)
			_, errOut, err := oX(t, r, c.args...)
			got := ""
			if len(*ran) > 0 {
				got = strings.Fields((*ran)[0])[0]
			}
			if got != c.ran || (c.err != "") != (err != nil) || !strings.Contains(errOut+errText(err), c.err) {
				t.Fatalf("o-x %s: ran %v, err %v\n%s", strings.Join(c.args, " "), *ran, err, errOut)
			}
		})
	}
}

// The tie-break is data: the main noun (Noun.Main). A tool that declares
// none leaves the shared verb ambiguous, listing the noun verb pairs.
func TestVerbFirstAmbiguityListsPairs(t *testing.T) {
	r, ran := verbFirstRuntime(t)
	r.Nouns["x"].Main = false
	out, errOut, err := oXNamed(t, "o-zzz", r, "show")
	if err == nil || len(*ran) != 0 || !strings.Contains(errOut, `"show" is ambiguous: agent show, x show`) {
		t.Fatalf("o-zzz show: ran %v err %v\n%s%s", *ran, err, out, errOut)
	}
}

func oXNamed(t *testing.T, name string, r *ochazuke.Runtime, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut strings.Builder
	app := oden.New(name, args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
	Mount(app, New(r, name, strings.NewReader(""), &out, &errOut))
	err := app.Run(context.Background())
	return out.String(), errOut.String(), err
}

// Flags, positionals and help after a verb-first word parse as they do
// after the long spelling.
func TestVerbFirstArgumentsAndHelpMatchTheLongSpelling(t *testing.T) {
	r, _ := verbFirstRuntime(t)
	long, _, err := oX(t, r, "x", "answer", "--target", "q1", "text")
	if err != nil {
		t.Fatal(err)
	}
	short, _, err := oX(t, r, "answer", "--target", "q1", "text")
	if err != nil || short != long {
		t.Fatalf("answer: %q (%v), want %q", short, err, long)
	}
	for _, words := range [][]string{{"state", "--help"}, {"define", "--help"}} {
		longHelp, _, _ := oX(t, r, append([]string{"x"}, words...)...)
		shortHelp, _, _ := oX(t, r, words...)
		if longHelp == "" || shortHelp != longHelp {
			t.Fatalf("o-x %s help:\n%s\nwant\n%s", strings.Join(words, " "), shortHelp, longHelp)
		}
	}
}

// cli.verb-first off keeps the BIN NOUN VERB spelling alone.
func TestVerbFirstSettingTurnsItOff(t *testing.T) {
	r, ran := verbFirstRuntime(t)
	if err := r.Settings().Set(ochazuke.CLIVerbFirstSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := oX(t, r, "define"); err == nil || len(*ran) != 0 || !strings.Contains(errOut, "not found") {
		t.Fatalf("o-x define with the setting off: ran %v err %v\n%s", *ran, err, errOut)
	}
	if _, _, err := oX(t, r, "x", "define"); err != nil || len(*ran) != 1 {
		t.Fatalf("o-x x define: %v %v", err, *ran)
	}
}

// A namespace group reads a verb of its nouns: lane state is lane lane state.
func TestVerbFirstInsideANamespaceGroup(t *testing.T) {
	testenv.ClearActor(t)
	r := ochazuke.NewRuntime()
	var ran []string
	say := func(label string) func(ochazuke.Ctx) ochazuke.Result {
		return func(ochazuke.Ctx) ochazuke.Result { ran = append(ran, label); return ochazuke.Done(label) }
	}
	r.Define("lane.lane").Main().Verb("state", say("lane.lane.state"), "State").Verb("define", say("lane.lane.define"), "Define")
	r.Define("lane.plan").Verb("define", say("lane.plan.define"), "Define a plan").Verb("show", say("lane.plan.show"), "Show a plan")
	for _, c := range []struct {
		args []string
		ran  string
	}{
		{[]string{"lane", "state"}, "lane.lane.state"},
		{[]string{"lane", "lane", "state"}, "lane.lane.state"},
		{[]string{"lane", "define"}, "lane.lane.define"},
		{[]string{"lane", "define", "plan"}, "lane.plan.define"},
		{[]string{"lane", "show"}, "lane.plan.show"},
	} {
		ran = nil
		_, errOut, err := oXNamed(t, "mtm", r, c.args...)
		if err != nil || len(ran) != 1 || ran[0] != c.ran {
			t.Fatalf("mtm %s: ran %v err %v\n%s", strings.Join(c.args, " "), ran, err, errOut)
		}
	}
}

// --target fills a parameter the operation calls target, as the first bare
// word does; before, a bare word filled it and the flag's value was lost.
func TestTargetFlagFillsADeclaredTargetParameter(t *testing.T) {
	r, ran := verbFirstRuntime(t)
	for _, args := range [][]string{
		{"x", "answer", "q1", "the text"},
		{"x", "answer", "--target", "q1", "the text"},
		{"x", "answer", "the text", "--target", "q1"},
	} {
		*ran = nil
		if _, errOut, err := oX(t, r, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut)
		}
		if len(*ran) != 1 || (*ran)[0] != "x.answer answer=the_text,target=q1" {
			t.Fatalf("%v: handler saw %v", args, *ran)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func fmtAny(v any) string { return fmt.Sprint(v) }

func sortedStrings(s []string) []string { sort.Strings(s); return s }

// An extension's noun whose last part is a framework short name (ai.setting)
// does not capture the short name: `tool setting show` reaches the framework's
// ochazuke.setting, and ai.setting is reached by its namespace.
func TestFrameworkShortNameIsNotCapturedByANamespacedTail(t *testing.T) {
	testenv.ClearActor(t)
	r := ochazuke.NewRuntime()
	var ran []string
	r.Define("ai.setting").Verb("show", func(ochazuke.Ctx) ochazuke.Result {
		ran = append(ran, "ai.setting.show")
		return ochazuke.Done("ok")
	}, "Show an ai setting")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"ai.setting", "show"}, "ai.setting.show"},
		{[]string{"ai", "setting", "show"}, "ai.setting.show"},
		{[]string{"ai", "show"}, "ai.setting.show"},
	} {
		ran = nil
		_, errOut, err := oXNamed(t, "mtm", r, c.args...)
		if err != nil || len(ran) != 1 || ran[0] != c.want {
			t.Errorf("mtm %s: ran %v err %v\n%s", strings.Join(c.args, " "), ran, err, errOut)
		}
	}
	for _, args := range [][]string{{"setting", "show", "--name", "daemon.runner"}, {"setting", "show"}, {"show", "setting"}} {
		ran = nil
		_, errOut, _ := oXNamed(t, "mtm", r, args...)
		if len(ran) != 0 || strings.Contains(errOut, "ai.setting") {
			t.Errorf("mtm %s ran %v: the extension's noun captured the framework short name\n%s", strings.Join(args, " "), ran, errOut)
		}
	}
}

// The palette's reader (every operation) already ranks the framework's alias
// above a namespaced noun's tail; MCP run names keys, and a short key reads
// through CanonicalKey, which also reaches the framework noun.
func TestFrameworkShortNameWinsInThePaletteAndKeys(t *testing.T) {
	testenv.ClearActor(t)
	r := ochazuke.NewRuntime()
	r.Define("ai.setting").Verb("show", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("ok") }, "Show an ai setting")
	cs := r.ReadingWithin(nil).Read([]string{"setting", "show"})
	if len(cs) == 0 || cs[0].Operation.Noun() != "ochazuke.setting" {
		t.Fatalf("palette reads setting show as %v", cs)
	}
	if got := r.CanonicalKey("setting.show"); got != "ochazuke.setting.show" {
		t.Errorf("CanonicalKey(setting.show) = %s", got)
	}
	if got := r.CanonicalKey("ai.setting.show"); got != "ai.setting.show" {
		t.Errorf("CanonicalKey(ai.setting.show) = %s", got)
	}
}
