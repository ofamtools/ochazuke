package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

const (
	paletteResultsID     = "palette-results"
	paletteResultsListID = "__palette-results/list"
)

// showResults presents JSON object lists without borrowing the source rows' actions.
func (p *Palette) showResults(operation string, value any) error {
	var data []byte
	var err error
	switch value := value.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		if v := reflect.ValueOf(value); v.IsValid() && v.Kind() == reflect.Slice && v.IsNil() {
			data = []byte("[]")
		} else {
			data, err = json.Marshal(value)
		}
	}
	data = bytes.TrimSpace(data)
	if err != nil || len(data) == 0 || data[0] != '[' {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil || decoder.InputOffset() != int64(len(data)) {
		return nil
	}
	rows, ok := decoded.([]any)
	if !ok {
		return nil
	}
	counts := make(map[string]int)
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			return nil
		}
		for key := range object {
			counts[key]++
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	columns := make([]Column, len(keys))
	for i, key := range keys {
		columns[i] = Column{Title: modalLabel(key), Priority: i}
	}
	if len(keys) == 0 {
		columns = []Column{{Title: "value"}}
	}
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		object := row.(map[string]any)
		cells := make([]string, len(columns))
		if len(keys) == 0 {
			cells[0] = "{}"
		}
		for j, key := range keys {
			value, present := object[key]
			if !present {
				continue
			}
			if text, ok := value.(string); ok {
				cells[j] = text
			} else {
				text, err := json.Marshal(value)
				if err != nil {
					return err
				}
				cells[j] = string(text)
			}
		}
		items[i] = ListItem{ID: fmt.Sprintf("__palette-results/%d", i), Noun: "palette-result-row", Data: object, Cells: cells}
	}
	if p.r.TopModal() != "" {
		return errModalAbove
	}
	inst, err := p.r.OpenModal(paletteResultsID, Payload{}, ReplaceOpen())
	if err != nil {
		return err
	}
	inst.Data = operation
	p.r.Define("palette-result-row")
	list, err := p.r.AddList(paletteResultsListID, items, Parent(paletteResultsID), Columns(columns...))
	if err == nil {
		err = list.Focus()
	}
	if err != nil {
		_ = p.r.Pop()
	}
	return err
}

func paletteResultsView(r *Runtime, inst *Instance, width, height int) Block {
	list := r.List(paletteResultsListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	operation, _ := inst.Data.(string)
	if list.Len() == 0 {
		return Text(operation + "\nNo rows")
	}
	if height == 1 {
		return Text(operation)
	}
	return Text(operation + "\n" + list.View(width, height-1))
}
