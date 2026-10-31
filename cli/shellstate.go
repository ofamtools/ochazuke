package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"ofam.dev/ochazuke"
)

// sinceRequest is a state call's --since and --since-cache (the delta
// protocol, StateDeltaVersion).
type sinceRequest struct {
	hashes map[string]string // list -> the hash the host holds, "" for none
	cache  string
}

// speaksDelta reports whether op's call takes --since: a state verb, the
// protocol on (StateCache.Off unset), and no parameter of op's own by the
// flag's name.
func (s *Shell) speaksDelta(op ochazuke.Operation, flag string) bool {
	return op.State && !s.StateCache.Off && !hasParameter(op, flag)
}

// store is the cache scope of a state call.
func (s *Shell) store(q ochazuke.Request) (stateStore, error) {
	dir, err := s.StateCache.dir(s.Bin)
	if err != nil {
		return stateStore{}, err
	}
	return stateStore{root: dir, scope: s.StateCache.scopeID(s.Bin, q.Key(), q.Args), keep: s.StateCache.keep()}, nil
}

// stateShortcut answers every list unchanged without running the verb when
// the tool's Fingerprint equals the previous answer's and the host holds
// every list at that answer's hash. fingerprint is the inputs' fingerprint
// now ("" when there is none), which the answer records.
func (s *Shell) stateShortcut(q ochazuke.Request, since sinceRequest) (lines Lines, fingerprint string, ok bool) {
	if fresh, _ := q.Args["fresh"].(bool); s.StateCache.Fingerprint == nil || fresh {
		return nil, "", false
	}
	fp, err := s.StateCache.Fingerprint(q.Args)
	if err != nil || fp == "" {
		return nil, "", false
	}
	st, err := s.store(q)
	if err != nil || since.cache != st.scope {
		return nil, fp, false
	}
	last, ok := st.readLast()
	if !ok || last.Fingerprint != fp {
		return nil, fp, false
	}
	names := make([]string, 0, len(last.Lists))
	for name, l := range last.Lists {
		if since.hashes[name] != l.Hash {
			return nil, fp, false
		}
		names = append(names, name)
	}
	for name := range since.hashes {
		if _, ok := last.Lists[name]; !ok {
			return nil, fp, false // a list the host names that the answer had not
		}
	}
	sort.Strings(names)
	if err := st.touch(s.StateCache.scopes()); err != nil {
		fmt.Fprintln(s.stderr(), "warning: state cache:", err)
	}
	lines = Lines{stateHeader{State: StateDeltaVersion, Cache: st.scope}}
	for _, name := range names {
		l := last.Lists[name]
		lines = append(lines, StateList{List: name, Answer: AnswerUnchanged, Hash: l.Hash, Rows: l.Rows})
	}
	return lines, fp, true
}

// stateDelta turns a state verb's answer into a delta answer against what
// the host holds (since), and stores the answer's versions for the next
// call. A cache that cannot be read answers in full; one that cannot be
// written is a warning on Stderr, the answer stands.
func (s *Shell) stateDelta(q ochazuke.Request, since sinceRequest, fingerprint string, value any) (Lines, error) {
	encoded, err := encodedLines(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", q.Key(), err)
	}
	order, lists := splitState(encoded)
	st, serr := s.store(q)
	// The lists: the answer's in order, then the previous answer's and the
	// host's that are empty now (each answered, so the host empties them).
	var last lastAnswer
	if serr == nil {
		last, _ = st.readLast()
	}
	var extra []string
	for name := range last.Lists {
		extra = append(extra, name)
	}
	for name := range since.hashes {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		if lists[name] == nil {
			lists[name] = &currentList{hash: ListHash(nil)}
			order = append(order, name)
		}
	}
	trusted := serr == nil && since.cache == st.scope
	out := Lines{stateHeader{State: StateDeltaVersion}}
	next := lastAnswer{Fingerprint: fingerprint, Lists: map[string]lastEntry{}}
	var warnings []error
	for _, name := range order {
		cur := lists[name]
		base := since.hashes[name]
		var v version
		var ok bool
		if trusted && base != "" && base != cur.hash {
			v, ok = st.read(name, base)
		}
		if !trusted {
			base = ""
		}
		head, rows := diffList(name, cur, base, v, ok)
		out = append(out, head)
		for _, row := range rows {
			out = append(out, row)
		}
		next.Lists[name] = lastEntry{Hash: cur.hash, Rows: len(cur.records)}
		if serr == nil {
			warnings = append(warnings, st.write(name, cur.hash, cur.version()))
		}
	}
	if serr == nil {
		warnings = append(warnings, st.writeLast(next), st.touch(s.StateCache.scopes()))
		out[0] = stateHeader{State: StateDeltaVersion, Cache: st.scope}
	} else {
		warnings = append(warnings, serr)
	}
	if err := errors.Join(warnings...); err != nil {
		fmt.Fprintln(s.stderr(), "warning: state cache:", err)
	}
	return out, nil
}

// encodedLines is a state verb's answer as one encoded JSON value per row:
// Lines (each line encoded as the shell prints it) or a JSON array.
func encodedLines(value any) ([]json.RawMessage, error) {
	if lines, ok := value.(Lines); ok {
		out := make([]json.RawMessage, len(lines))
		for i, line := range lines {
			if raw, ok := line.(json.RawMessage); ok && bytes.IndexByte(raw, '\n') < 0 {
				out[i] = raw // HandleCall's copy: encoded and on one line
				continue
			}
			data, err := json.Marshal(line)
			if err != nil {
				return nil, err
			}
			out[i] = data
		}
		return out, nil
	}
	data, ok := value.(json.RawMessage)
	if !ok {
		var err error
		if data, err = json.Marshal(value); err != nil {
			return nil, err
		}
	}
	if string(data) == "null" || len(data) == 0 {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, errors.New("a state answer is Lines or an array of rows")
	}
	for i, item := range items {
		data, err := json.Marshal(item) // compact, as the shell prints a line
		if err != nil {
			return nil, err
		}
		items[i] = data
	}
	return items, nil
}

// printVerbatim prints a delta answer: an encoded line (json.RawMessage)
// byte for byte, so the bytes a host hashes are the bytes the record hash
// was taken of; any other line as JSON.
func (s *Shell) printVerbatim(lines Lines) error {
	w := bufio.NewWriter(s.stdout())
	for _, line := range lines {
		data, ok := line.(json.RawMessage)
		if !ok {
			var err error
			if data, err = json.Marshal(line); err != nil {
				return err
			}
		}
		w.Write(data)
		w.WriteByte('\n')
	}
	return w.Flush()
}
