package ochazuke

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The read cache (background-reads-and-cache plan, lane read-cache; owner:
// "I want to cache a lot of these values even just to disk ... the data
// source ... should have some key ... so that we can quickly see whether or
// not we need to update our cache").
//
// What a From list last drew (its rows, total and page) and what the Nouns
// page last counted are kept per store on disk, in
// <state>/read-cache/<store>.json, each answer with the store's ChangeToken
// it was read under. A list drawn before its store has answered (a new
// list, the first frame after a start) draws the kept rows at once:
//
//   - the store's token now is the kept one: the rows are current, and the
//     store is not read (a local store's token is read on the loop; a
//     remote store's inside the read's job, which then reads nothing more);
//   - the token moved, or the store keeps none: the rows are drawn marked
//     stale (cache.stale-mark) and the store is read off the loop as
//     before; when the answer lands the rows update in place (SetItems
//     keeps the selection by id) and the answer is kept.
//
// The cache is never the source of truth: a file that does not read, has
// another version or is larger than cache.disk-max is ignored, and is
// replaced at the next write. Nothing here names a store, a page or a noun:
// every From list and every Nouns page count goes through it, and
// cache.skip keeps named nouns off disk.
//
// Limits: an answer larger than cache.entry-max is not kept; past
// cache.memory-max the least recently used stores leave memory (read again
// from their files on next use); past cache.disk-max the writer removes the
// least recently written files; the retention row read-cache removes a file
// not written for 14 days. The writer is one goroutine, off the loop,
// writing at most every ReadCacheWriteEvery and on Config.Flush (quit).
//
// The refresher (lane read-cache-refresher) walks the entries
// (Runtime.readCacheEntries: store, key, token, read and used times, hits,
// how long the read took) in its order and reads again those whose token
// moved; it lands answers through the same lists, so nothing else changes.

const (
	// ReadCacheDir is the cache's folder in the state directory.
	ReadCacheDir = "read-cache"
	// ReadCacheVersion is the files' format; another version is ignored.
	ReadCacheVersion = 1
	// ReadCacheWriteEvery is the least time between two writes while the
	// Model runs; Config.Flush writes what is left.
	ReadCacheWriteEvery = 2 * time.Second
	// ReadCacheNoun is the framework noun of the kept answers.
	ReadCacheNoun = "ochazuke.read-cache"

	// ReadCacheDiskSetting turns the disk cache on (default) or off: off,
	// nothing is read from or written to the folder, and lists read their
	// stores as before.
	ReadCacheDiskSetting = "cache.disk"
	// ReadCacheSkipSetting names nouns whose answers are never kept.
	ReadCacheSkipSetting = "cache.skip"
	// ReadCacheStaleMarkSetting is how a list drawing kept rows whose
	// store has not confirmed them says so: age ("cached 3m"), dot, none.
	ReadCacheStaleMarkSetting = "cache.stale-mark"
	// ReadCacheDiskMaxSetting, ReadCacheMemoryMaxSetting and
	// ReadCacheEntryMaxSetting are the limits, in MB.
	ReadCacheDiskMaxSetting   = "cache.disk-max"
	ReadCacheMemoryMaxSetting = "cache.memory-max"
	ReadCacheEntryMaxSetting  = "cache.entry-max"

	// readCacheEntryLimit bounds the entries kept per store (the
	// remoteSeenLimit of the in-memory answers): past it the least
	// recently used is dropped.
	readCacheEntryLimit = remoteSeenLimit
	// readCacheCountKey is the key of a store's Nouns page count.
	readCacheCountKey = "count"
)

// readCache is the cache's state: the stores read from disk so far, and
// the writer.
type readCache struct {
	dir     string // the folder at the last use; "" keeps nothing
	diskMax int64  // cache.disk-max at the last use, for the writer
	stores  map[string]*cacheStore
	bytes   int // the loaded stores' bytes
	writer  *cacheWriter
	sent    time.Time
	now     func() time.Time
	// registryToken is RegistryToken at registryStamp.
	registryToken string
	registryStamp [3]int
}

// cacheStore is one store's kept answers, by key.
type cacheStore struct {
	name    string
	entries map[string]*cacheEntry
	bytes   int
	used    time.Time
	dirty   bool
}

// cacheEntry is one kept answer: a list's rows (Rows, Total, Archived,
// Page) or a count (Total, Archived), the token it was read under, when it
// was read, how long the read took, and when and how often it was used.
type cacheEntry struct {
	Key      string          `json:"key"`
	Token    string          `json:"token,omitempty"`
	ReadAt   time.Time       `json:"read_at"`
	UsedAt   time.Time       `json:"used_at"`
	TookMS   int64           `json:"took_ms"`
	Hits     int             `json:"hits"`
	Total    int             `json:"total"`
	Archived *int            `json:"archived,omitempty"`
	Page     int             `json:"page,omitempty"`
	Rows     json.RawMessage `json:"rows,omitempty"`
}

func (e *cacheEntry) size() int { return len(e.Rows) + len(e.Key) + len(e.Token) + 96 }

// cacheFile is one store's file.
type cacheFile struct {
	Version int           `json:"version"`
	Store   string        `json:"store"`
	Entries []*cacheEntry `json:"entries"`
}

// readCache is the runtime's cache, nil without a config.
func (r *Runtime) readCache() *readCache {
	c := r.configMade()
	if c == nil {
		return nil
	}
	if c.cache == nil {
		c.cache = &readCache{stores: map[string]*cacheStore{}, now: time.Now}
	}
	return c.cache
}

// ReadCachePath is the cache's folder, "" when nothing is kept (no state
// directory: Config.Disable, a test binary).
func (c *Config) ReadCachePath() string {
	dir := c.StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ReadCacheDir)
}

