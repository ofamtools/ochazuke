package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

type pageRecord struct {
	instance string
	row      *Row
}

type pageReveal struct {
	list string
	row  Row
}

func (v *PageView) baseRow(instance string) (Row, bool) {
	if row, ok := v.rows[instance]; ok {
		return row, true
	}
	for _, pl := range v.lists {
		if !pl.index {
			continue
		}
		if item, ok := pl.list.Selected(); ok && item.ID == instance {
			return Row{Noun: pl.spec.Noun, ID: strings.TrimPrefix(instance, pl.spec.ID+"/"), Label: item.Label, Data: map[string]any(item.Props), Cells: item.Cells}, true
		}
	}
	return Row{}, false
}

// OpenRow opens this page on a particular thing, not its remembered selection.
// FetchRecord resolves a thing outside the current list without loading every
// record. An explicitly opened row stays visible across scoped list reloads.
func (v *PageView) OpenRow(noun, key string) error {
	pl := v.rowList(noun)
	if pl == nil {
		return fmt.Errorf("page %s has no list of %s", v.Page(), noun)
	}
	if key == "" {
		return v.m.Runtime.OpenPage(v.Page())
	}
	if err := v.m.Runtime.OpenPage(v.Page()); err != nil {
		return err
	}
	if row, ok := v.Row(pl.spec.ID + "/" + key); ok {
		if err := v.revealRow(pl, row); err != nil {
			return err
		}
	} else if v.cfg.FetchRecord == nil {
		return fmt.Errorf("no %s %q on page %s", noun, key, v.Page())
	}
	if v.cfg.FetchRecord == nil {
		return nil
	}
	return v.launchWith(v.Page(), "open-row", noun, "get", func(ctx context.Context) ([]byte, error) {
		return v.cfg.FetchRecord(ctx, pl.spec.Noun, key)
	}, func(raw []byte, err error) {
		if err == nil {
			var row Row
			row, err = v.decodeRecord(pl, key, raw)
			if err == nil {
				err = v.revealRow(pl, row)
			}
		}
		if err != nil {
			v.SetStatus(err.Error())
			v.m.Runtime.UI().Error(err.Error())
		}
	})
}

func (v *PageView) rowList(noun string) *pageList {
	for _, pl := range v.lists {
		if !pl.verbsListed && (pl.spec.Noun == noun || pl.rowNoun == v.cfg.Noun(noun)) {
			return pl
		}
	}
	return nil
}

// RevealedRow is the instance of the explicitly opened thing. A host's scope
// filter may keep this row visible without changing the user's scope setting.
func (v *PageView) RevealedRow() string {
	if v.revealed == nil {
		return ""
	}
	return v.revealed.list + "/" + v.revealed.row.ID
}

func (v *PageView) revealRow(pl *pageList, row Row) error {
	v.revealed = &pageReveal{list: pl.spec.ID, row: row}
	rows := make([]Row, 0, len(pl.rowIDs)+1)
	for _, id := range pl.rowIDs {
		if old, ok := v.baseRow(id); ok && old.ID != row.ID {
			rows = append(rows, old)
		}
	}
	if pl.index {
		for _, item := range pl.list.Items() {
			key := strings.TrimPrefix(item.ID, pl.spec.ID+"/")
			if key != row.ID {
				rows = append(rows, Row{Noun: pl.spec.Noun, ID: key, Label: item.Label, Data: map[string]any(item.Props), Cells: item.Cells})
			}
		}
	}
	rows = append(rows, row)
	if err := v.setRows(pl, rows, nil); err != nil {
		return err
	}
	id := pl.spec.ID + "/" + row.ID
	if err := pl.list.Select(id); err != nil {
		return err
	}
	v.current = slices.Index(v.lists, pl)
	if v.groups != nil {
		if err := v.groups.Select(v.groupID(pl)); err != nil {
			return err
		}
	}
	if v.m.Runtime.Page == v.Page() && v.m.Runtime.TopModal() == "" {
		return v.m.Runtime.SetFocus(id)
	}
	return nil
}

func (v *PageView) revealedRows(pl *pageList, rows []Row, hashes []string) ([]Row, []string) {
	if v.revealed == nil || v.revealed.list != pl.spec.ID {
		return rows, hashes
	}
	for _, row := range rows {
		if row.ID == v.revealed.row.ID {
			v.revealed.row = row
			return rows, hashes
		}
	}
	return append(slices.Clone(rows), v.revealed.row), nil
}

func (v *PageView) decodeRecord(pl *pageList, key string, raw []byte) (Row, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Row{}, err
	}
	if data == nil {
		return Row{}, fmt.Errorf("no %s %q", pl.spec.Noun, key)
	}
	var err error
	key, err = v.m.Runtime.CanonicalThingKey(ochazuke.Ctx{Runtime: v.m.Runtime}, pl.rowNoun, key, ochazuke.Thing(data))
	if err != nil {
		return Row{}, err
	}
	label := key
	for _, p := range v.m.Runtime.Props(pl.rowNoun) {
		if p.Role == ochazuke.RoleLabel {
			if value := data[p.Name]; value != nil && fmt.Sprint(value) != "" {
				label = fmt.Sprint(value)
			}
			break
		}
	}
	return Row{Noun: pl.spec.Noun, ID: key, Label: label, Data: data}, nil
}

func (v *PageView) loadDetailRecord() {
	if v.detail == nil || v.cfg.FetchRecord == nil {
		return
	}
	id := v.selectedID()
	if v.record != nil && v.record.instance == id {
		return
	}
	row, ok := v.baseRow(id)
	if !ok || !v.missingDetailField(row.Data) {
		return
	}
	pl := v.rowList(row.Noun)
	if pl == nil {
		return
	}
	state := &pageRecord{instance: id}
	v.record = state
	if err := v.launchWith(v.Page(), "detail-record", pl.spec.Noun, "get", func(ctx context.Context) ([]byte, error) {
		return v.cfg.FetchRecord(ctx, pl.spec.Noun, row.ID)
	}, func(raw []byte, err error) {
		if v.record != state {
			return
		}
		if err == nil {
			var full Row
			full, err = v.decodeRecord(pl, row.ID, raw)
			if err == nil {
				state.row = &full
			}
		}
		if err != nil {
			v.SetStatus(err.Error())
		}
	}); err != nil {
		v.SetStatus(err.Error())
	}
}

func (v *PageView) missingDetailField(data any) bool {
	d := v.spec.Detail
	missing := func(field string) bool { return field != "" && detailValue(data, field) == nil }
	if missing(d.PathField) {
		return true
	}
	for _, field := range d.Fields {
		if missing(field) {
			return true
		}
	}
	for _, section := range d.Sections {
		if missing(section.Field) {
			return true
		}
		for _, field := range section.Fields {
			if missing(field.Field) {
				return true
			}
		}
	}
	return false
}
