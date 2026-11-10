package ochazuke

import "testing"

func TestPropsRefuseAHostNounThatIsNotANounName(t *testing.T) {
	t.Parallel()
	defer func() {
		if v := recover(); v == nil {
			t.Fatal("a hostNoun with spaces was accepted")
		}
	}()
	NewRuntime().Define("thing").Props(PropSpec{Name: "n", Parameter: Parameter{HostNoun: "Bad Noun"}})
}
