package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"ofam.dev/ochazuke"
)

// The state delta protocol (CatalogDoc.StateDelta). A host that has a
// state answer's lists asks for what changed since:
//
//	BIN NOUN VERB --json '{}' --since-cache CACHE --since skill=HASH,thinking=HASH
//
// --since names each list (a Row.Noun) and the content hash the host holds
// of it (StateList.Hash); --since= (empty) asks for every list in full with
// hashes, the first call. --since-cache is the cache id of the answer those
// hashes came from (StateAnswer.Cache). The answer is JSON Lines: a header,
// then per list a StateList line and the rows that follow it:
//
//	{"state":1,"cache":"9f2c1a0b7d3e4f56"}
//	{"list":"skill","answer":"unchanged","hash":"ab…","rows":1204}
//	{"list":"thinking","answer":"delta","base":"cd…","hash":"ef…","rows":96,"at":[3],"removed":["thinking/old"]}
//	{"noun":"thinking","id":"thinking/new","label":"…","data":{…}}
//	{"list":"agent","answer":"full","hash":"01…","rows":2}
//	{"noun":"agent","id":"agent/a1","label":"ada"}
//	{"noun":"agent","id":"agent/a2","label":"bo"}
//
// unchanged: the host's rows stand. delta: the rows that changed or were
// added follow, whole, at the indexes At names (ochazuke.ListPatch), and
// Removed are the ids that went away; the host held Base. full: every row
// follows. The extension answers full when it cannot answer a delta: no
// hash or an unknown one (a version it no longer keeps), another cache id,
// or a reorder a patch cannot say. Hash is the list's content hash after
// the answer (ListHash over the record hashes in order, a record's hash
// RecordHash over its line's bytes): a host recomputes it after applying
// and fetches the list in full on a mismatch. Rows is the list's length.
//
// Every row line is the line a plain state answer prints, so an old host
// (no --since) gets the old answer and its RowReader skips the header and
// list lines it does not know.
const StateDeltaVersion = 1

// The answers a StateList carries.
const (
	AnswerUnchanged = "unchanged"
	AnswerDelta     = "delta"
	AnswerFull      = "full"
)

// The shell flags of a state delta request.
const (
	SinceFlag      = "since"
	SinceCacheFlag = "since-cache"
)

