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
	for _, id := range r.Ancestry(focus) {
		if c := r.Chooser(id); c != nil {
			if c.propFiltersEnabled() {
				c.sync()
				return c.filters
			}
			return nil
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
