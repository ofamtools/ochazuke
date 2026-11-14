package offercheck

import (
	"testing"

	"ofam.dev/ochazuke"
)

// A verb check that refuses an offered request is reported; a verb that
// would run, and one that asks for a missing value, are not.
func TestWalkReportsARefusedOffer(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	ok := func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return nil, nil }
	r.Register(ochazuke.Operation{Key: "box.open", Run: ok})
	r.Register(ochazuke.Operation{Key: "box.name", Parameters: map[string]ochazuke.Parameter{"name": {Type: "string", Required: true}}, Run: ok})
	r.Register(ochazuke.Operation{Key: "box.paint", Parameters: map[string]ochazuke.Parameter{"colour": {Type: "string", Enum: []string{"red", "blue"}}}, Run: ok})
	if err := r.AddVerbCheck("box.paint", ochazuke.VerbCheck{Name: "no-blue", Check: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) ochazuke.Verdict {
		if a["colour"] == "blue" {
			return ochazuke.Refuse("no blue")
		}
		return ochazuke.Allow()
	}}); err != nil {
		t.Fatal(err)
	}
	refusals, n, err := Walk(r, Config{})
	if err != nil || n == 0 {
		t.Fatal(err, n)
	}
	var keys []string
	for _, f := range refusals {
		keys = append(keys, f.Key+" "+f.Args["colour"].(string))
	}
	if len(keys) != 1 || keys[0] != "main.box.paint blue" && keys[0] != "box.paint blue" {
		t.Fatalf("refusals %v", keys)
	}
	if _, _, err := Walk(r, Config{Cap: 1}); err == nil {
		t.Fatal("the cap did not stop the walk")
	}
}
