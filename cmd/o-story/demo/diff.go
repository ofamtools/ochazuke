package demo

import (
	"fmt"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/diffview"
)

// DiffSpec supplies either unified patch text or structured files. File
// navigation replaces the view's snapshot, exercising the host callback.
type DiffSpec struct {
	Text       string          `json:"text,omitempty"`
	Files      []diffview.File `json:"files,omitempty"`
	Layout     diffview.Layout `json:"layout,omitempty"`
	Highlight  *bool           `json:"highlight,omitempty"`
	SplitWidth int             `json:"splitWidth,omitempty"`
}

func (ss *session) addDiffPage(p *Page) error {
	files := p.Diff.Files
	if p.Diff.Text != "" {
		var err error
		files, err = diffview.Parse(p.Diff.Text)
		if err != nil {
			return fmt.Errorf("diff: %w", err)
		}
	}
	if len(files) == 0 {
		files = []diffview.File{{}}
	}
	ss.R.Instance(p.ID, ochazuke.Kind(Demo))
	opts := []diffview.Option{diffview.WithLayout(p.Diff.Layout), diffview.WithSplitWidth(p.Diff.SplitWidth)}
	if p.Diff.Highlight != nil {
		opts = append(opts, diffview.WithHighlight(*p.Diff.Highlight))
	}
	v, err := diffview.New(ss.R, p.ID+"/diff", p.ID, files[0], opts...)
	if err != nil {
		return err
	}
	at := 0
	v.OnFileChange = func(delta int) { at = min(max(0, at+delta), len(files)-1); v.SetFile(files[at]) }
	ss.pages[p.ID] = p
	title := p.Title
	if title == "" {
		title = "Diff"
	}
	_, err = ss.R.AddPage(p.ID, title, func(_ *ochazuke.Runtime, w, h int) ochazuke.Block { return ochazuke.Text(v.View(ss.M.Styles(), w, h)) }, ochazuke.Focus(v.ID()))
	return err
}
