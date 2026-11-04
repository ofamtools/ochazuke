package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func protectedFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	m := NewModel(r)
	if err := r.DeclareSetting(Setting{Name: "private", Type: SettingList, Default: []string{"first"}, Protected: true, Deprecated: []string{"old-private"}}, Setting{Name: "editable", Type: SettingString, Default: "old", Section: "agents", Owner: OwnerFramework}); err != nil {
		t.Fatal(err)
	}
	return r, m
}

func TestProtectedSettingsWriters(t *testing.T) {
	for _, gate := range []bool{false, true} {
		for _, session := range []bool{false, true} {
			r, _ := protectedFixture(t)
			s := r.Settings()
			if err := s.Set(AgentRightsGateSetting, gate); err != nil {
				t.Fatal(err)
			}
			if err := s.Set("private", []string{"saved"}); err != nil {
				t.Fatal(err)
			}
			if session {
				s.StartSession()
				if _, err := s.SetSession("private", []string{"session"}); err != nil {
					t.Fatal(err)
				}
			}
			snap := s.snap("private")
			file, err := os.ReadFile(filepath.Join(r.Config().Dir, SettingsFile))
			if err != nil {
				t.Fatal(err)
			}
			restore := r.WithActor(testAgent("p"))
			for _, write := range []func() error{
				func() error { return s.Set("private", []string{"changed"}) },
				func() error { return s.Set("old-private", []string{"changed"}) },
				func() error { return s.Reset("private") },
				func() error { _, e := s.SetSession("private", []string{"changed"}); return e },
				func() error { _, e := s.ResetSession("private"); return e },
				func() error { return s.Revert("private") },
				func() error { _, e := s.Write("private"); return e },
				func() error { sn := snap; sn.inSession = false; return s.restore(sn) },
				func() error {
					raw := map[string]json.RawMessage{}
					for k, v := range s.raw {
						raw[k] = v
					}
					raw["private"] = json.RawMessage(`["changed"]`)
					return s.write(raw)
				},
			} {
				var refused *RefusedError
				if err := write(); !errors.As(err, &refused) {
					t.Fatalf("gate=%v session=%v: %v", gate, session, err)
				}
				if !reflect.DeepEqual(snap, s.snap("private")) {
					t.Fatal("protected session changed")
				}
				got, err := os.ReadFile(filepath.Join(r.Config().Dir, SettingsFile))
				if err != nil || !bytes.Equal(file, got) {
					t.Fatal("protected file changed", err)
				}
			}
			restore()
		}
	}
}

func TestProtectedSettingsBulkSkips(t *testing.T) {
	for _, verb := range []string{"write", "revert", "reset-all"} {
		for _, session := range []bool{false, true} {
			r, _ := protectedFixture(t)
			s := r.Settings()
			if err := s.Set(AgentRightsGateSetting, false); err != nil {
				t.Fatal(err)
			}
			if err := s.Set("private", []string{"saved"}); err != nil {
				t.Fatal(err)
			}
			if err := s.Set("editable", "saved"); err != nil {
				t.Fatal(err)
			}
			if session {
				s.StartSession()
				_, _ = s.SetSession("private", []string{"session"})
				_, _ = s.SetSession("editable", "session")
			}
			before := s.snap("private")
			sink := &RecordingSink{}
			r.UI().Attach(sink)
			restore := r.BindActor(testAgent("p"), false)
			value, err := r.Execute(Request{Noun: "settings", Verb: verb})
			restore()
			if err != nil {
				t.Fatalf("%s: %v", verb, err)
			}
			if !reflect.DeepEqual(before, s.snap("private")) {
				t.Fatalf("%s changed protected value", verb)
			}
			if (session || verb == "reset-all") && !strings.Contains(sink.LastInfo(), "private") {
				t.Fatalf("missing skipped answer %s: %v %s", verb, value, sink.LastInfo())
			}
			want := "saved"
			if verb == "write" && session {
				want = "session"
			}
			if verb == "reset-all" {
				want = "old"
			}
			if s.String("editable") != want {
				t.Fatalf("%s writable value %q want %q", verb, s.String("editable"), want)
			}
		}
	}
}

