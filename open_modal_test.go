package ochazuke

import "testing"

func TestOpenVerbs(t *testing.T) {
	t.Parallel()
	r, f, _ := openFixture(t)
	got, err := r.Execute(Request{Noun: OpenNoun, Verb: "resolve", Args: Arguments{"text": "https://x.test/a"}})
	res, ok := got.(openResult)
	if err != nil || !ok || res.Action != OpenActionOS || len(f.runs) != 0 {
		t.Fatalf("resolve %#v %v ran %v", got, err, f.runs)
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "run", Args: Arguments{"text": "https://x.test/a"}}); err != nil || len(f.runs) != 1 {
		t.Fatalf("run %v ran %v", err, f.runs)
	}
	op, _ := r.Operation("ochazuke.opener.run")
	if op.AgentRightOf(false) != AgentConfirm {
		t.Fatal("open.run needs confirmation from an agent")
	}
	if err := r.Settings().Set(OpenModalSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "show"}); err == nil {
		t.Fatal("open.show is off with open.modal=false")
	}
	if _, err := r.Execute(Request{Noun: OpenNoun, Verb: "run", Args: Arguments{"text": "y.png"}}); err != nil || len(f.runs) != 2 {
		t.Fatalf("run still opens with the modal off: %v %v", err, f.runs)
	}
}
