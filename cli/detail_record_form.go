package cli

import (
	"fmt"

	"ofam.dev/ochazuke"
)

// DetailRecordForm is the catalog detail kind that asks for the show form
// (noun scaffold b): the selected row's thing as an edit form over every
// prop, its relation sections and its history under it, the same
// ochazuke.ShowForm a generated index page draws. A tool or a mounted
// extension asks for it by data alone:
//
//	"detail": {"kind": "record-form"}
//
// The row's noun and id name the thing (a mounted noun's through its
// proxy store); a noun the form cannot hold draws nothing.
const DetailRecordForm = "record-form"

type recordFormView struct {
	r         *ochazuke.Runtime
	sf        *ochazuke.ShowForm
	noun, key string
	data      string
	host      DetailHost
}

func newRecordFormView(h DetailHost) (DetailView, error) {
	if h.Model == nil || h.Model.Runtime == nil {
		return nil, fmt.Errorf("record-form: no runtime")
	}
	v := &recordFormView{r: h.Model.Runtime, host: h}
	sf, err := v.r.AddShowForm(h.Parent+"/show", ochazuke.ShowFormSpec{
		Target: func() (string, string) { return v.noun, v.key },
		Data:   func() string { return v.data },
	}, ochazuke.Parent(h.Page))
	if err != nil {
		return nil, err
	}
	v.sf = sf
	return v, nil
}

func (v *recordFormView) Show(row Row, ok bool) error {
	v.noun, v.key, v.data = "", "", ""
	if ok {
		v.noun, v.key, v.data = v.r.CanonicalNoun(row.Noun), row.ID, fmt.Sprint(row.Data)
	}
	v.sf.Sync()
	return nil
}

func (v *recordFormView) Block(width, height int) ochazuke.Block {
	return v.sf.Block("", width, height)
}

func (v *recordFormView) Focused() bool { return v.sf.Holds(v.r.Focus) }

func init() {
	if err := RegisterDetailKind(DetailRecordForm, newRecordFormView); err != nil {
		panic(err)
	}
}