// on reports whether answers of noun are read from and kept on disk.
func (r *Runtime) cacheOn(noun string) *readCache {
	rc := r.readCache()
	if rc == nil {
		return nil
	}
	rc.dir = r.Config().ReadCachePath()
	rc.diskMax = int64(megabytes(r, ReadCacheDiskMaxSetting))
	if rc.dir == "" || !r.Settings().Bool(ReadCacheDiskSetting) || slices.Contains(r.Settings().Strings(ReadCacheSkipSetting), noun) {
		return nil
	}
	return rc
}

func megabytes(r *Runtime, setting string) int { return max(0, r.Settings().Int(setting)) << 20 }

// storeFile is the file of a store's answers: its name with anything but
// letters, digits, dot, dash and underscore as _.
func (rc *readCache) storeFile(store string) string {
	name := strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
			return c
		}
		return '_'
	}, store)
	return filepath.Join(rc.dir, name+".json")
}

// store is a store's kept answers, read from its file on first use. A file
// that does not read, has another version, names another store or is
// larger than cache.disk-max is ignored (and replaced at the next write).
func (rc *readCache) store(r *Runtime, name string) *cacheStore {
	if s := rc.stores[name]; s != nil {
		s.used = rc.now()
		return s
	}
	s := &cacheStore{name: name, entries: map[string]*cacheEntry{}, used: rc.now()}
	rc.stores[name] = s
	path := rc.storeFile(name)
	if info, err := os.Stat(path); err == nil && info.Size() <= int64(megabytes(r, ReadCacheDiskMaxSetting)) {
		if data, err := os.ReadFile(path); err == nil {
			var f cacheFile
			if json.Unmarshal(data, &f) == nil && f.Version == ReadCacheVersion && f.Store == name {
				for _, e := range f.Entries {
					if e != nil && e.Key != "" {
						s.entries[e.Key] = e
						s.bytes += e.size()
					}
				}
			}
		}
	}
	rc.bytes += s.bytes
	rc.trimMemory(r, name)
	return s
}

// get is the kept answer of store under key, nil when none.
func (r *Runtime) cacheGet(store, key string) *cacheEntry {
	rc := r.cacheOn(store)
	if rc == nil {
		return nil
	}
	return rc.store(r, store).entries[key]
}

// used notes a kept answer was drawn (its hits, for the refresher's order).
func (r *Runtime) cacheUsed(store string, e *cacheEntry) {
	rc := r.cacheOn(store)
	if rc == nil || e == nil {
		return
	}
	e.Hits++
	e.UsedAt = rc.now()
	rc.store(r, store).dirty = true
}

