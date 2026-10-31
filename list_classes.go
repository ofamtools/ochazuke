package ochazuke

import "slices"

// ClassDecl names an application-defined row class. Key generates normal-mode
// ] KEY and [ KEY rows on the declaring noun. An empty Key is palette-only.
// Title describes the class in help; an empty title uses Name.
type ClassDecl struct {
	Name  string `json:"name"`
	Key   string `json:"key,omitempty"`
	Title string `json:"title,omitempty"`
}

// Classes declares row classes and their motion bindings. Repeating a name
// replaces its declaration. Declare classes before loading user bindings so
// bindings.json can override or unbind the generated rows.
func (b *NounBuilder) Classes(classes ...ClassDecl) *NounBuilder {
	for _, class := range classes {
		if class.Name == "" {
			panic("class name must be nonempty")
		}
		if i := slices.IndexFunc(b.n.Classes, func(c ClassDecl) bool { return c.Name == class.Name }); i >= 0 {
			old := b.n.Classes[i]
			if old.Key != "" {
				b.Mode("normal").Unbind("] " + old.Key).Unbind("[ " + old.Key)
			}
			b.n.Classes[i] = class
		} else {
			b.n.Classes = append(b.n.Classes, class)
		}
		if class.Key == "" {
			continue
		}
		// Keep the count through a multi-key row, as the list and markdown
		// nouns do. A noun's explicit count-mode inheritance wins.
		if b.n.Extends["count_pending"] == "" {
			b.Mode("count_pending").Extends("normal")
		}
		title := class.Title
		if title == "" {
			title = class.Name
		}
		for _, motion := range []struct{ prefix, verb string }{{"]", "next-class"}, {"[", "previous-class"}} {
			row := Do("ochazuke.list."+motion.verb, Arguments{"class": class.Name}).FromPayload("steps", SourceCount).
				Hinted("list." + motion.verb + " " + title)
			row.Help = "Jump to " + title + " (" + class.Name + ")"
			b.Mode("normal").Bind(motion.prefix+" "+class.Key, row)
		}
	}
	return b
}

// jumpClass scans the shown rows once, excluding the current row. A count
// beyond the remaining matches lands on the last match, never another stop.
func (l *List) jumpClass(class string, steps, delta int) {
	to := l.selected
	for i := l.selected + delta; i >= 0 && i < len(l.items); i += delta {
		if slices.Contains(l.items[i].Classes, class) {
			to = i
			steps--
			if steps == 0 {
				break
			}
		}
	}
	if to == l.selected {
		l.r.UI().Info("No further row of class " + class)
		return
	}
	l.goTo(to)
}
