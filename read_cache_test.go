package ochazuke

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cacheRuntime is fromRuntime with its state in dir and the note store's
// change token *token.
func cacheRuntime(t *testing.T, dir string, token *string) (*Runtime, *fromStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().StateDir = dir
	t.Cleanup(func() { _ = r.Config().Flush() })
	notes := &fromStore{things: []Thing{{"id": "n1", "title": "First"}, {"id": "n2", "title": "Second"}}}
	s := notes.store()
	s.ChangeToken = func(Ctx) (string, error) { return *token, nil }
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(s)
	return r, notes
}

func TestReadCacheDrawsKeptRowsAndRereadsOnlyWhenTheTokenMoved(t *testing.T) {
	dir := t.TempDir()
	token := "1"
	r, _ := cacheRuntime(t, dir, &token)
	first, err := r.AddList("a", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	if first.refills != 1 {
		t.Fatalf("first run read the list %d times, want 1", first.refills)
	}

	// A new run, the token unmoved: the kept rows are drawn and current.
	r, _ = cacheRuntime(t, dir, &token)
	l, err := r.AddList("a", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	if l.refills != 0 || len(fromIDs(l)) != 2 || l.CachedAge() != 0 {
		t.Fatalf("unmoved token: reads %d, rows %v, age %v; want no read, two rows, fresh", l.refills, fromIDs(l), l.CachedAge())
	}

	// The token moved: the store is read again.
	token = "2"
	r, notes := cacheRuntime(t, dir, &token)
	notes.things = append(notes.things, Thing{"id": "n3", "title": "Third"})
	l, err = r.AddList("a", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	if l.refills != 1 || len(fromIDs(l)) != 3 || l.CachedAge() != 0 {
		t.Fatalf("moved token: reads %d, rows %v, age %v; want one read, three rows, fresh", l.refills, fromIDs(l), l.CachedAge())
	}
}

func TestReadCacheIgnoresACorruptOrOversizeFile(t *testing.T) {
	for name, setup := range map[string]func(r *Runtime, path string){
		"corrupt":  func(_ *Runtime, path string) { _ = os.WriteFile(path, []byte("{not json"), 0o600) },
		"oversize": func(r *Runtime, path string) { _ = r.Settings().Set(ReadCacheDiskMaxSetting, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			token := "1"
			r, _ := cacheRuntime(t, dir, &token)
			if _, err := r.AddList("a", nil, From("note")); err != nil {
				t.Fatal(err)
			}
			if err := r.Config().Flush(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ReadCacheDir, "note.json")
			r, _ = cacheRuntime(t, dir, &token)
			setup(r, path)
			l, err := r.AddList("a", nil, From("note"))
			if err != nil {
				t.Fatal(err)
			}
			if l.refills != 1 || len(fromIDs(l)) != 2 {
				t.Fatalf("reads %d, rows %v; want the list read once and its two rows", l.refills, fromIDs(l))
			}
		})
	}
}

func TestReadCacheLimits(t *testing.T) {
	token := "1"
	t.Run("entry-max", func(t *testing.T) {
		dir := t.TempDir()
		r, _ := cacheRuntime(t, dir, &token)
		_ = r.Settings().Set(ReadCacheEntryMaxSetting, 0)
		if _, err := r.AddList("a", nil, From("note")); err != nil {
			t.Fatal(err)
		}
		_ = r.Config().Flush()
		if _, err := os.Stat(filepath.Join(dir, ReadCacheDir, "note.json")); !os.IsNotExist(err) {
			t.Fatalf("an answer over cache.entry-max was written (%v)", err)
		}
	})
	t.Run("disk off", func(t *testing.T) {
		dir := t.TempDir()
		r, _ := cacheRuntime(t, dir, &token)
		_ = r.Settings().Set(ReadCacheDiskSetting, false)
		if _, err := r.AddList("a", nil, From("note")); err != nil {
			t.Fatal(err)
		}
		_ = r.Config().Flush()
		if _, err := os.Stat(filepath.Join(dir, ReadCacheDir)); !os.IsNotExist(err) {
			t.Fatalf("cache.disk off made the folder (%v)", err)
		}
	})
	t.Run("memory-max", func(t *testing.T) {
		r, _ := cacheRuntime(t, t.TempDir(), &token)
		_ = r.Settings().Set(ReadCacheMemoryMaxSetting, 0)
		rc := r.cacheOn("note")
		for _, store := range []string{"a", "b", "c"} {
			rc.store(r, store).bytes = 10
			rc.bytes += 10
		}
		rc.trimMemory(r, "c")
		if len(rc.stores) != 1 || rc.stores["c"] == nil {
			t.Fatalf("over cache.memory-max the stores in memory are %v, want only the one in use", rc.stores)
		}
	})
	t.Run("disk-max", func(t *testing.T) {
		dir := t.TempDir()
		for i, name := range []string{"old.json", "mid.json", "new.json"} {
			path := filepath.Join(dir, name)
			_ = os.WriteFile(path, make([]byte, 100), 0o600)
			at := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
			_ = os.Chtimes(path, at, at)
		}
		trimCacheDir(dir, 150, []string{filepath.Join(dir, "mid.json")})
		left, _ := os.ReadDir(dir)
		if len(left) != 1 || left[0].Name() != "mid.json" {
			t.Fatalf("over cache.disk-max %v are left, want the one just written", left)
		}
	})
}
