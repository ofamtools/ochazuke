package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/unixsuperhero/ochazuke"
)

// speaksDelta reports whether Reload asks for deltas: the host gave
// FetchFlags and the catalog declares the protocol (CatalogDoc.StateDelta).
func (v *PageView) speaksDelta() bool {
	return v.cfg.FetchFlags != nil && v.doc.StateDelta >= 1 && v.doc.State != ""
}

// SinceFlags are the flags asking the state verb for what changed since
// the rows the page holds (StateFlags): each list's content hash, none for
// a list without one (rows the host pushed, a mismatch), so that list is
// answered in full. A host that runs the state call itself (streaming it
// through a RowReader) appends them and applies RowReader.Answer with
// ApplyState.
func (v *PageView) SinceFlags() []string {
	hashes := map[string]string{}
	for _, pl := range v.lists {
		if pl.verbsListed {
			continue
		}
		if h, ok := hashes[pl.spec.Noun]; !ok || h == "" {
			hashes[pl.spec.Noun] = pl.hash
		}
	}
	return StateFlags(v.cache, hashes)
}

// ApplyState files a state answer under the page's lists by noun. A plain
// answer (Delta false) is SetState. A delta answer, per list: unchanged
// leaves the rows (no row is touched, OnRows is not called); a delta
// patches them (ochazuke.List.Patch: changed rows replaced, added ones
// placed, removed ones closed, every other row instance, the selection and
// the marks kept); full replaces them (SetRows, which keeps a row whose id
// stays). Each list's content hash is checked after: mismatched names the
// lists whose rows do not hash to the answer's (or that a delta did not
// apply to, its base not the hash the page holds). Their hashes are
// forgotten, so the next reload asks for them in full; Reload does so at
// once. A list the answer does not name is emptied, as SetState does.
func (v *PageView) ApplyState(a StateAnswer) (mismatched []string, err error) {
	if !a.Delta {
		return nil, v.SetState(a.Rows)
	}
	v.cache = a.Cache
	rows := map[string][]Row{}
	hashes := map[string][]string{}
	for i, row := range a.Rows {
		rows[row.Noun] = append(rows[row.Noun], row)
		hashes[row.Noun] = append(hashes[row.Noun], a.Hashes[i])
	}
	var errs []error
	for _, pl := range v.lists {
		if pl.verbsListed {
			continue
		}
		noun := pl.spec.Noun
		l, ok := a.List(noun)
		if !ok {
			l = StateList{List: noun, Answer: AnswerFull, Hash: ListHash(nil)}
		}
		good := false
		switch l.Answer {
		case AnswerUnchanged:
			good = pl.hash != "" && pl.hash == l.Hash
		case AnswerFull:
			if err := v.setRows(pl, rows[noun], orEmpty(hashes[noun])); err != nil {
				errs = append(errs, err)
			}
			good = pl.hash == l.Hash
		case AnswerDelta:
			if pl.hash != "" && pl.hash == l.Base {
				var err error
				if good, err = v.patch(pl, l, rows[noun], hashes[noun]); err != nil {
					errs = append(errs, err)
				}
			}
		}
		if !good {
			pl.hash = ""
			if !slices.Contains(mismatched, noun) {
				mismatched = append(mismatched, noun)
			}
		}
	}
	return mismatched, errors.Join(errs...)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// patch applies a list's delta and reports whether its rows then hash to
// the answer's. A patch that does not apply leaves the rows.
func (v *PageView) patch(pl *pageList, l StateList, rows []Row, hashes []string) (bool, error) {
	p := ochazuke.ListPatch{Upsert: make([]ochazuke.ListItem, 0, len(rows)), At: l.At}
	if len(l.At) != len(rows) {
		return false, nil
	}
	for i, row := range rows {
		if row.Label == "" {
			rows[i].Label = row.ID
		}
		p.Upsert = append(p.Upsert, v.item(pl, rows[i]))
	}
	for _, id := range l.Removed {
		p.Remove = append(p.Remove, pl.spec.ID+"/"+id)
	}
	if err := pl.list.Patch(p); err != nil {
		return false, nil // an answer that does not apply is a mismatch
	}
	for _, id := range p.Remove {
		delete(v.rows, id)
		delete(v.hashes, id)
	}
	for i, row := range rows {
		id := pl.spec.ID + "/" + row.ID
		v.rows[id], v.hashes[id] = row, hashes[i]
	}
	items := pl.list.Items()
	pl.rowIDs = pl.rowIDs[:0]
	records := make([]string, 0, len(items))
	filed := make([]Row, 0, len(items))
	for _, item := range items {
		pl.rowIDs = append(pl.rowIDs, item.ID)
		records = append(records, v.hashes[item.ID])
		filed = append(filed, v.rows[item.ID])
	}
	pl.hash = ListHash(records)
	pl.loaded, pl.err = true, ""
	if v.cfg.OnRows != nil {
		v.cfg.OnRows(pl.spec.ID, filed)
	}
	return pl.hash == l.Hash, nil
}

// Refresh is an explicit refresh: it forgets the hashes the page holds, so
// the reload asks for every list in full, and reloads.
func (v *PageView) Refresh() error {
	for _, pl := range v.lists {
		pl.hash = ""
	}
	return v.Reload()
}

// reloadState is a delta reload: one state call through FetchFlags with
// the page's hashes (SinceFlags), its answer applied (ApplyState). Lists
// whose hashes mismatch are fetched again, in full, once.
func (v *PageView) reloadState(noun, verb string, retry bool) error {
	for _, pl := range v.lists {
		pl.loading = !pl.verbsListed
	}
	flags, fetch := v.SinceFlags(), v.cfg.FetchFlags
	run := func(ctx context.Context) ([]byte, error) { return fetch(ctx, noun, verb, flags) }
	return v.launchWith(v.spec.ID, "state", noun, verb, run, func(out []byte, err error) {
		for _, pl := range v.lists {
			pl.loading = false
		}
		var a StateAnswer
		if err == nil {
			a, err = DecodeState(out)
		}
		var bad []string
		if err == nil {
			bad, err = v.ApplyState(a)
		}
		if err == nil && len(bad) > 0 && retry {
			err = v.reloadState(noun, verb, false)
		}
		if err != nil {
			for _, pl := range v.lists {
				if !pl.verbsListed {
					pl.err = fmt.Sprint(err)
				}
			}
		}
	})
}
