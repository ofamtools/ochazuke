package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// StateCache is how an extension's shell keeps the versions of its state
// answer's lists that deltas are computed against (the --since protocol,
// StateDeltaVersion). Every field is optional: the zero value caches under
// $XDG_CACHE_HOME/<bin>/state (else ~/.cache/<bin>/state), scoped by the
// state verb and its arguments.
//
// It is a cache for hosts' screens, nothing else: per list it keeps the
// last Keep versions (default 2: two hosts a poll apart) and deletes the
// older ones, and it keeps the Scopes most recently used scopes (default
// 8). A version is a file named by its content hash holding the list's
// (id, record hash) pairs in order, written to a temporary file and
// renamed, so hosts polling at once never see half a file and two writers
// of one version write the same bytes. A version that is gone, unreadable
// or pruned by another process makes that list's answer full, never
// wrong.
type StateCache struct {
	// Dir is the cache directory; "" is $XDG_CACHE_HOME/<bin>/state, else
	// ~/.cache/<bin>/state.
	Dir string
	// Scope, when set, adds what else the answer depends on to the scope
	// (the verb and its arguments always count): a tool whose rows come from
	// a TUIOS server returns that server's name.
	Scope func(args ochazuke.Arguments) string
	// Fingerprint, when set, is a cheap summary of the answer's inputs (the
	// stat of the files it reads, StatFingerprint): when it equals the
	// previous answer's and the host holds every list at that answer's hash,
	// the shell answers every list unchanged without running the verb. An
	// error skips the shortcut. --fresh (a boolean parameter named fresh
	// set true) skips it too.
	Fingerprint func(args ochazuke.Arguments) (string, error)
	// Keep is the versions kept per list (default 2); Scopes the scopes
	// kept (default 8).
	Keep, Scopes int
	// Off turns the protocol off: --since is then an unknown flag, as with
	// an older ochazuke, and the catalog does not declare stateDelta.
	Off bool
}

const (
	defaultKeep   = 2
	defaultScopes = 8
	tempPrefix    = ".tmp-"
	versionExt    = ".jsonl"
	lastFile      = "last.json"
)

// dir is the cache directory of bin.
func (c StateCache) dir(bin string) (string, error) {
	if c.Dir != "" {
		return c.Dir, nil
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	name := filepath.Base(bin)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "ochazuke"
	}
	return filepath.Join(base, name, "state"), nil
}