// StateList is one list's line of a delta answer.
type StateList struct {
	List    string   `json:"list"`
	Answer  string   `json:"answer"`
	Base    string   `json:"base,omitempty"`
	Hash    string   `json:"hash"`
	Rows    int      `json:"rows"`
	At      []int    `json:"at,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// stateHeader is a delta answer's first line.
type stateHeader struct {
	State int    `json:"state"`
	Cache string `json:"cache"`
}

// StateAnswer is a decoded state answer (DecodeState, RowReader.Answer).
// Delta is false for a plain answer (an extension without the protocol,
// or no --since): Rows are then every list's rows and Lists is empty.
// Hashes holds each row's RecordHash in a delta answer, by index.
type StateAnswer struct {
	Delta  bool
	Cache  string
	Lists  []StateList
	Rows   []Row
	Hashes []string
}

// List is the answer for list (a Row.Noun), if the answer has one.
func (a StateAnswer) List(list string) (StateList, bool) {
	for _, l := range a.Lists {
		if l.List == list {
			return l, true
		}
	}
	return StateList{}, false
}

// Hash is the whole answer's content hash: its lists' hashes by list name.
// A history entry's reference to the response it got (HistoryResult) and a
// replay trace would record this; neither is built yet.
func (a StateAnswer) Hash() string {
	if !a.Delta {
		return ""
	}
	names := make([]string, 0, len(a.Lists))
	by := map[string]string{}
	for _, l := range a.Lists {
		names = append(names, l.List)
		by[l.List] = l.Hash
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s=%s\n", n, by[n])
	}
	return hex.EncodeToString(h.Sum(nil)[:hashBytes])
}

// hashBytes is how much of a SHA-256 a hash keeps (128 bits, 32 hex digits).
const hashBytes = 16

// RecordHash is a row's hash: SHA-256 (128 bits, hex) of its line's bytes
// as the state answer prints them, surrounding space trimmed. A host hashes
// the line it read, no re-encoding.
func RecordHash(line []byte) string { return ochazuke.RecordHash(line) }

// ListHash is a list's content hash: SHA-256 (128 bits, hex) of its rows'
// record hashes in order, one per line. An empty list hashes too.
func ListHash(records []string) string { return ochazuke.ListHash(records) }

var hashPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// validHash reports whether s is a hash this protocol writes (it names a
// file in the extension's cache, so nothing else is accepted).
func validHash(s string) bool { return hashPattern.MatchString(s) }

// FormatSince is the --since value for hashes by list: list=hash pairs,
// sorted, comma separated; a list with no hash is named with none (it is
// answered in full).
func FormatSince(hashes map[string]string) string {
	names := make([]string, 0, len(hashes))
	for n := range hashes {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n + "=" + hashes[n]
	}
	return strings.Join(parts, ",")
}

// ParseSince reads a --since value (FormatSince): list=hash pairs. An
// empty value is no hashes; a pair whose hash is not one this protocol
// writes keeps the list with no hash.
func ParseSince(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		list, hash, ok := strings.Cut(part, "=")
		if !ok || list == "" {
			return nil, fmt.Errorf("--since %q: want LIST=HASH[,LIST=HASH...]", s)
		}
		if !validHash(hash) {
			hash = ""
		}
		out[list] = hash
	}
	return out, nil
}

// StateFlags are the shell flags asking for a delta against hashes (by
// list) of the answer with cache id cache: --since-cache (when known) and
// --since.
func StateFlags(cache string, hashes map[string]string) []string {
	var out []string
	if cache != "" {
		out = append(out, "--"+SinceCacheFlag, cache)
	}
	return append(out, "--"+SinceFlag+"="+FormatSince(hashes))
}

// stateLine is a decoded line of a state answer: a header, a list line or
// a row (with its record hash), else nothing known.
type stateLine struct {
	header *stateHeader
	list   *StateList
	row    *Row
	hash   string
}

// anyLine is every field a state answer's lines have, so one decode
// tells a header, a list line and a row apart.
type anyLine struct {
	State *int   `json:"state"`
	Cache string `json:"cache"`
	StateList
	Noun   string              `json:"noun"`
	ID     string              `json:"id"`
	Parent string              `json:"parent"`
	Label  string              `json:"label"`
	Data   any                 `json:"data"`
	Cells  []string            `json:"cells"`
	Tones  []ochazuke.ToneSpan `json:"tones"`
}

// decodeStateLine reads one line. hashRows asks for a row's record hash
// (a delta answer's rows).
func decodeStateLine(line []byte, hashRows bool) (stateLine, bool) {
	var l anyLine
	if json.Unmarshal(line, &l) != nil {
		return stateLine{}, false
	}
	switch {
	case l.State != nil:
		return stateLine{header: &stateHeader{State: *l.State, Cache: l.Cache}}, true
	case l.List != "" && l.Noun == "":
		return stateLine{list: &l.StateList}, true
	case l.Noun != "" && l.ID != "":
		out := stateLine{row: &Row{Noun: l.Noun, ID: l.ID, Parent: l.Parent, Label: l.Label, Data: l.Data, Cells: l.Cells, Tones: l.Tones}}
		if hashRows {
			out.hash = RecordHash(line)
		}
		return out, true
	}
	return stateLine{}, false
}

// DecodeState reads a state answer: a delta answer (the header first) into
// its lists, rows and record hashes; any other answer as DecodeRows reads
// it, every row of every list (Delta false).
func DecodeState(out []byte) (StateAnswer, error) {
	trimmed := bytes.TrimSpace(out)
	first, _, _ := bytes.Cut(trimmed, []byte("\n"))
	if l, ok := decodeStateLine(first, false); !ok || l.header == nil {
		rows, err := DecodeRows("", out)
		return StateAnswer{Rows: rows}, err
	}
	a := StateAnswer{Delta: true}
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		a.add(line)
	}
	return a, nil
}

// add files one line of a delta answer; a line it does not know is skipped.
func (a *StateAnswer) add(line []byte) {
	l, ok := decodeStateLine(line, true)
	switch {
	case !ok:
	case l.header != nil:
		a.Delta, a.Cache = true, l.header.Cache
	case l.list != nil:
		a.Lists = append(a.Lists, *l.list)
	case l.row != nil:
		a.Rows = append(a.Rows, *l.row)
		a.Hashes = append(a.Hashes, l.hash)
	}
}
