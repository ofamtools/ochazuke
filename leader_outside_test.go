package ochazuke

import (
	"reflect"
	"testing"
)

func outsideFixture(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := associationFixture(t, true)
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("surface"); err != nil {
		t.Fatal(err)
	}
	if err := r.DefineMode("captured", false); err != nil {
		t.Fatal(err)
	}
	if err := r.DefinePassthrough("captured"); err != nil {
		t.Fatal(err)
	}
	if err := r.DefineMode("shortcut", true, ModeOutside("normal")); err != nil {
		t.Fatal(err)
	}
	var sent []string
	r.Define("surface").Verb("send", func(c Ctx) Result { sent = append(sent, StringArg(c.Args, "keys", "")); return Done(nil) })
	op := r.Operations.At(Key("surface.send"))
	op.Parameters = map[string]Parameter{"keys": {Type: "string"}}
	r.Operations.Set(op)
	prefix := Do("surface.send", Arguments{"keys": "ctrl+x"}).Request
	r.Define("surface").Mode("captured").Bind("ctrl+x", Enter("shortcut", Payload{Prefix: "ctrl+x", Request: &prefix})).Pattern("any", Do("surface.send").FromKey("keys"))
	r.Define("surface").Mode("shortcut").Bind("ctrl+x", Do("surface.send", Arguments{"keys": "ctrl+x"}))
	if err := r.SetMode("captured"); err != nil {
		t.Fatal(err)
	}
	return r, &sent
}

func outsideKeys(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("key %s: %v", key, err)
		}
	}
}

func TestOutsideDispatchPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, policy       string
		motion, bound, run bool
	}{
		{"ordinary-default", "no-motions", false, false, true},
		{"motion-default", "no-motions", true, false, false},
		{"focus-bound-default", "no-motions", false, true, false},
		{"motion-all", "all", true, false, true},
		{"focus-bound-all", "all", false, true, true},
		{"off", "off", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, sent := outsideFixture(t)
			if err := r.Settings().Set(LeaderFallThroughSetting, tc.policy); err != nil {
				t.Fatal(err)
			}
			changed := map[string]int{}
			r.Define("thing").Verb("touch", func(c Ctx) Result {
				changed[c.Instance.ID]++
				if r.Focus != "surface" {
					t.Fatal("temporary focus was installed")
				}
				return Done(nil)
			}).Mode("normal").Bind("r", Do("thing.touch"))
			op := r.Operations.At(Key("thing.touch"))
			op.Motion, op.FocusBound = tc.motion, tc.bound
			r.Operations.Set(op)
			outsideKeys(t, r, "ctrl+x")
			if len(changed) != 0 || len(*sent) != 0 || r.Mode.Name != "shortcut" {
				t.Fatalf("leader changed the store: %v, %v, %s", changed, *sent, r.Mode.Name)
			}
			outsideKeys(t, r, "r")
			if tc.run {
				if !reflect.DeepEqual(changed, map[string]int{"row-a": 1}) || len(*sent) != 0 {
					t.Fatalf("store %v sent %v", changed, *sent)
				}
			} else if len(changed) != 0 || !reflect.DeepEqual(*sent, []string{"ctrl+x", "r"}) {
				t.Fatalf("store %v sent %v", changed, *sent)
			}
			if r.Focus != "surface" || r.BaseMode() != "captured" {
				t.Fatalf("return %s %s", r.Focus, r.BaseMode())
			}
		})
	}
}

func TestOutsideDispatchBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"subject-off", "other-page", "multi-key", "guard-off", "double-leader", "explicit-leader"} {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			r, sent := outsideFixture(t)
			var changes []string
			r.Define("thing").Verb("touch", func(c Ctx) Result { changes = append(changes, c.Instance.ID); return Done(nil) }).Mode("normal").Bind("r", Do("thing.touch"))
			keys := []string{"ctrl+x", "r"}
			switch tc {
			case "subject-off":
				if err := r.Settings().Set(SubjectFromAssociationSetting, false); err != nil {
					t.Fatal(err)
				}
			case "other-page":
				if _, err := r.AddPage("other", "Other", func(*Runtime, int, int) Block { return Text("") }); err != nil {
					t.Fatal(err)
				}
				r.Instances["row-a"].Parent = "other"
			case "multi-key":
				r.Define("thing").Mode("normal").Unbind("r").Bind("r r", Do("thing.touch"))
			case "guard-off":
				r.DefineGuard("never", func(*Runtime, KeyEvent) bool { return false })
				r.SetAvailability("thing.touch", "never")
			case "double-leader":
				keys = []string{"ctrl+x", "ctrl+x", "r"}
			case "explicit-leader":
				r.Define("surface").Mode("shortcut").Bind("r", Do("surface.send", Arguments{"keys": "explicit"}))
			}
			outsideKeys(t, r, keys...)
			want := []string{"ctrl+x", "r"}
			if tc == "explicit-leader" {
				want = []string{"explicit"}
			}
			if len(changes) != 0 || !reflect.DeepEqual(*sent, want) || r.BaseMode() != "captured" {
				t.Fatalf("changes %v sent %v mode %s", changes, *sent, r.BaseMode())
			}
		})
	}
}