// scopeID is the cache id of a state call: the verb, its arguments but
// fresh, and the tool's own Scope.
func (c StateCache) scopeID(bin, verb string, args ochazuke.Arguments) string {
	rest := ochazuke.Arguments{}
	for k, v := range args {
		if k != "fresh" {
			rest[k] = v
		}
	}
	data, _ := json.Marshal(rest) // map keys sorted
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", filepath.Base(bin), verb, data)
	if c.Scope != nil {
		h.Write([]byte(c.Scope(args)))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func (c StateCache) keep() int {
	if c.Keep > 0 {
		return c.Keep
	}
	return defaultKeep
}

func (c StateCache) scopes() int {
	if c.Scopes > 0 {
		return c.Scopes
	}
	return defaultScopes
}

// stateStore is one scope's directory: <dir>/<scope>/<list>/<hash>.jsonl
// and <dir>/<scope>/last.json.
type stateStore struct {
	root, scope string
	keep        int
}

func (s stateStore) path() string { return filepath.Join(s.root, s.scope) }

func (s stateStore) listDir(list string) string {
	return filepath.Join(s.path(), url.PathEscape(list))
}

// version is one list version: its rows' ids and record hashes in order.
type version struct {
	ids, hashes []string
}

// versionHead is a version file's first line.
type versionHead struct {
	List string `json:"list"`
	Hash string `json:"hash"`
	Rows int    `json:"rows"`
}

// read loads a version of list; a missing, unreadable or inconsistent file
// is not there (ok false).
func (s stateStore) read(list, hash string) (version, bool) {
	if !validHash(hash) {
		return version{}, false
	}
	f, err := os.Open(filepath.Join(s.listDir(list), hash+versionExt))
	if err != nil {
		return version{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var head versionHead
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &head) != nil || head.List != list || head.Hash != hash {
		return version{}, false
	}
	v := version{ids: make([]string, 0, head.Rows), hashes: make([]string, 0, head.Rows)}
	for sc.Scan() {
		var pair [2]string
		if json.Unmarshal(sc.Bytes(), &pair) != nil {
			return version{}, false
		}
		v.ids, v.hashes = append(v.ids, pair[0]), append(v.hashes, pair[1])
	}
	if sc.Err() != nil || len(v.ids) != head.Rows || ListHash(v.hashes) != hash {
		return version{}, false
	}
	return v, true
}

// write stores the version of list with hash (a present one is touched,
// so it counts as recent) and prunes the list to the newest keep versions,
// the written one among them.
func (s stateStore) write(list, hash string, v version) error {
	dir := s.listDir(list)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := filepath.Join(dir, hash+versionExt)
	now := time.Now()
	if err := os.Chtimes(name, now, now); err != nil {
		var b strings.Builder
		head, _ := json.Marshal(versionHead{List: list, Hash: hash, Rows: len(v.ids)})
		b.Write(head)
		b.WriteByte('\n')
		for i, id := range v.ids {
			pair, _ := json.Marshal([2]string{id, v.hashes[i]})
			b.Write(pair)
			b.WriteByte('\n')
		}
		if err := writeAtomic(name, []byte(b.String())); err != nil {
			return err
		}
	}
	return s.prune(dir, hash+versionExt)
}

// prune deletes all but the newest keep versions in dir (current always
// stays) and temporary files older than a minute (a writer that died).
func (s stateStore) prune(dir, current string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type file struct {
		name string
		at   time.Time
	}
	var versions []file
	var errs []error
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // pruned by another process meanwhile
		}
		switch {
		case strings.HasPrefix(e.Name(), tempPrefix):
			if time.Since(info.ModTime()) > time.Minute {
				errs = append(errs, ignoreGone(os.Remove(filepath.Join(dir, e.Name()))))
			}
		case strings.HasSuffix(e.Name(), versionExt) && e.Name() != current:
			versions = append(versions, file{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		if !versions[i].at.Equal(versions[j].at) {
			return versions[i].at.After(versions[j].at)
		}
		return versions[i].name < versions[j].name
	})
	for i, f := range versions {
		if i+1 >= s.keep { // the current version is the first kept
			errs = append(errs, ignoreGone(os.Remove(filepath.Join(dir, f.name))))
		}
	}
	return errors.Join(errs...)
}

// lastAnswer is what a scope's previous answer was: the fingerprint of its
// inputs and each list's hash and length.
type lastAnswer struct {
	Fingerprint string               `json:"fingerprint,omitempty"`
	Lists       map[string]lastEntry `json:"lists"`
}

type lastEntry struct {
	Hash string `json:"hash"`
	Rows int    `json:"rows"`
}

func (s stateStore) readLast() (lastAnswer, bool) {
	data, err := os.ReadFile(filepath.Join(s.path(), lastFile))
	if err != nil {
		return lastAnswer{}, false
	}
	var last lastAnswer
	if json.Unmarshal(data, &last) != nil || last.Lists == nil {
		return lastAnswer{}, false
	}
	return last, true
}

func (s stateStore) writeLast(last lastAnswer) error {
	data, err := json.Marshal(last)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.path(), 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.path(), lastFile), data)
}

// touch marks the scope used now and prunes the state directory to the
// newest scopes scopes, this one among them.
func (s stateStore) touch(scopes int) error {
	if err := os.MkdirAll(s.path(), 0o700); err != nil {
		return err
	}
	now := time.Now()
	if err := os.Chtimes(s.path(), now, now); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	type dir struct {
		name string
		at   time.Time
	}
	var dirs []dir
	for _, e := range entries {
		if !e.IsDir() || e.Name() == s.scope {
			continue
		}
		if info, err := e.Info(); err == nil {
			dirs = append(dirs, dir{e.Name(), info.ModTime()})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].at.After(dirs[j].at) })
	var errs []error
	for i, d := range dirs {
		if i+1 >= scopes {
			errs = append(errs, ignoreGone(os.RemoveAll(filepath.Join(s.root, d.name))))
		}
	}
	return errors.Join(errs...)
}

// writeAtomic writes data to a temporary file beside name and renames it
// over name.
func writeAtomic(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), tempPrefix+"*")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), name); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

func ignoreGone(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// StatFingerprint is a Fingerprint over files: each path's size and
// modification time (a missing one counts as missing). A directory counts
// by its own entry, which changes when a file is added or removed, not
// when one is edited: name the files whose content matters.
func StatFingerprint(paths ...string) string {
	h := sha256.New()
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(h, "%s\x00missing\n", p)
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%v\n", p, info.Size(), info.ModTime().UnixNano(), info.Mode())
	}
	return hex.EncodeToString(h.Sum(nil)[:hashBytes])
}

// stateRecord is one row line of the current answer.
type stateRecord struct {
	id   string
	hash string
	line json.RawMessage
}

// currentList is one list of the current answer, rows in order.
type currentList struct {
	records []stateRecord
	hash    string
}

func (l *currentList) version() version {
	v := version{ids: make([]string, len(l.records)), hashes: make([]string, len(l.records))}
	for i, r := range l.records {
		v.ids[i], v.hashes[i] = r.id, r.hash
	}
	return v
}

