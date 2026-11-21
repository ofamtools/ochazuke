package ui

import (
	"testing"

	"ofam.dev/ochazuke/layout"
)

func cachedTree(key uint64) ViewNode {
	return ViewNode{Kind: KindVStack, Children: []ViewNode{
		txt("header"),
		{Kind: KindCard, ID: "c1", Focusable: true, CacheKey: key, Box: layout.Constraints{Flex: 1}, Title: "cached", Children: []ViewNode{txt("body")}},
		{Kind: KindCard, ID: "c2", Focusable: true, Title: "uncached"}, // no CacheKey: never cached
	}}
}

func TestRendererSubtreeCache(t *testing.T) {
	th := DefaultDarkTheme()
	r := NewRenderer(0)
	step := func(name string, root ViewNode, th Theme, focus string, w, h, hits, misses int) Frame {
		t.Helper()
		f, _ := r.Render(root, th, focus, w, h)
		if r.Stats.CacheHits != hits || r.Stats.CacheMisses != misses {
			t.Fatalf("%s: stats = %+v, want hits=%d misses=%d", name, r.Stats, hits, misses)
		}
		return f
	}

	first := step("first render", cachedTree(7), th, "", 30, 12, 0, 1)
	again := step("same inputs", cachedTree(7), th, "", 30, 12, 1, 0)
	if first.ANSI != again.ANSI {
		t.Error("a cache hit must reproduce the miss render exactly")
	}
	step("CacheKey change", cachedTree(8), th, "", 30, 12, 0, 1)
	step("new key now cached", cachedTree(8), th, "", 30, 12, 1, 0)
	step("width change", cachedTree(8), th, "", 31, 12, 0, 1)
	step("height change", cachedTree(8), th, "", 31, 14, 0, 1)
	step("focus change", cachedTree(8), th, "c1", 31, 14, 0, 1)
	step("same focus", cachedTree(8), th, "c1", 31, 14, 1, 0)
	step("unfocused state is still cached", cachedTree(8), th, "c2", 31, 14, 1, 0)

	th2 := th
	th2.Version++
	step("theme version bump", cachedTree(8), th2, "c2", 31, 14, 0, 1)
	step("same theme version", cachedTree(8), th2, "c2", 31, 14, 1, 0)

	if n := r.CacheLen(); n < 2 {
		t.Errorf("CacheLen = %d, want several entries", n)
	}
	r.Reset()
	if r.CacheLen() != 0 {
		t.Error("Reset must drop entries")
	}
}

func TestRendererCacheIsBounded(t *testing.T) {
	r := NewRenderer(2)
	for key := uint64(1); key <= 5; key++ {
		r.Render(cachedTree(key), DefaultDarkTheme(), "", 20, 10)
	}
	if r.CacheLen() != 2 {
		t.Errorf("CacheLen = %d, want 2", r.CacheLen())
	}
	// The oldest entry (key 1) was evicted, the newest is still hot.
	r.Render(cachedTree(5), DefaultDarkTheme(), "", 20, 10)
	if r.Stats.CacheHits != 1 {
		t.Errorf("newest entry should hit: %+v", r.Stats)
	}
	r.Render(cachedTree(1), DefaultDarkTheme(), "", 20, 10)
	if r.Stats.CacheMisses != 1 {
		t.Errorf("evicted entry should miss: %+v", r.Stats)
	}
}

func TestRendererSkipsCacheForVolatileSubtrees(t *testing.T) {
	r := NewRenderer(0)
	n := ViewNode{Kind: KindVStack, ID: "v", CacheKey: 1, Children: []ViewNode{{Kind: KindScroll, ID: "s", Children: []ViewNode{txt("x")}}}}
	r.Render(n, DefaultDarkTheme(), "", 10, 4)
	r.Render(n, DefaultDarkTheme(), "", 10, 4)
	if r.Stats.CacheHits != 0 || r.Stats.CacheMisses != 0 || r.CacheLen() != 0 {
		t.Errorf("subtrees containing scroll or overlay must not be cached: %+v len=%d", r.Stats, r.CacheLen())
	}
}
