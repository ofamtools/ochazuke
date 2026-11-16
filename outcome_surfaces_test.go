package ochazuke

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// outcomeFixture has one verb of each outcome kind: tag.use is done,
// tag.keep nothing / kept, tag.pick needs (a ChoiceAnswer of one candidate
// until tag is alpha), tag.burn failed; keys u, k, p, b run them. ran
// lists what ran to its end.
func outcomeFixture(t *testing.T) (*Runtime, *RecordingSink, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Instance("app")
	var ran []string
	tag := r.Define("tag")
	tag.Verb("use", func(Ctx) Result { ran = append(ran, "use"); return Done(nil) }, "Use")
	tag.Verb("keep", func(Ctx) Result { return Nothing(CauseKept, "the root stays") }, "Keep")
	tag.Verb("pick", func(c Ctx) Result {
		if StringArg(c.Args, "tag", "") == "alpha" {
			ran = append(ran, "pick alpha")
			return Done(nil)
		}
		return Failed(&ChoiceAnswer{Needs: "tag", Reason: "unknown tag", Choices: []ChoiceRow{{ID: "alpha", Command: "tag pick --tag alpha"}}})
	}, "Pick")
	tag.Verb("burn", func(Ctx) Result { return Failed(errors.New("disk on fire")) }, "Burn")
	op := r.Operations.At("tag.pick")
	op.Parameters = map[string]Parameter{"tag": {Type: "string"}}
	r.Operations.Set(op)
	r.Define("app").Mode("normal").Bind("u", Do("tag.use")).Bind("k", Do("tag.keep")).Bind("p", Do("tag.pick")).Bind("b", Do("tag.burn"))
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	return r, sink, &ran
}

// lastInfo is the text of the last info message, "" for none.
func lastInfo(sink *RecordingSink) string {
	msgs := sink.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == UIInfo && msgs[i].Text != "" {
			return msgs[i].Text
		}
	}
	return ""
}

// A person's request, from a key and from the palette, answers each kind
// as the TUI's encoder says: done runs, nothing is an info line, needs
// opens the picker of its candidates (whose pick runs the request again
// with it), failed is the error the caller shows.
func TestPersonOutcomeSurfaces(t *testing.T) {
	routes := []struct {
		name string
		run  func(r *Runtime, sink *RecordingSink, key, verb string) error
	}{
		{"key", func(r *Runtime, _ *RecordingSink, key, _ string) error {
			_, err := r.Dispatch(MacroKey{Key: key})
			return err
		}},
		{"palette", func(r *Runtime, sink *RecordingSink, _, verb string) error {
			m := NewModel(r)
			r.UI().Attach(sink) // the model's own sink would take the messages
			if err := m.Palette.Open(); err != nil {
				return err
			}
			return m.Palette.dispatch(Request{Noun: "tag", Verb: verb})
		}},
	}
	kinds := []struct {
		key, verb, info, modal, err, ran string
	}{
		{"u", "use", "", "", "", "use"},
		{"k", "keep", "tag.keep: kept: the root stays", "", "", ""},
		{"p", "pick", "", NeedPickerKind, "", ""},
		{"b", "burn", "", "", "disk on fire", ""},
	}
	for _, route := range routes {
		for _, k := range kinds {
			r, sink, ran := outcomeFixture(t)
			err := route.run(r, sink, k.key, k.verb)
			got := ""
			if err != nil {
				got = err.Error()
			}
			picker := r.TopModal() == NeedPickerKind
			if got != k.err || lastInfo(sink) != k.info || picker != (k.modal == NeedPickerKind) || (k.ran != "" && !slices.Contains(*ran, k.ran)) {
				t.Errorf("%s %s: err %q info %q modal %q ran %v", route.name, k.verb, got, lastInfo(sink), r.TopModal(), *ran)
				continue
			}
			if k.modal == NeedPickerKind {
				if _, err := r.Execute(Request{Noun: PickerNoun, Verb: "pick"}); err != nil || !slices.Contains(*ran, "pick alpha") {
					t.Errorf("%s pick: %v ran %v", route.name, err, *ran)
				}
			}
		}
	}
}

