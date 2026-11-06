package diffview

import "ofam.dev/ochazuke"

// RenderText draws an ochazuke.DiffText (a copy refresh's plain diff) as
// a unified diff with no syntax colour: one hunk per section, headed by
// its header. It is the runtime's DiffRenderer once Register has run.
func RenderText(s ochazuke.Styles, d ochazuke.DiffText, width, height int) string {
	file := File{Status: "M", OldPath: d.Old, NewPath: d.New}
	for _, sec := range d.Sections {
		h := Hunk{OldStart: 1, NewStart: 1, Header: sec.Header}
		for _, l := range sec.Lines {
			line := Line{Text: l.Text}
			switch {
			case l.Side < 0:
				h.OldCount++
				line.Kind, line.Old = Delete, h.OldCount
			case l.Side > 0:
				h.NewCount++
				line.Kind, line.New = Add, h.NewCount
			default:
				h.OldCount++
				h.NewCount++
				line.Kind, line.Old, line.New = Context, h.OldCount, h.NewCount
			}
			h.Lines = append(h.Lines, line)
		}
		file.Hunks = append(file.Hunks, h)
	}
	v := &View{Layout: Unified, SplitWidth: DefaultSplitWidth}
	v.SetFile(file)
	return v.View(s, width, height)
}
