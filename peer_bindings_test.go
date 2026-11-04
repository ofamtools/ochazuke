package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func peerFixture(t *testing.T) (*Runtime, PeerBinding) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("lane.lane")
	r.Instance("lane1", Kind("lane.lane"))
	if err := r.SetFocus("lane1"); err != nil {
		t.Fatal(err)
	}
	return r, PeerBinding{Extension: "o-lane", Row: Binding{Noun: "lane.lane", Mode: "normal", Keys: "g s", Request: Request{Noun: "md.doc", Verb: "send-notes"}}}
}

func TestPeerBindingsMissingLateRemoved(t *testing.T) {
	t.Parallel()
	r, row := peerFixture(t)
	notices := []string{}
	r.Config().Report = func(s string) { notices = append(notices, s) }
	status := r.ReloadPeerBindings([]PeerBinding{row}, nil)
	if status[0].Loaded || status[0].Reason != "extension md is not registered; the key loads when it is" {
		t.Fatalf("missing: %+v", status)
	}
	if len(notices) != 0 {
		t.Fatal(notices)
	}
	result, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
	if err != nil || !strings.Contains(strings.Join(result.([]string), "\n"), "o-lane peer row g s on lane.lane: md.doc.send-notes: extension md is not registered") {
		t.Fatalf("check: %v %v", result, err)
	}
	ran := 0
	r.Define("md.doc").Verb("send-notes", func(Ctx) Result { ran++; return Done(nil) })
	if s := r.ReloadPeerBindings([]PeerBinding{row}, nil); !s[0].Loaded {
		t.Fatal(s)
	}
	for _, key := range []string{"g", "s"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if ran != 1 {
		t.Fatalf("ran %d", ran)
	}
	r.ReloadPeerBindings(nil, nil)
	for _, key := range []string{"g", "s"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if ran != 1 || strings.Contains(r.Pending, "g") {
		t.Fatalf("removed row ran or left prefix: %d %s", ran, r.Pending)
	}
}

func TestPeerBindingsDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Runtime, *PeerBinding)
		want   string
	}{
		{"verb", func(r *Runtime, _ *PeerBinding) { r.Define("md.doc") }, "o-md 0.1.0 has no verb md.doc.send-notes"},
		{"argument", func(r *Runtime, p *PeerBinding) {
			r.Define("md.doc").Verb("send-notes", func(Ctx) Result { return Done(nil) })
			p.Row.From = map[string]string{"pane": "row:pane"}
		}, "md.doc.send-notes has no argument pane (o-md 0.1.0)"},
		{"row noun", func(r *Runtime, p *PeerBinding) {
			r.Define("md.doc").Verb("send-notes", func(Ctx) Result { return Done(nil) })
			p.Row.Noun = "git.repo"
		}, "no noun git.repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, row := peerFixture(t)
			tc.change(r, &row)
			s := r.ReloadPeerBindings([]PeerBinding{row}, []BindingPeer{{Name: "o-md", Namespace: "md", Version: "0.1.0", Nouns: []string{"md.doc"}}})
			if s[0].Loaded || s[0].Reason != tc.want {
				t.Fatalf("%+v, want %s", s, tc.want)
			}
		})
	}
}