// cachePut keeps an answer: rows (encoded here, on the loop) or none for a
// count. An answer larger than cache.entry-max is not kept (and what was
// kept under its key is dropped).
func (r *Runtime) cachePut(store string, e cacheEntry, rows []ListItem) {
	rc := r.cacheOn(store)
	if rc == nil {
		return
	}
	s := rc.store(r, store)
	if old := s.entries[e.Key]; old != nil && old.Token != "" && old.Token == e.Token && old.Total == e.Total {
		old.ReadAt, old.UsedAt, old.TookMS = rc.now(), rc.now(), e.TookMS // the same answer: not encoded again
		s.dirty = true
		return
	}
	if rows != nil {
		raw, err := json.Marshal(rows)
		if err != nil {
			return // an answer that cannot be written is read again next time
		}
		e.Rows = raw
	}
	old := s.entries[e.Key]
	if old != nil {
		s.bytes -= old.size()
		rc.bytes -= old.size()
		e.Hits = old.Hits
	}
	if e.size() > megabytes(r, ReadCacheEntryMaxSetting) {
		if old != nil {
			delete(s.entries, e.Key)
			s.dirty = true
		}
		return
	}
	now := rc.now()
	e.ReadAt, e.UsedAt = now, now
	s.entries[e.Key] = &e
	s.bytes += e.size()
	rc.bytes += e.size()
	s.dirty = true
	if len(s.entries) > readCacheEntryLimit {
		oldest := ""
		for k, o := range s.entries {
			if oldest == "" || o.UsedAt.Before(s.entries[oldest].UsedAt) {
				oldest = k
			}
		}
		s.bytes -= s.entries[oldest].size()
		rc.bytes -= s.entries[oldest].size()
		delete(s.entries, oldest)
	}
	rc.trimMemory(r, store)
	r.settleReadCache()
}

// cacheForget drops what is kept of a store (a write through a remote
// store, which may have changed anything of it): its file too.
func (r *Runtime) cacheForget(store string) {
	rc := r.cacheOn(store)
	if rc == nil {
		return
	}
	s := rc.store(r, store)
	if len(s.entries) == 0 {
		return
	}
	rc.bytes -= s.bytes
	s.entries, s.bytes, s.dirty = map[string]*cacheEntry{}, 0, true
}

// trimMemory lets the least recently used stores leave memory while the
// loaded stores pass cache.memory-max; keep (the store in use) and a store
// with unwritten answers stay.
func (rc *readCache) trimMemory(r *Runtime, keep string) {
	limit := megabytes(r, ReadCacheMemoryMaxSetting)
	for rc.bytes > limit {
		var drop *cacheStore
		for _, s := range rc.stores {
			if s.name == keep || s.dirty {
				continue
			}
			if drop == nil || s.used.Before(drop.used) {
				drop = s
			}
		}
		if drop == nil {
			return
		}
		rc.bytes -= drop.bytes
		delete(rc.stores, drop.name)
	}
}

// settleReadCache hands the stores with unwritten answers to the writer,
// at most every ReadCacheWriteEvery; the Model calls it after each update.
func (r *Runtime) settleReadCache() {
	c := r.configMade()
	if c == nil || c.cache == nil {
		return
	}
	rc := c.cache
	if rc.now().Sub(rc.sent) < ReadCacheWriteEvery {
		return
	}
	rc.send(false)
}

// send writes the dirty stores (off the loop unless final).
func (rc *readCache) send(final bool) {
	var ops []cacheWrite
	for _, s := range rc.stores {
		if !s.dirty {
			continue
		}
		op := cacheWrite{store: s.name, path: rc.storeFile(s.name)}
		if len(s.entries) > 0 {
			f := cacheFile{Version: ReadCacheVersion, Store: s.name}
			for _, k := range slices.Sorted(maps.Keys(s.entries)) {
				f.Entries = append(f.Entries, s.entries[k])
			}
			data, err := json.Marshal(f)
			if err != nil {
				continue
			}
			op.data = data
		}
		ops = append(ops, op)
		s.dirty = false
	}
	rc.sent = rc.now()
	if len(ops) == 0 {
		return
	}
	if rc.writer == nil {
		rc.writer = &cacheWriter{ops: make(chan cacheBatch, 4), done: make(chan struct{})}
		go rc.writer.run()
	}
	batch := cacheBatch{writes: ops, diskMax: rc.diskMax}
	if final {
		rc.writer.ops <- batch
		return
	}
	select {
	case rc.writer.ops <- batch:
	default:
		for _, op := range ops { // the writer is busy: written with the next batch
			if s := rc.stores[op.store]; s != nil {
				s.dirty = true
			}
		}
	}
}

