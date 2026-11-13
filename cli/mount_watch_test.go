package cli

import (
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// A mounted noun's standard watch: the extension's own verb mounts (the
// proxy store does not serve it) and stays mounted when the host
// refreshes the noun's generated verbs later (here: a noun with an about
// prop arrives, as o-json's doc does beside o-md's in mtm); with
// HostMount.Watch the proxy store streams it and the host's standard
// watch serves it.
func TestMountedWatchStaysAndStreams(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	proxy := func(_ string, v CatalogVerb) any {
		return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	}
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	MountNouns(host, doc.AppNouns(), HostMount{Local: Namespace("tool"), Proxy: proxy})
	memos := []ochazuke.Thing{}
	host.Define("memo").Props(ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "about", Role: ochazuke.RoleAbout, Parameter: ochazuke.Parameter{Noun: ochazuke.AnyNoun}}).
		Store(ochazuke.Store{Kind: ochazuke.KindCollection,
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return memos, nil },
			Create: func(_ ochazuke.Ctx, v ochazuke.Thing) (ochazuke.Thing, error) {
				memos = append(memos, v)
				return v, nil
			}})
	if op, ok := host.Operation("tool.parcel.watch"); !ok || !op.CatalogProxy {
		t.Fatalf("tool.parcel.watch dropped by a refresh of the generated verbs (ok %v)", ok)
	}

	streamed := ochazuke.WatchEvent{Type: "added", ID: "x1"}
	host = ochazuke.NewRuntime()
	host.Config().Disable()
	MountNouns(host, doc.AppNouns(), HostMount{Local: Namespace("tool"), Proxy: proxy,
		Watch: func(noun string, q ochazuke.WatchQuery, emit func(ochazuke.WatchEvent)) (func(), error) {
			if noun != "parcel" {
				t.Errorf("watch of %q", noun)
			}
			go emit(streamed)
			return func() {}, nil
		}})
	op, ok := host.Operation("tool.parcel.watch")
	if !ok || op.CatalogProxy || op.Standard != "watch" {
		t.Fatalf("with HostMount.Watch the store should serve watch: ok %v proxy %v standard %q", ok, op.CatalogProxy, op.Standard)
	}
	w, err := host.Watch("tool.parcel", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cmd := host.PendingCommands(); cmd != nil {
			if _, err := host.Deliver(cmd()); err != nil {
				t.Fatal(err)
			}
		}
		if got := w.Take(); len(got) > 0 {
			if got[0].ID != "x1" {
				t.Fatalf("events %+v", got)
			}
			return
		}
	}
	t.Fatal("no event streamed")
}