func TestPeerBindingsOwnerOverrideAndSoftCollision(t *testing.T) {
	t.Parallel()
	for _, unbind := range []bool{false, true} {
		t.Run(map[bool]string{false: "override", true: "unbind"}[unbind], func(t *testing.T) {
			t.Parallel()
			r, row := peerFixture(t)
			peerRuns, ownerRuns := 0, 0
			r.Define("md.doc").Verb("send-notes", func(Ctx) Result { peerRuns++; return Done(nil) })
			r.Define("lane.lane").Verb("own", func(Ctx) Result { ownerRuns++; return Done(nil) })
			c := r.Config()
			c.Dir = t.TempDir()
			owner := row.Row
			owner.Request = Request{Noun: "lane.lane", Verb: "own"}
			if unbind {
				owner.Request = Request{}
				owner.Unbind = true
			}
			data, err := json.Marshal([]Binding{owner})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Dir, BindingsFile), data, 0600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if s := r.ReloadPeerBindings([]PeerBinding{row}, nil); !s[0].Loaded {
					t.Fatal(s)
				}
			}
			for _, key := range []string{"g", "s"} {
				if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
					t.Fatal(err)
				}
			}
			want := 1
			if unbind {
				want = 0
			}
			if peerRuns != 0 || ownerRuns != want {
				t.Fatalf("peer=%d owner=%d", peerRuns, ownerRuns)
			}
			r.ReloadPeerBindings(nil, nil)
			// Removing the user file restores no obsolete peer row.
			if err := os.WriteFile(filepath.Join(c.Dir, BindingsFile), []byte("[]"), 0600); err != nil {
				t.Fatal(err)
			}
			r.ReloadUserBindings()
			r.Define("lane.lane").Mode("normal").Bind("g s", Do("lane.lane.own"))
			status := r.ReloadPeerBindings([]PeerBinding{row}, nil)
			if status[0].Loaded || status[0].Reason != "key is already bound" {
				t.Fatal(status)
			}
		})
	}
}

func TestPeerBindingNoticesSetting(t *testing.T) {
	t.Parallel()
	r, row := peerFixture(t)
	c := r.Config()
	c.Dir = t.TempDir()
	active := map[string]bool{}
	r.Config().Report = func(s string) { active[s] = true }
	r.Config().Withdraw = func(s string) { delete(active, s) }
	r.ReloadPeerBindings([]PeerBinding{row}, nil)
	if err := r.Settings().Set(PeerNoticesSetting, "all"); err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("all: %v", active)
	}
	if err := r.Settings().Set(PeerNoticesSetting, "off"); err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("off: %v", active)
	}
	data, _ := json.Marshal([]Binding{row.Row})
	if err := os.WriteFile(filepath.Join(c.Dir, BindingsFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	r.ReloadUserBindings()
	if len(active) != 0 {
		t.Fatalf("off user: %v", active)
	}
	if err := r.Settings().Set(PeerNoticesSetting, "user"); err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("user: %v", active)
	}
	for line := range active {
		if !strings.HasPrefix(line, "warning: bindings.json:") {
			t.Fatal(line)
		}
	}
}

func TestPeerPolicyRunsBelowOwnerAndPreviousPeerLayers(t *testing.T) {
	t.Parallel()
	r, row := peerFixture(t)
	peerRuns, ownerRuns := 0, 0
	r.Define("md.doc").Verb("send-notes", func(Ctx) Result { peerRuns++; return Done(nil) })
	r.Define("lane.lane").Verb("own", func(Ctx) Result { ownerRuns++; return Done(nil) })
	c := r.Config()
	c.Dir = t.TempDir()
	owner := row.Row
	owner.Request = Request{Noun: "lane.lane", Verb: "own"}
	data, err := json.Marshal([]Binding{owner})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, BindingsFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	r.ReloadUserBindings()
	policy := func(peer PeerBinding) string {
		if peer.Extension != "o-lane" {
			return "wrong declaring extension"
		}
		for _, b := range r.ListBindings(r.Focus) {
			if b.Keys == "g s" {
				return "stale peer or owner override seen as a collision"
			}
		}
		return ""
	}
	for range 2 {
		status := r.ReloadPeerBindings([]PeerBinding{row}, nil, policy)
		if !status[0].Loaded {
			t.Fatal(status)
		}
	}
	for _, key := range []string{"g", "s"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if peerRuns != 0 || ownerRuns != 1 {
		t.Fatalf("peer=%d owner=%d", peerRuns, ownerRuns)
	}
	status := r.ReloadPeerBindings([]PeerBinding{row}, nil, func(PeerBinding) string { return "peer keys disabled" })
	if status[0].Loaded || status[0].Reason != "peer keys disabled" {
		t.Fatal(status)
	}
}