// flush writes what is left and stops the writer; Config.Flush calls it.
func (rc *readCache) flush() error {
	if rc == nil {
		return nil
	}
	if rc.dir != "" {
		rc.send(true)
	}
	w := rc.writer
	if w == nil {
		return nil
	}
	rc.writer = nil
	close(w.ops)
	<-w.done
	return w.failure()
}

// cacheWrite is one file to write (data) or remove (no data).
type cacheWrite struct {
	store, path string
	data        []byte
}

type cacheBatch struct {
	writes  []cacheWrite
	diskMax int64
}

// cacheWriter writes batches off the loop: each file whole to a temporary
// file renamed over it (0600 in a 0700 folder), then the folder kept under
// cache.disk-max by removing the least recently written files.
type cacheWriter struct {
	ops  chan cacheBatch
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func (w *cacheWriter) run() {
	defer close(w.done)
	for b := range w.ops {
		var written []string
		for _, op := range b.writes {
			if op.data == nil {
				if err := os.Remove(op.path); err != nil && !errors.Is(err, os.ErrNotExist) {
					w.fail(err)
				}
				continue
			}
			if err := writeCacheFile(op.path, op.data); err != nil {
				w.fail(err)
				continue
			}
			written = append(written, op.path)
		}
		if len(b.writes) > 0 {
			trimCacheDir(filepath.Dir(b.writes[0].path), b.diskMax, written)
		}
	}
}

func (w *cacheWriter) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

func (w *cacheWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func writeCacheFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".read-cache-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o600)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// trimCacheDir removes the least recently written files of the folder
// while it holds more than max bytes; the files just written stay.
func trimCacheDir(dir string, limit int64, keep []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	slices.SortFunc(files, func(a, b file) int { return a.mod.Compare(b.mod) })
	for _, f := range files {
		if total <= limit {
			return
		}
		if slices.Contains(keep, f.path) {
			continue
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// listCacheKey is the key a From list's rows are kept under: the list, the
// columns its cells were made for, and the query its rows answer (a
// windowed list's page query; all of the store for the rest).
func (l *Collection) listCacheKey() string {
	titles := make([]string, len(l.columns))
	for i, col := range l.columns {
		titles[i] = col.Title
	}
	key := l.id + "|" + strings.Join(titles, ",") + "|"
	if l.paged != nil {
		return key + windowKey(l.windowQuery())
	}
	return key + "all"
}

// seedFromCache draws the rows kept for the list, before its store has
// answered, and reports whether there were any: their token is the one
// the read compares (seedToken; a windowed list's window token) so an
// unmoved store is not read, and until the store confirms or replaces them
// the rows are marked (cachedAt).
func (l *Collection) seedFromCache(n *Noun) bool {
	if l.refills > 0 || l.from == "" {
		return false
	}
	r := l.r
	e := r.cacheGet(n.Name, l.listCacheKey())
	if e == nil || len(e.Rows) == 0 {
		return false
	}
	var items []ListItem
	if json.Unmarshal(e.Rows, &items) != nil {
		return false
	}
	for i := range items {
		if m, ok := items[i].Data.(map[string]any); ok {
			items[i].Data = Thing(m)
		}
	}
	if w := l.paged; w != nil {
		w.total, w.key, w.token, w.filled = e.Total, windowKey(l.windowQuery()), e.Token, true
		w.archived = -1
		if e.Archived != nil {
			w.archived = *e.Archived
		}
	}
	if err := l.SetItems(items); err != nil {
		return false
	}
	l.seedToken, l.cachedAt = e.Token, e.ReadAt
	r.cacheUsed(n.Name, e)
	return true
}

// cacheFresh notes the list's rows are its store's answer now: confirmed by
// the token, or just read.
func (l *Collection) cacheFresh() { l.seedToken, l.cachedAt = "", time.Time{} }

// unmovedRead ends a read that failed (err) or found its store unmoved:
// the rows drawn are the store's when it did not fail.
func (l *Collection) unmovedRead(err error) error {
	if err == nil {
		l.cacheFresh()
	}
	return err
}

// countedArchived is an archived count as kept: nil when not counted (-1).
func countedArchived(n int) *int {
	if n < 0 {
		return nil
	}
	return &n
}

// keepRows keeps the rows a read of the list just drew, with the token it
// was read under and how long it took.
func (l *Collection) keepRows(n *Noun, token string, items []ListItem, total int, archived *int, start time.Time) {
	l.cacheFresh()
	e := cacheEntry{Key: l.listCacheKey(), Token: token, Total: total, Archived: archived, TookMS: time.Since(start).Milliseconds()}
	if l.paged != nil {
		e.Page = l.paged.page
	}
	if items == nil {
		items = []ListItem{}
	}
	l.r.cachePut(n.Name, e, items)
}

// MarkCached marks the rows the list draws as the read cache's, read at
// readAt: they are drawn with the stale mark (cache.stale-mark) until the
// next SetItems. A host that fills a list itself from a kept answer
// (CachedAnswer) calls it after SetItems.
func (l *Collection) MarkCached(readAt time.Time) { l.cachedAt = readAt }

// CachedAnswer decodes the answer kept for noun under key into into, and
// reports when it was read and the token it was read under; false when
// none is kept, it does not decode, or the cache is off for the noun
// (cache.disk, cache.skip). A host that reads a source itself (an
// extension page's rows) keeps its answers with KeepAnswer and draws them
// from here until the source answers.
func (r *Runtime) CachedAnswer(noun, key string, into any) (readAt time.Time, token string, ok bool) {
	e := r.cacheGet(noun, key)
	if e == nil || len(e.Rows) == 0 || json.Unmarshal(e.Rows, into) != nil {
		return time.Time{}, "", false
	}
	r.cacheUsed(noun, e)
	return e.ReadAt, e.Token, true
}

// KeepAnswer keeps answer (anything JSON encodes) for noun under key, read
// under token ("" when the source keeps none), as CachedAnswer reads it.
func (r *Runtime) KeepAnswer(noun, key, token string, answer any, took time.Duration) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return
	}
	r.cachePut(noun, cacheEntry{Key: key, Token: token, TookMS: took.Milliseconds(), Rows: raw}, nil)
}

// CachedAge is how old the kept rows the list draws are, 0 when its rows
// are its store's answer (read, or confirmed by the store's token). An
// application drawing its own panel title may show it there.
func (l *Collection) CachedAge() time.Duration {
	if l.cachedAt.IsZero() {
		return 0
	}
	return max(time.Since(l.cachedAt), time.Second)
}

// cacheSummary is the list's stale mark (cache.stale-mark) while it draws
// kept rows its store has not confirmed: "cached 3m", a dot, or nothing.
func (l *Collection) cacheSummary() string {
	age := l.CachedAge()
	if age == 0 {
		return ""
	}
	switch l.r.Settings().String(ReadCacheStaleMarkSetting) {
	case "age":
		return "cached " + shortDuration(age)
	case "dot":
		return "•"
	}
	return ""
}

// shortDuration is d in its largest whole unit: 40s, 3m, 2h, 5d.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// seedCountFromCache gives a count not read in this run what was kept of
// it, so the token comparison decides whether to read the store.
func (r *Runtime) seedCountFromCache(n *Noun, c *nounCount) {
	if c.known || c.token != "" {
		return
	}
	e := r.cacheGet(n.Name, readCacheCountKey)
	if e == nil || e.Token == "" {
		return
	}
	c.count, c.known, c.token = e.Total, true, e.Token
	if e.Archived != nil {
		c.archived, c.hasArch = *e.Archived, true
	}
	r.cacheUsed(n.Name, e)
}

// keepCount keeps a count just read.
func (r *Runtime) keepCount(n *Noun, c *nounCount, took time.Duration) {
	if c.token == "" {
		return // a store with no token is counted again each time
	}
	e := cacheEntry{Key: readCacheCountKey, Token: c.token, Total: c.count, TookMS: took.Milliseconds()}
	if c.hasArch {
		archived := c.archived
		e.Archived = &archived
	}
	r.cachePut(n.Name, e, nil)
}

// RegistryToken is a change token of the operation registry and the nouns:
// a hash of every operation's key, description and parameter count and
// every noun's name and about, so it is
// the same in every process with the same registry. It is worked out again
// only when the registry moved (Operations.Invalidations, Keys, the noun
// count); a facts store over the registry (ochazuke.operation, the
// catalog) answers it as its ChangeToken.
func (r *Runtime) RegistryToken() string {
	stamp := [3]int{r.Operations.Invalidations, r.Operations.Keys, len(r.Nouns)}
	rc := r.readCache()
	if rc != nil && rc.registryToken != "" && rc.registryStamp == stamp {
		return rc.registryToken
	}
	h := fnv.New64a()
	type named struct {
		text string
		op   *Operation
	}
	ops := make([]named, 0, len(r.Operations.All))
	for k, op := range r.Operations.All {
		ops = append(ops, named{k.String(), op})
	}
	slices.SortFunc(ops, func(a, b named) int { return strings.Compare(a.text, b.text) })
	for _, one := range ops {
		h.Write([]byte(one.text))
		h.Write([]byte{0})
		if op := one.op; op != nil {
			h.Write([]byte(op.Description))
			h.Write([]byte{byte(len(op.Parameters))})
		}
		h.Write([]byte{0})
	}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		h.Write([]byte(name))
		h.Write([]byte{1})
		if n := r.Nouns[name]; n != nil {
			h.Write([]byte(n.About))
		}
		h.Write([]byte{0})
	}
	token := strconv.FormatUint(h.Sum64(), 36)
	if rc != nil {
		rc.registryToken, rc.registryStamp = token, stamp
	}
	return token
}

// registryToken is RegistryToken as a store's ChangeToken.
func registryToken(c Ctx) (string, error) { return c.Runtime.RegistryToken(), nil }

// readCacheFact is a kept answer as the ochazuke.read-cache noun lists it.
type readCacheFact struct {
	ID     string `json:"id"`
	Noun   string `json:"noun"`
	Source string `json:"source"`
	Token  string `json:"token"`
	State  string `json:"state"`
	ReadAt string `json:"read-at"`
	UsedAt string `json:"used-at"`
	TookMS int64  `json:"took-ms"`
	Hits   int    `json:"hits"`
	Bytes  int    `json:"bytes"`
	Rows   int    `json:"rows"`
}

// readCacheEntries are every kept answer, of the stores in memory and the
// files on disk, by store and key.
func (r *Runtime) readCacheEntries(c Ctx) []readCacheFact {
	rc := r.readCache()
	if rc == nil {
		return nil
	}
	names := map[string]bool{}
	for name := range rc.stores {
		names[name] = true
	}
	if dir := r.Config().ReadCachePath(); dir != "" {
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") && !strings.HasPrefix(f.Name(), ".") {
				if data, err := os.ReadFile(filepath.Join(dir, f.Name())); err == nil {
					var head struct {
						Store string `json:"store"`
					}
					if json.Unmarshal(data, &head) == nil && head.Store != "" {
						names[head.Store] = true
					}
				}
			}
		}
	}
	var out []readCacheFact
	for _, store := range slices.Sorted(maps.Keys(names)) {
		if r.cacheOn(store) == nil {
			continue
		}
		token := ""
		if n := r.Nouns[store]; n != nil && n.Store != nil && !n.Store.Remote {
			cx := c
			cx.Noun = store
			token = storeToken(n.Store, cx)
		}
		s := rc.store(r, store)
		for _, k := range slices.Sorted(maps.Keys(s.entries)) {
			e := s.entries[k]
			state := "unconfirmed" // a remote store's token is read only in a job
			switch {
			case token != "" && token == e.Token:
				state = "fresh"
			case token != "" || e.Token == "":
				state = "stale"
			}
			rows := 0
			if len(e.Rows) > 0 {
				var items []json.RawMessage
				if json.Unmarshal(e.Rows, &items) == nil {
					rows = len(items)
				}
			}
			out = append(out, readCacheFact{ID: store + " " + k, Noun: store, Source: k, Token: e.Token, State: state,
				ReadAt: e.ReadAt.Format(time.RFC3339), UsedAt: e.UsedAt.Format(time.RFC3339), TookMS: e.TookMS, Hits: e.Hits, Bytes: e.size(), Rows: rows})
		}
	}
	return out
}

