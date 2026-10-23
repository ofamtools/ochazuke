package ochazuke

import "strings"

// blockChange is an insert-session selection descriptor, not application state.
// Moving outside the first line or changing its surrounding text cancels repeat,
// just as leaving a Vim block insertion's contiguous edit does.
type blockChange struct {
	Top, Bottom, Column int
	Prefix, Suffix      string
}

func (in *Input) finishBlockChange() {
	block := in.blockChange
	in.blockChange = nil
	if block == nil {
		return
	}
	lines := strings.Split(string(in.Text), "\n")
	if block.Bottom >= len(lines) {
		return
	}
	row, _ := in.rowColumn(in.Cursor)
	first := lines[block.Top]
	if row != block.Top || len(first) < len(block.Prefix)+len(block.Suffix) || !strings.HasPrefix(first, block.Prefix) || !strings.HasSuffix(first, block.Suffix) {
		return
	}
	inserted := first[len(block.Prefix) : len(first)-len(block.Suffix)]
	if inserted == "" {
		return
	}
	for row := block.Top + 1; row <= block.Bottom; row++ {
		prefix, suffix := splitCell(lines[row], block.Column)
		lines[row] = prefix + inserted + suffix
	}
	cursor := in.Cursor
	if err := in.Replace(0, len(in.Text), strings.Join(lines, "\n")); err != nil {
		panic(err)
	}
	in.Cursor = cursor
}