// outcome.nothing: info tells a person on the status line, quiet tells
// nothing, notice posts a notice that stays.
func TestOutcomeNothingSetting(t *testing.T) {
	for _, c := range []struct{ value, info, notice string }{
		{"info", "tag.keep: kept: the root stays", ""},
		{"quiet", "", ""},
		{"notice", "", "tag.keep: kept: the root stays"},
	} {
		r, sink, _ := outcomeFixture(t)
		if err := r.Settings().Set(OutcomeNothingSetting, c.value); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Dispatch(MacroKey{Key: "k"}); err != nil {
			t.Fatal(err)
		}
		notice := strings.Join(sink.Notices(), "\n")
		if lastInfo(sink) != c.info || notice != c.notice {
			t.Errorf("%s: info %q notice %q", c.value, lastInfo(sink), notice)
		}
	}
}

// ask.choices: picker opens the candidates' picker, form the args form on
// the needed field.
func TestAskChoicesSetting(t *testing.T) {
	for _, c := range []struct{ value, modal string }{
		{"picker", NeedPickerKind},
		{"form", ArgsFormID},
	} {
		r, _, _ := outcomeFixture(t)
		if err := r.Settings().Set(AskChoicesSetting, c.value); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Dispatch(MacroKey{Key: "p"}); err != nil || r.TopModal() != c.modal {
			t.Errorf("%s: modal %q, want %q: %v", c.value, r.TopModal(), c.modal, err)
		}
	}
}

// The socket answers each kind with its Outcome beside the result: none
// for done, nothing, needs with the ChoiceAnswer, failed with the error.
func TestSocketOutcomes(t *testing.T) {
	for _, c := range []struct {
		verb  string
		kind  OutcomeKind
		error string
	}{
		{"use", "", ""},
		{"keep", OutcomeNothing, ""},
		{"pick", OutcomeNeeds, "unknown tag"},
		{"burn", OutcomeFailed, "disk on fire"},
	} {
		r, _, _ := outcomeFixture(t)
		reply := make(chan Response, 1)
		r.HandleCall(CallMsg{Headless: true, Request: Request{Noun: "tag", Verb: c.verb}, Reply: reply})
		res := <-reply
		kind := OutcomeKind("")
		if res.Outcome != nil {
			kind = res.Outcome.Kind
		}
		if kind != c.kind || res.Error != c.error || (c.kind == OutcomeNeeds && res.Choice == nil) {
			t.Errorf("%s: outcome %q error %q choice %v", c.verb, kind, res.Error, res.Choice)
		}
	}
}

// A hook's request never opens a picker: done and nothing are not told,
// needs is a notice with the command that gives it, failed the failure
// notice.
func TestHookOutcomes(t *testing.T) {
	for _, c := range []struct{ verb, notice string }{
		{"use", ""},
		{"keep", ""},
		{"pick", "hook surface.opened → tag.pick: unknown tag; run: tag pick --tag alpha"},
		{"burn", "hook surface.opened → tag.burn failed: disk on fire"},
	} {
		r, _, _ := outcomeFixture(t)
		r.Define("tasks")
		sink := &RecordingSink{}
		r.UI().Attach(sink)
		if err := r.AddDefaultHooks(HookRow{On: "surface.opened", Do: "tag." + c.verb}); err != nil {
			t.Fatal(err)
		}
		openTestModal(t, r, "tasks")
		notice := strings.Join(sink.Notices(), "\n")
		if notice != c.notice || r.TopModal() != "tasks" || strings.Contains(lastInfo(sink), "kept") {
			t.Errorf("%s: notice %q modal %q info %q", c.verb, notice, r.TopModal(), lastInfo(sink))
		}
	}
}