// Every registered verb on these nouns is exercised against the owner's rights,
// including newly added verbs. The file and effective value must stay unchanged.
func TestProtectedSettingsEveryVerb(t *testing.T) {
	nouns := []string{"ochazuke.settings", "ochazuke.setting", "ochazuke.setting-item", "ochazuke.setting-value", "ochazuke.settings-section", "ochazuke.settings-tab", "ochazuke.settings-page"}
	catalog := NewRuntime()
	NewModel(catalog)
	for _, op := range catalog.Catalog() {
		if !slices.Contains(nouns, op.Noun) {
			continue
		}
		for _, gate := range []bool{false, true} {
			t.Run(op.Key()+"/gate="+stringBool(gate), func(t *testing.T) {
				r, _ := protectedFixture(t)
				s := r.Settings()
				if err := s.Set(AgentRightsGateSetting, gate); err != nil {
					t.Fatal(err)
				}
				_, err := r.Execute(Request{Noun: "settings", Verb: "edit"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = r.Execute(Request{Noun: "setting", Verb: "set", Args: Arguments{"name": AgentRightsSetting, "value": `[{"match":"task.*","right":"never"}]`, "scope": "file"}, Confirmed: true})
				if err != nil {
					t.Fatal(err)
				}
				row := settingRowID(r, AgentRightsSetting, -1)
				if err := r.SetFocus(row); err != nil {
					t.Fatal(err)
				}
				if op.Noun == "ochazuke.setting-value" {
					_, err = r.Execute(Request{Noun: "setting", Verb: "edit", Args: Arguments{"name": AgentRightsSetting}})
					if err != nil {
						t.Fatal(err)
					}
					in := r.Input(settingValueID)
					in.Replace(0, len(in.Text), "[]")
				}
				before, _ := s.Value(AgentRightsSetting)
				file, err := os.ReadFile(filepath.Join(r.Config().Dir, SettingsFile))
				if err != nil {
					t.Fatal(err)
				}
				actor := testAgent("p")
				restore := r.BindActor(actor, false)
				defer restore()
				args := Arguments{"name": AgentRightsSetting, "value": "[]", "by": 1, "step": 1, "index": 0, "insert": 0, "scope": "file", "line": "agents.rights=[]", "owner": OwnerFramework, "section": "agents", "id": AgentRightsSetting, "prop": "value"}
				for name := range args {
					if _, ok := op.Parameters[name]; !ok {
						delete(args, name)
					}
				}
				_, _ = r.Execute(Request{Noun: op.Noun, Verb: op.Verb, Args: args, Actor: &actor, Confirmed: true})
				after, _ := s.Value(AgentRightsSetting)
				got, err := os.ReadFile(filepath.Join(r.Config().Dir, SettingsFile))
				if err != nil || !bytes.Equal(file, got) || !reflect.DeepEqual(before, after) {
					t.Fatalf("%s changed protected rights: before=%v after=%v err=%v", op.Key(), before, after, err)
				}
			})
		}
	}
}

func stringBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestProtectedRightsDeclarations(t *testing.T) {
	for _, gate := range []bool{false, true} {
		r, _ := protectedFixture(t)
		s := r.Settings()
		if err := s.Set(AgentRightsGateSetting, gate); err != nil {
			t.Fatal(err)
		}
		actor := testAgent("p")
		for _, name := range []string{AgentRightsSetting, AgentRightsGateSetting, AgentConfirmAttributionSetting} {
			before, _ := s.Value(name)
			var value any = "false"
			if name == AgentRightsSetting {
				value = `[{"match":"*","right":"allow"}]`
			}
			_, err := r.Execute(Request{Noun: "settings", Verb: "set", Actor: &actor, Args: Arguments{"name": name, "value": value}, Confirmed: true})
			var refused *RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("%s gate=%v: %v", name, gate, err)
			}
			after, _ := s.Value(name)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("%s changed", name)
			}
			row, err := r.Execute(Request{Noun: "setting", Verb: "get", Args: Arguments{"name": name}})
			if err != nil || row.(map[string]any)["protected"] != true {
				t.Fatalf("missing protected flag: %v %v", row, err)
			}
		}
	}
}

func TestProtectedSettingsScopes(t *testing.T) {
	for _, noun := range []string{"settings-section", "settings-tab"} {
		for _, verb := range []string{"reset", "write", "revert"} {
			r, _ := protectedFixture(t)
			s := r.Settings()
			if err := s.Set(AgentRightsGateSetting, false); err != nil {
				t.Fatal(err)
			}
			if err := s.Set(AgentRightsSetting, `[{"match":"task.*","right":"never"}]`); err != nil {
				t.Fatal(err)
			}
			s.StartSession()
			if _, err := s.SetSession(AgentRightsSetting, `[{"match":"task.*","right":"confirm"}]`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetSession("editable", "new"); err != nil {
				t.Fatal(err)
			}
			before := s.snap(AgentRightsSetting)
			sink := &RecordingSink{}
			r.UI().Attach(sink)
			actor := testAgent("p")
			args := Arguments{}
			if verb == "reset" {
				args["scope"] = "file"
			}
			if noun == "settings-section" {
				args["section"] = "ochazuke/agents"
			} else {
				args["owner"] = OwnerFramework
			}
			_, err := r.Execute(Request{Noun: noun, Verb: verb, Actor: &actor, Confirmed: true, Args: args})
			if err != nil || !reflect.DeepEqual(before, s.snap(AgentRightsSetting)) || !strings.Contains(sink.LastInfo(), AgentRightsSetting) {
				t.Fatalf("%s.%s: %v %s", noun, verb, err, sink.LastInfo())
			}
		}
	}
}