func TestOutsideModalReturnsToCapturedSurface(t *testing.T) {
	t.Parallel()
	r, sent := outsideFixture(t)
	if err := r.List("things").Select("row-b"); err != nil {
		t.Fatal(err)
	}
	r.DefineGuard("on-alpha", func(_ *Runtime, ev KeyEvent) bool { return ev.Focus == "row-a" && ev.Mode.Name == "normal" })
	r.Define("thing").Verb("prompt", func(c Ctx) Result {
		if c.Instance.ID != "row-a" {
			t.Fatalf("subject %s", c.Instance.ID)
		}
		_, err := c.Runtime.OpenModal("rename", Payload{Prompt: "Rename " + c.Instance.Label}, WithInput(NewInput("name", c.Instance.Label, false), Kind("ochazuke.prompt")))
		if err != nil {
			return Failed(err)
		}
		return Done(nil)
	}).When("on-alpha").Mode("normal").Bind("r", Do("thing.prompt"))
	outsideKeys(t, r, "ctrl+x", "r")
	if r.TopModal() != "rename" || r.Instances["rename"].Payload.Prompt != "Rename Alpha" || len(*sent) != 0 {
		t.Fatalf("modal %s sent %v", r.TopModal(), *sent)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "surface" || r.BaseMode() != "captured" {
		t.Fatalf("return %s %s", r.Focus, r.BaseMode())
	}
}

func TestOutsideModeDeclaration(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	if err := r.DefineMode("bad", false, ModeOutside("normal")); err == nil {
		t.Fatal("base mode accepted outside")
	}
	if err := r.DefineMode("bad", true, ModeOutside("missing")); err == nil {
		t.Fatal("unknown outside accepted")
	}
	data, err := FormatBindings(r.BindingsDoc())
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := ParseBindings(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Modes["leader_pending"].Outside != "normal" {
		t.Fatalf("outside lost: %+v", doc.Modes["leader_pending"])
	}
	if problems := r.fileModeProblems(doc); len(problems) != 0 {
		t.Fatal(problems)
	}
}

// Several associated things pick nothing: the leader key ends the pending
// mode, runs the verb with no subject (so it asks), and never errors or acts
// on one of them.
func TestOutsideSeveralCandidatesPickNothing(t *testing.T) {
	t.Parallel()
	r, sent := outsideFixture(t)
	var touched []string
	r.Define("thing").Verb("touch", func(c Ctx) Result { touched = append(touched, c.Instance.ID); return Done(nil) }).Mode("normal").Bind("r", Do("thing.touch")).ByAssociation()
	op := r.Operations.At(Key("thing.touch"))
	op.Parameters = map[string]Parameter{"id": {Type: "string", Focus: true, Noun: "thing", Source: "thing.list"}}
	r.Operations.Set(op)
	r.Instances["surface"].Data = map[string]any{"subject": []any{"a", "b"}}
	outsideKeys(t, r, "ctrl+x", "r")
	if len(touched) != 0 && (touched[0] == "row-a" || touched[0] == "row-b") {
		t.Fatalf("acted on one of %v", touched)
	}
	if r.TopModal() == "" || r.Mode.Name == "shortcut" {
		t.Fatalf("no ask opened or leader left pending: modal %q mode %s base %s touched %v", r.TopModal(), r.Mode.Name, r.BaseMode(), touched)
	}
	t.Logf("touched %v sent %v modal %q err-free", touched, *sent, r.TopModal())
}
