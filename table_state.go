package ochazuke

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// TablesFile is the remembered table views in the state directory: an
// object of table id (a list's, a grid's) to TableState.
const TablesFile = "tables.json"

// TableState is what a table remembers across a restart: its current
// column (a cell-mode table's), the widths its columns were given with
// z > and z < (column index to cells; a column left out is automatic) and
// the pinned columns the user chose (any table's; nil keeps the default
// pins, an empty list pins none).
type TableState struct {
	Column int         `json:"column,omitempty"`
	Widths map[int]int `json:"widths,omitempty"`
	Pins   *[]int      `json:"pins,omitempty"`
}

func (s TableState) empty() bool { return s.Column == 0 && len(s.Widths) == 0 && s.Pins == nil }

// samePins reports whether two remembered pin choices are the same.
func samePins(a, b *[]int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return slices.Equal(*a, *b)
}

// TablesPath is the tables file, "" when table views are not persisted
// (no state directory: Config.Disable, a test binary).
func (c *Config) TablesPath() string {
	dir := c.StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, TablesFile)
}

// tablesState is the table views by id: read from the tables file on
// first use, kept in memory when there is no state directory.
func (c *Config) tablesState() map[string]TableState {
	if c.tables != nil {
		return c.tables
	}
	c.tables = map[string]TableState{}
	path := c.TablesPath()
	if path == "" {
		return c.tables
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c.tables
	}
	if err == nil {
		err = json.Unmarshal(data, &c.tables)
	}
	if err != nil {
		c.tables = map[string]TableState{}
		c.report(TablesFile + ": " + err.Error() + " (table views reset)")
	}
	return c.tables
}

// TableState is the remembered view of the table with this id (a list's
// or a grid's instance id): the zero TableState when none was kept.
func (r *Runtime) TableState(id string) TableState {
	c := r.Config()
	if c == nil {
		return TableState{}
	}
	s := c.tablesState()[id]
	s.Widths = maps.Clone(s.Widths)
	if s.Pins != nil {
		pins := slices.Clone(*s.Pins)
		s.Pins = &pins
	}
	return s
}

// SetTableState remembers the view of the table with this id and writes
// the tables file (the state directory's TablesFile) when it changed; a
// failed write is a notice and the view still holds for the session.
func (r *Runtime) SetTableState(id string, s TableState) {
	c := r.Config()
	if c == nil || id == "" {
		return
	}
	tables := c.tablesState()
	old := tables[id]
	if old.Column == s.Column && maps.Equal(old.Widths, s.Widths) && samePins(old.Pins, s.Pins) {
		return
	}
	if s.empty() {
		delete(tables, id)
	} else {
		s.Widths = maps.Clone(s.Widths)
		if s.Pins != nil {
			pins := slices.Clone(*s.Pins)
			s.Pins = &pins
		}
		tables[id] = s
	}
	path := c.TablesPath()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(tables, "", "  ")
	if err == nil {
		err = writeAtomic(path, ".tables-*.json", append(data, '\n'))
	}
	if err != nil {
		c.report(TablesFile + ": " + err.Error() + " (not saved)")
	}
}
