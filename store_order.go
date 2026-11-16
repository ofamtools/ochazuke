package ochazuke

import "slices"

// A store's own order as data (Store.Order): the order its lists answer
// when a request sorts on nothing, and the tie-break after a request's
// sort keys (a noun whose things always listed by created, then id). The
// framework adds the keys to every Query request after the request's own,
// so a store that sorts in its query (a DocStore's table: DocQuery.Sort)
// keeps the order paged, and sorts a List answer by them; a store with no
// Order is as it is.

// withOrder adds the store's Order to every List and Query request of noun.
func withOrder(noun string, s Store) Store {
	if len(s.Order) == 0 {
		return s
	}
	raw := s
	if raw.Query != nil {
		s.Query = func(c Ctx, q ListQuery) (ListPage, error) {
			q.Sort = orderedSort(q.Sort, raw.Order)
			return raw.Query(c, q)
		}
	}
	if raw.List != nil {
		s.List = func(c Ctx, q ListQuery) ([]Thing, error) {
			things, err := raw.List(c, q)
			if err != nil || c.Runtime == nil || c.Runtime.Nouns[noun] == nil {
				return things, err
			}
			return c.Runtime.queryAll(c.Runtime.Nouns[noun], things, ListQuery{Sort: raw.Order, Archived: true}).Items, nil
		}
	}
	return s
}

// orderedSort is a request's sort keys followed by each order key on a
// field the request does not sort on.
func orderedSort(keys, order []SortKey) []SortKey {
	out := slices.Clone(keys)
	for _, k := range order {
		if !slices.ContainsFunc(keys, func(o SortKey) bool { return o.Field == k.Field }) {
			out = append(out, k)
		}
	}
	return out
}