// clearReadCache forgets every kept answer, or one store's, and removes
// their files; the next reads keep them again.
func (r *Runtime) clearReadCache(store string) (int, error) {
	rc := r.readCache()
	if rc == nil {
		return 0, nil
	}
	dir := r.Config().ReadCachePath()
	removed := 0
	for name, s := range rc.stores {
		if store != "" && name != store {
			continue
		}
		removed += len(s.entries)
		rc.bytes -= s.bytes
		delete(rc.stores, name)
	}
	if dir == "" {
		return removed, nil
	}
	if rc.writer != nil { // a batch on its way must not write the files back
		w := rc.writer
		rc.writer = nil
		close(w.ops)
		<-w.done
	}
	var errs []error
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		if store != "" && path != rc.storeFile(store) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}

// registerReadCache declares the ochazuke.read-cache noun (list, get and
// show over the kept answers) and its clear verb.
func registerReadCache(r *Runtime) {
	r.defineFacts(factsSpec{Noun: ReadCacheNoun, Sample: readCacheFact{}, Key: "id", Label: "source",
		Docs: map[string]string{"id": "The noun and the source, one space apart", "noun": "The noun whose store answered", "source": "What was read: a list and its query, or count",
			"token": "The store's change token the answer was read under", "state": "fresh (the store's token is the kept one), stale (it moved, or the store keeps none) or unconfirmed (a remote store, whose token is read only in a job)",
			"read-at": "When it was read", "used-at": "When it was last drawn or read", "took-ms": "How long the read took, in ms", "hits": "How often a list or count was drawn from it",
			"bytes": "Its size", "rows": "How many rows it holds (0 for a count)"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) { return factThings(c.Runtime.readCacheEntries(c), nil) },
	})
	r.Register(Operation{Key: KeyOf(ReadCacheNoun, "clear"), Description: "Forget every kept answer of the read cache, or one noun's (noun), and remove their files; the next reads keep them again",
		Parameters: map[string]Parameter{"noun": {Type: "string", Description: "Only this noun's kept answers (default every noun's)"}},
		Result:     "object", HistoryKind: HistoryAction, Agent: AgentAllow,
		Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			n, err := r.clearReadCache(StringArg(a, "noun", ""))
			r.Changed(ReadCacheNoun)
			return map[string]any{"cleared": n}, err
		}})
}

