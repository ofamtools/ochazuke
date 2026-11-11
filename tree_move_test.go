package ochazuke

import (
	"errors"
	"testing"
)

// The outline of deleteFixture: a (b (c d) e) f.

func TestTreeMoveSwapsSiblingsAndStopsAtTheTopLevelEdge(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "G", "K") // f up past a
	if got, want := shape(tree), "f<- a<- b<a c<b d<b e<a"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	pressKeys(m, "K") // first at the top level: quiet edge
	if got, want := shape(tree), "f<- a<- b<a c<b d<b e<a"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
}

func TestTreeMoveCountMultipliesAndStopsAtTheEdge(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, "")
	if err := tree.SetNodes([]TreeNode{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}, {ID: "c", Label: "c"}, {ID: "d", Label: "d"}}); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "g", "g", "2", "J")
	if got, want := shape(tree), "b<- c<- a<- d<-"; got != want {
		t.Fatalf("2J: shape %q, want %q", got, want)
	}
	if len(*writes) != 1 {
		t.Fatalf("one write for a counted move, got %v", *writes)
	}
	pressKeys(m, "9", "J")
	if got, want := shape(tree), "b<- c<- d<- a<-"; got != want {
		t.Fatalf("9J: shape %q, want %q", got, want)
	}
	pressKeys(m, "9", "K")
	if got, want := shape(tree), "a<- b<- c<- d<-"; got != want {
		t.Fatalf("9K: shape %q, want %q", got, want)
	}
}

func TestTreeMoveToTheFirstAndLastPlace(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "j", "j", "g", "J") // c, down to the last place among b's children
	if got, want := shape(tree), "a<- b<a d<b c<b e<a f<-"; got != want {
		t.Fatalf("g J: shape %q, want %q", got, want)
	}
	pressKeys(m, "g", "K")
	if got, want := shape(tree), "a<- b<a c<b d<b e<a f<-"; got != want {
		t.Fatalf("g K: shape %q, want %q", got, want)
	}
	pressKeys(m, "g", "K") // already first: nothing moves
	if got, want := shape(tree), "a<- b<a c<b d<b e<a f<-"; got != want {
		t.Fatalf("g K again: shape %q, want %q", got, want)
	}
}

func TestTreeMoveSpillsOutOfTheParentAtItsEdges(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "j", "j", "K") // c is b's first child: it precedes b
	if got, want := shape(tree), "a<- c<a b<a d<b e<a f<-"; got != want {
		t.Fatalf("K at the first child: shape %q, want %q", got, want)
	}
	pressKeys(m, "u")
	pressKeys(m, "3", "J") // c over d, out after b, over e
	if got, want := shape(tree), "a<- b<a d<b e<a c<a f<-"; got != want {
		t.Fatalf("3J: shape %q, want %q", got, want)
	}
	if len(*writes) < 2 {
		t.Fatalf("writes %v", *writes)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after u %q, want %q", got, deleteStart)
	}
}

func TestTreeMoveSpillOffStopsAtTheEdge(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := deleteFixture(t, "")
	if err := r.Settings().Set(TreeMoveSpillSetting, false); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "j", "j", "K")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("shape %q, want it unchanged", got)
	}
}

func TestTreeMoveSpillStopsWhereTheApplicationRefuses(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := deleteFixture(t, "")
	tree.Refuse = func(_ TreeNode, c TreeChange) error {
		if c.Op == TreeMove && c.Parent == "" {
			return errors.New("stays under its parent")
		}
		return nil
	}
	pressKeys(m, "j", "K") // b is a's first child: it may not become a top-level row
	if got := shape(tree); got != deleteStart {
		t.Fatalf("shape %q, want it unchanged", got)
	}
}

func TestTreeMoveVisualSelectionMovesAsABlockInOneStep(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "j", "V", "j", "J") // b (and its subtree) and c selected: b one place down
	if got, want := shape(tree), "a<- e<a b<a c<b d<b f<-"; got != want {
		t.Fatalf("selection: shape %q, want %q", got, want)
	}
	pressKeys(m, "esc", "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after one u %q, want %q", got, deleteStart)
	}
}

func TestTreeMoveVisualBlockStopsTogetherAtTheEdge(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := deleteFixture(t, "")
	if err := tree.SetNodes([]TreeNode{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}, {ID: "c", Label: "c"}, {ID: "d", Label: "d"}}); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "j", "V", "j", "K", "K", "K") // b, c up past the top: they keep their order
	if got, want := shape(tree), "b<- c<- a<- d<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
}
