package ochazuke

// ChooserPropFiltersSetting enables chooser prop filters (default true).
const ChooserPropFiltersSetting = "chooser.prop-filters"

func (c *Chooser) propFiltersEnabled() bool {
	return !c.spec.NoPropFilters && c.r.Settings().Bool(ChooserPropFiltersSetting)
}

func (c *Chooser) propItem(row ChooserRow) ListItem {
	noun := row.Noun
	if noun == "" {
		noun = c.spec.Noun
	}
	return ListItem{ID: row.ID, Label: row.Label, Noun: noun, Data: row.Data, Props: row.Props, Cells: row.Cells}
}

func (c *Chooser) cachePropRows() {
	c.filters.all = make([]ListItem, 0, len(c.spec.Rows))
	for _, row := range c.spec.Rows {
		c.filters.all = append(c.filters.all, c.propItem(row))
	}
	c.filters.cacheRows()
	// A declared row noun still supplies props/defaults before a source loads
	// or after its last row is removed.
	if len(c.spec.Rows) == 0 && c.spec.Noun != "" {
		c.filters.nouns = []string{c.r.CanonicalNoun(c.spec.Noun)}
	}
}

func (c *Chooser) initPropFilters() {
	c.filters = &List{r: c.r, id: c.spec.Kind, selected: -1, filterFocus: c.input.ID}
	c.filters.filterChanged = func() {
		keep := ""
		if c.moved && c.cursor >= 0 && c.cursor < len(c.matches) {
			keep = c.matches[c.cursor].ID
		}
		c.refilter()
		for i, row := range c.matches {
			if keep != "" && row.ID == keep {
				c.cursor, c.moved = i, true
				break
			}
		}
	}
	c.cachePropRows()
}

// propFilterList resolves the nearest row view. A chooser is a boundary:
// its filters never target a list underneath it, including when opted out.
func (r *Runtime) propFilterList(focus string) *List {
	l := r.rowFilterList(focus)
	if l != nil && l.filterFocus != "" {
		if c := r.Chooser(l.id); c != nil && !c.propFiltersEnabled() {
			return nil
		}
	}
	return l
}

// rowFilterList also serves text-only choosers that opt out of prop filters.
func (r *Runtime) rowFilterList(focus string) *List {
	for _, id := range r.Ancestry(focus) {
		if c := r.Chooser(id); c != nil {
			c.sync()
			return c.filters
		}
		if l := r.List(id); l != nil {
			return l
		}
		if t := r.Tree(id); t != nil {
			return t.list
		}
	}
	return nil
}

// PropFilters returns a copy of the chooser's active prop conditions.
func (c *Chooser) PropFilters() []PropFilter { return c.filters.PropFilters() }

// SetPropFilters replaces typed conditions, keeping the text query and a
// surviving moved highlight. Invalid updates leave the chooser unchanged.
func (c *Chooser) SetPropFilters(filters []PropFilter) error {
	return c.filters.SetPropFilters(filters)
}

// SavedFilters returns the app and user definitions available to this chooser.
func (c *Chooser) SavedFilters() ([]SavedFilter, error) { return c.filters.SavedFilters() }

// ApplyFilter applies a named saved filter, keeping the chooser's text query.
func (c *Chooser) ApplyFilter(name string) error { return c.filters.ApplyFilter(name) }

// lingerRows merges departures with ranked matches without changing either
// the source records or ranking. Anonymous rows lack stable identity and
// are never retained, but stay distinct while matching.
func (c *Chooser) lingerRows() {
	if c.seen == "" && len(c.filters.propFilters) == 0 || c.r.Settings().Duration(ListFilterLingerSetting) <= 0 {
		c.filters.linger.reset()
		return
	}
	all, ids := make([]string, 0, len(c.spec.Rows)), make([]string, 0, len(c.matches))
	byID := make(map[string]ChooserRow, len(c.spec.Rows)+1)
	for _, row := range c.spec.Rows {
		all = append(all, row.ID)
		byID[row.ID] = row
	}
	for _, row := range c.matches {
		ids = append(ids, row.ID)
		byID[row.ID] = row
	}
	duration := c.r.Settings().Duration(ListFilterLingerSetting)
	matchedRows := make(map[string][]ChooserRow, len(c.matches))
	for _, row := range c.matches {
		matchedRows[row.ID] = append(matchedRows[row.ID], row)
	}
	shown := c.filters.linger.apply(all, ids, c.filters.filterNow(), duration)
	c.matches = make([]ChooserRow, 0, len(shown))
	for _, id := range shown {
		row := byID[id]
		if rows := matchedRows[id]; len(rows) > 0 {
			row, matchedRows[id] = rows[0], rows[1:]
		}
		c.matches = append(c.matches, row)
	}
}