// EnsureReadCache declares the cache's retention row and draws the kept
// rows of every From list made before it whose store has not answered yet
// (an application builds its pages before the Model starts, maybe before
// it names its config). Model.Init calls it; lists made later are drawn
// from the cache as they are made.
func (r *Runtime) EnsureReadCache() {
	r.declareReadCacheRetention()
	for _, inst := range r.Instances {
		if l, ok := inst.Component.(*Collection); ok && l.from != "" && l.refills == 0 && len(l.all) == 0 {
			if n := r.Nouns[l.from]; n != nil && l.seedFromCache(n) {
				if t := storeToken(n.Store, r.backgroundCtx()); t != "" && t == l.seedToken {
					l.cacheFresh() // current: the read under way lands the same rows
				}
			}
		}
	}
}

// declareReadCacheRetention declares the cache's retention row: a store's
// file not written for 14 days goes.
func (r *Runtime) declareReadCacheRetention() {
	c := r.Config()
	if c == nil || c.ReadCachePath() == "" {
		return // no state directory: nothing is kept, so nothing to remove
	}
	if _, err := os.Stat(c.ReadCachePath()); err != nil {
		return // nothing kept yet: the row is declared from the next start on
	}
	_ = r.DeclareRetention(RetentionRow{Name: "read-cache", Description: "The read cache's files (" + ReadCacheNoun + "): rebuilt from the stores when gone",
		Paths: []string{filepath.Join("{state}", ReadCacheDir, "*.json")}, Days: 14})
}