// splitState files the encoded lines of a state answer by list (Row.Noun)
// in first-seen order; a line that is not a row, or repeats a row id of
// its list, is dropped (a host's SetRows drops a repeated id too, so the
// hashes agree).
func splitState(lines []json.RawMessage) (order []string, lists map[string]*currentList) {
	lists = map[string]*currentList{}
	seen := map[string]map[string]bool{}
	for _, line := range lines {
		key, ok := rowKey(line)
		if !ok {
			continue
		}
		l := lists[key.Noun]
		if l == nil {
			l = &currentList{}
			lists[key.Noun], seen[key.Noun] = l, map[string]bool{}
			order = append(order, key.Noun)
		}
		if seen[key.Noun][key.ID] {
			continue
		}
		seen[key.Noun][key.ID] = true
		l.records = append(l.records, stateRecord{id: key.ID, hash: RecordHash(line), line: line})
	}
	for _, l := range lists {
		hashes := make([]string, len(l.records))
		for i, r := range l.records {
			hashes[i] = r.hash
		}
		l.hash = ListHash(hashes)
	}
	return order, lists
}

// diffList is the answer for one list given the version the host holds
// (base, ok false for none): unchanged, a delta, or full when a delta
// cannot say it (a reorder of unchanged rows) or is no smaller.
func diffList(name string, cur *currentList, baseHash string, base version, ok bool) (StateList, []json.RawMessage) {
	full := func() (StateList, []json.RawMessage) {
		rows := make([]json.RawMessage, len(cur.records))
		for i, r := range cur.records {
			rows[i] = r.line
		}
		return StateList{List: name, Answer: AnswerFull, Hash: cur.hash, Rows: len(cur.records)}, rows
	}
	if baseHash == cur.hash && baseHash != "" {
		return StateList{List: name, Answer: AnswerUnchanged, Hash: cur.hash, Rows: len(cur.records)}, nil
	}
	if !ok {
		return full()
	}
	old := make(map[string]string, len(base.ids))
	for i, id := range base.ids {
		old[id] = base.hashes[i]
	}
	now := make(map[string]bool, len(cur.records))
	keptSet := make(map[string]bool, len(cur.records))
	var at []int
	var rows []json.RawMessage
	var kept []string // ids neither upserted nor removed, in the new order
	for i, r := range cur.records {
		now[r.id] = true
		if h, had := old[r.id]; had && h == r.hash {
			kept, keptSet[r.id] = append(kept, r.id), true
			continue
		}
		at, rows = append(at, i), append(rows, r.line)
	}
	if len(rows) >= len(cur.records) && len(cur.records) > 0 {
		return full()
	}
	// The kept rows must come in the same relative order as before: the
	// host fills the indexes the upserts leave free with them in their old
	// order (ochazuke.ListPatch). Any other reorder is answered in full.
	var removed []string
	j := 0
	for _, id := range base.ids {
		switch {
		case !now[id]:
			removed = append(removed, id)
		case keptSet[id]:
			if j >= len(kept) || kept[j] != id {
				return full()
			}
			j++
		}
	}
	return StateList{List: name, Answer: AnswerDelta, Base: baseHash, Hash: cur.hash, Rows: len(cur.records), At: at, Removed: removed}, rows
}

// rowKeyOf is a row line's list and id.
type rowKeyOf struct{ Noun, ID string }

// rowKey reads a row line's noun and id: from the head a Row encodes
// ({"noun":"…","id":"…"…) without decoding the record, else by decoding
// the line. ok is false for a line that is not a row.
func rowKey(line []byte) (rowKeyOf, bool) {
	if rest, ok := bytes.CutPrefix(line, []byte(`{"noun":`)); ok {
		if noun, rest, ok := jsonString(rest); ok {
			if rest, ok := bytes.CutPrefix(rest, []byte(`,"id":`)); ok {
				if id, _, ok := jsonString(rest); ok && noun != "" && id != "" {
					return rowKeyOf{noun, id}, true
				}
			}
		}
	}
	var key rowKeyOf
	if json.Unmarshal(line, &key) != nil || key.Noun == "" || key.ID == "" {
		return rowKeyOf{}, false
	}
	return key, true
}

// jsonString reads the JSON string b starts with and what follows it.
func jsonString(b []byte) (string, []byte, bool) {
	if len(b) == 0 || b[0] != '"' {
		return "", nil, false
	}
	escaped := false
	for i := 1; i < len(b); i++ {
		switch b[i] {
		case '\\':
			escaped = true
			i++
		case '"':
			if !escaped {
				return string(b[1:i]), b[i+1:], true
			}
			var s string
			if json.Unmarshal(b[:i+1], &s) != nil {
				return "", nil, false
			}
			return s, b[i+1:], true
		}
	}
	return "", nil, false
}
