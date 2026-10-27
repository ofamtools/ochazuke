package storymatrix

// Param is one parameter of a covering array: a name and its values.
type Param struct {
	Name   string
	Values []string
}

// Combos is a covering array over params: every combination of the first
// full params (a full product), extended with the rest by IPOG at strength
// two, so every pair of values of any two params appears in some row,
// except a pair no valid row can hold. A row is valid when no exclude
// matches it (an exclude matches when the row has every one of its
// axis=value pairs). The algorithm takes params, values and rows in the
// order given and breaks ties by the lowest index, so the result is the
// same every run. Each row holds one value index per param.
func Combos(params []Param, full int, excludes []Exclude) [][]int {
	n := len(params)
	if n == 0 {
		return [][]int{{}}
	}
	for _, p := range params {
		if len(p.Values) == 0 {
			return nil
		}
	}
	if full < 2 {
		full = min(2, n)
	}
	full = min(full, n)
	valid := func(row []int) bool {
		for _, e := range excludes {
			hit := true
			for axis, v := range e.Match {
				i := index(params, axis)
				if i < 0 || i >= len(row) || row[i] < 0 || params[i].Values[row[i]] != v {
					hit = false
					break
				}
			}
			if hit {
				return false
			}
		}
		return true
	}
	// The full product of the first params, invalid rows dropped.
	rows := [][]int{{}}
	for k := 0; k < full; k++ {
		var next [][]int
		for _, r := range rows {
			for v := range params[k].Values {
				row := append(append([]int(nil), r...), v)
				if valid(row) {
					next = append(next, row)
				}
			}
		}
		rows = next
	}
	for k := full; k < n; k++ {
		// The pairs (i, vi) x (k, vk) to cover; a pair no row could hold
		// alone is not required.
		type pair struct{ i, vi, vk int }
		need := map[pair]bool{}
		var order []pair
		for i := 0; i < k; i++ {
			for vi := range params[i].Values {
				for vk := range params[k].Values {
					probe := make([]int, k+1)
					for j := range probe {
						probe[j] = -1
					}
					probe[i], probe[k] = vi, vk
					if valid(probe) {
						p := pair{i, vi, vk}
						need[p] = true
						order = append(order, p)
					}
				}
			}
		}
		// Horizontal growth: each row takes the value covering most
		// uncovered pairs.
		for ri, r := range rows {
			best, bestGain := -1, -1
			for vk := range params[k].Values {
				row := append(append([]int(nil), r...), vk)
				if !valid(row) {
					continue
				}
				gain := 0
				for i := 0; i < k; i++ {
					if r[i] >= 0 && need[pair{i, r[i], vk}] {
						gain++
					}
				}
				if gain > bestGain {
					best, bestGain = vk, gain
				}
			}
			rows[ri] = append(r, best)
			if best >= 0 {
				for i := 0; i < k; i++ {
					if r[i] >= 0 {
						delete(need, pair{i, r[i], best})
					}
				}
			}
		}
		// Vertical growth: a pair still uncovered fills a free cell of a
		// row that agrees with it, else starts a row of its own.
		for _, p := range order {
			if !need[p] {
				continue
			}
			placed := false
			for _, r := range rows {
				if r[p.i] == p.vi && r[k] == p.vk {
					placed = true // an earlier placement covered it
					break
				}
			}
			for ri, r := range rows {
				if placed {
					break
				}
				if r[k] == p.vk && r[p.i] == -1 {
					r[p.i] = p.vi
					if valid(r) {
						placed = true
					} else {
						r[p.i] = -1
					}
				} else if r[k] == -1 && r[p.i] == p.vi {
					r[k] = p.vk
					if valid(r) {
						placed = true
					} else {
						r[k] = -1
					}
				}
				if placed {
					rows[ri] = r
					break
				}
			}
			if !placed {
				row := make([]int, k+1)
				for j := range row {
					row[j] = -1
				}
				row[p.i], row[k] = p.vi, p.vk
				rows = append(rows, row)
			}
			delete(need, p)
		}
	}
	// Free cells take the first value that keeps the row valid; a row
	// with no valid completion is dropped.
	var out [][]int
	seen := map[string]bool{}
	for _, r := range rows {
		ok := fill(r, 0, params, valid)
		if !ok {
			continue
		}
		key := rowKey(r)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// fill gives each free cell from j on the first value keeping the row
// valid, backtracking when a later cell has none.
func fill(r []int, j int, params []Param, valid func([]int) bool) bool {
	if j == len(r) {
		return valid(r)
	}
	if r[j] >= 0 {
		return fill(r, j+1, params, valid)
	}
	for v := range params[j].Values {
		r[j] = v
		if valid(r) && fill(r, j+1, params, valid) {
			return true
		}
	}
	r[j] = -1
	return false
}

func rowKey(r []int) string {
	b := make([]byte, 0, len(r)*2)
	for _, v := range r {
		b = append(b, byte(v), ',')
	}
	return string(b)
}

func index(params []Param, name string) int {
	for i, p := range params {
		if p.Name == name {
			return i
		}
	}
	return -1
}
