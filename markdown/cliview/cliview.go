// Package cliview is the "markdown" detail kind of a mounted data page
// (cli.CatalogDetail.Kind): an editable markdown.Doc over the file the
// selected row's PathField names. It lives apart from package cli so that
// Glamour reaches only the hosts that register it:
//
//	cliview.Register()                      // every page this process mounts
//	cli.PageConfig{DetailKinds: map[string]cli.DetailFactory{cliview.Kind: cliview.Factory()}}
//
// The Doc is the inline editor of package markdown, committed on change
// (typing after a pause, every other edit at once) through its Source,
// markdown.File(path) by default. A new selected row opens its file (the
// open element of the last one written first); the same row with a
// changed record (a reload after the file changed) reads it again unless
// an element is open. A row without the field, or a file that cannot be
// read, shows why. The Doc is a focus stop after the page's lists.
package cliview

import (
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/markdown"
)

// Kind is the detail kind this package draws.
const Kind = "markdown"

// Option changes the views a Factory builds.
type Option func(*config)

type config struct {
	source func(path string) markdown.Source
	opts   []markdown.Option
	author string
}

// Source opens each path through src instead of markdown.File (a host
// keeping documents elsewhere, a test).
func Source(src func(path string) markdown.Source) Option {
	return func(c *config) { c.source = src }
}

// Render passes options to every Doc (markdown.PreserveNewlines).
func Render(opts ...markdown.Option) Option {
	return func(c *config) { c.opts = append(c.opts, opts...) }
}

// Author signs the notes added in the view (markdown.Doc.Author); ""
// keeps the markdown.note-author setting.
func Author(name string) Option { return func(c *config) { c.author = name } }

// Factory is the markdown detail kind's factory.
func Factory(opts ...Option) cli.DetailFactory {
	c := config{source: markdown.File}
	for _, o := range opts {
		o(&c)
	}
	return func(h cli.DetailHost) (cli.DetailView, error) {
		if h.Spec.PathField == "" {
			return nil, errors.New("a markdown detail needs pathField")
		}
		return &view{h: h, c: c}, nil
	}
}

// Register registers the markdown kind for every page this process
// mounts (cli.RegisterDetailKind).
func Register(opts ...Option) error { return cli.RegisterDetailKind(Kind, Factory(opts...)) }

// view is one mounted page's markdown detail.
type view struct {
	h    cli.DetailHost
	c    config
	doc  *markdown.Doc
	path string
	note string // why no document is shown
}

// docID is the Doc's instance id under the row stand-in.
func (v *view) docID() string { return v.h.Parent + "/markdown" }

// Doc is the open document, or nil.
func (v *view) Doc() *markdown.Doc { return v.doc }

// Of is the Doc a page view's markdown detail shows, or nil.
func Of(p *cli.PageView) *markdown.Doc { return DocOf(p.DetailView()) }

// DocOf is the Doc a markdown detail view shows, or nil (another kind, or
// no document open).
func DocOf(v cli.DetailView) *markdown.Doc {
	if mv, ok := v.(*view); ok {
		return mv.doc
	}
	return nil
}

func (v *view) Show(row cli.Row, ok bool) error {
	path := ""
	if ok {
		path = v.h.Field(row, v.h.Spec.PathField)
	}
	if v.doc != nil && path == v.path {
		if v.doc.Editing() {
			return nil
		}
		return v.doc.Reload()
	}
	if err := v.close(); err != nil {
		return err
	}
	v.path = path
	switch {
	case !ok:
		v.note = "no row selected"
		return nil
	case path == "":
		v.note = fmt.Sprintf("%s has no %s", row.Label, v.h.Spec.PathField)
		return nil
	}
	r := v.h.Model.Runtime
	doc, err := markdown.New(r, v.docID(), v.h.Parent, v.c.source(path), v.c.opts...)
	if err != nil {
		v.note = err.Error()
		return nil
	}
	if v.c.author != "" {
		doc.Author = v.c.author
	}
	v.doc, v.note = doc, ""
	return nil
}

// close writes the open element of the shown document and removes it.
func (v *view) close() error {
	if v.doc == nil {
		return nil
	}
	err := v.doc.Leave()
	if rerr := v.h.Model.Runtime.RemoveInstance(v.doc.ID()); err == nil {
		err = rerr
	}
	v.doc = nil
	return err
}

func (v *view) Block(width, height int) ochazuke.Block {
	if v.doc == nil {
		if width <= 0 || height <= 0 {
			return ochazuke.Block{}
		}
		return ochazuke.Text(ansi.Wrap(v.note, width, " /"))
	}
	if width <= 0 || height <= 0 {
		v.doc.Block(1, 1, *v.h.Model.Appearance) // keeps its place in the drawing order
		return ochazuke.Block{}
	}
	return v.doc.Block(width, height, *v.h.Model.Appearance)
}

func (v *view) Focused() bool {
	r := v.h.Model.Runtime
	return v.doc != nil && slices.Contains(r.Ancestry(r.Focus), v.doc.ID())
}
