package cli

import (
	"context"
	"strings"
	"sync"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

// liveStream is a pane that records what it is sent and whether it was
// released.
type liveStream struct {
	mu       sync.Mutex
	sent     []string
	released bool
}

func (s *liveStream) Next(ctx context.Context) (ochazuke.TerminalFrame, error) {
	<-ctx.Done()
	return ochazuke.TerminalFrame{}, ctx.Err()
}
func (s *liveStream) Send(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, keys...)
	return nil
}
func (s *liveStream) Paste(string) error    { return nil }
func (s *liveStream) Resize(int, int) error { return nil }
func (s *liveStream) Release() error        { s.released = true; return nil }
func (s *liveStream) keys() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.sent, " ")
}
func (s *liveStream) wasReleased() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.released }

// A noun whose prop declares a terminal view shows its thing's terminal
// in the show page of a record-form detail: enter (a row naming
// ochazuke.detail.live) opens it there and focuses it, keys go to the
// pane, the leader's d closes it, releases the stream and lands back on
// the row. With detail.live-view off the row's guard (detail-live) is
// false, so the key goes on to its next row.
func TestRecordFormLiveViewOpensInPlace(t *testing.T) {
	t.Parallel()
	for _, on := range []bool{true, false} {
		r := ochazuke.NewRuntime()
		r.Config().Disable()
		stream := &liveStream{}
		opened, other := "", 0
		r.AddHook("item.terminal", func(_ ochazuke.Ctx, key string) (ochazuke.TerminalStream, error) {
			opened = key
			return stream, nil
		})
		things := []ochazuke.Thing{{"id": "i1", "title": "Item one"}}
		r.DefineOwn("item").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
			ochazuke.PropSpec{Name: "live", ReadOnly: true, View: &ochazuke.PropView{Kind: ochazuke.PropViewTerminal, Hook: "item.terminal"}}).
			Store(ochazuke.Store{
				Get:  func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return things[0], nil },
				List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil },
			}).
			Verb("other", func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { other++; return nil, nil }, "The key's other row").
			Mode("normal").Bind("enter", ochazuke.Do("ochazuke.detail.attach")).When("detail-live").Bind("enter", ochazuke.Do("item.other"))
		if err := r.Settings().Set(ochazuke.DetailLiveSetting, on); err != nil {
			t.Fatal(err)
		}
		m := ochazuke.NewModel(r)
		m.Width, m.Height = 100, 30
		page := CatalogPage{ID: "items", Title: "Items", Layout: LayoutSplit, Lists: []CatalogList{{ID: "items/list", Noun: "item", Kind: ListKindIndex, Tree: true}}, Detail: &CatalogDetail{Kind: DetailRecordForm}}
		if _, err := MountPage(m, CatalogDoc{}, page, PageConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenPage("items"); err != nil {
			t.Fatal(err)
		}
		m.View()
		row := r.Focus
		if !strings.HasSuffix(row, "/i1") {
			t.Fatalf("focus %q", row)
		}
		if f := frameOf(m); strings.Contains(f, "│ live ") {
			t.Fatalf("the form draws the view prop as a field:\n%s", f)
		}
		keytest.Press(m, "enter")
		m.View()
		if !on {
			if opened != "" || r.Focus != row || other != 1 {
				t.Fatalf("off: opened %q, focus %q, the other row ran %d times", opened, r.Focus, other)
			}
			continue
		}
		if opened != "i1" || other != 0 || !strings.HasSuffix(r.Focus, "/live") || r.Mode.Name != "terminal" {
			t.Fatalf("on: opened %q, focus %q, mode %q", opened, r.Focus, r.Mode.Name)
		}
		keytest.Press(m, "x")
		if stream.keys() != "x" {
			t.Fatalf("sent %q", stream.keys())
		}
		keytest.Press(m, r.TerminalLeader())
		keytest.Press(m, "d")
		m.View()
		if r.Focus != row || r.Instances["items/detail/row/show/live"] != nil || !stream.wasReleased() {
			t.Fatalf("after detach: focus %q, released %v", r.Focus, stream.wasReleased())
		}
	}
}
