package inspect

import (
	"encoding/json"
	"testing"
)

// A catalog prop's hostNoun and a list's items reach the inspect page's
// data (propsTable draws them).
func TestPropCarriesHostNounAndItems(t *testing.T) {
	t.Parallel()
	var p Prop
	if err := json.Unmarshal([]byte(`{"name":"project","noun":"lane.project","hostNoun":"project"}`), &p); err != nil || p.HostNoun != "project" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := json.Unmarshal([]byte(`{"name":"dirs","type":"list","items":{"name":"","type":"path"}}`), &p); err != nil || p.Items == nil || p.Items.Type != "path" {
		t.Fatalf("%+v %v", p, err)
	}
}
