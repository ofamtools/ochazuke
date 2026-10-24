package main

import (
	"errors"

	"ochazuke"
)

// The prompt: one single-line input of the field noun (insert pinned, like
// the framework's prompt noun) in one modal, reused by every verb that needs
// a line of text: cell edit, rename, add column, filter, open and save-as.
// enter commits, esc cancels, ctrl+o opens the path picker into the field.
// The question (label and commit) is application state; the field keeps its
// own undo tree across uses.

const promptFrame = "csv-prompt"
const fieldID = "field"

type question struct {
	label  string
	commit func(text string) error
}

// registerPrompt creates the field and the verbs that close it.
func (a *App) registerPrompt() {
	r := a.r
	r.Define("field").Pin("insert").
		Verb("commit", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			q := a.ask
			if q == nil || r.Instances[r.FocusID()] == nil || r.FocusID() != fieldID {
				return nil, errors.New("no prompt is open")
			}
			text := string(a.field.Text)
			a.ask = nil
			if err := r.Pop(); err != nil {
				return nil, err
			}
			return text, q.commit(text)
		}, "Close the prompt and run the question with the field's text").
		Verb("cancel", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if a.ask == nil {
				return nil, errors.New("no prompt is open")
			}
			a.ask = nil
			return nil, r.Pop()
		}, "Close the prompt without running the question").
		Mode("insert").
		Bind("enter", ochazuke.Do("commit.field")).
		Bind("esc", ochazuke.Do("cancel.field")).
		Bind("ctrl+o", ochazuke.Do("open.picker"))
	a.field = ochazuke.NewInput(fieldID, "", false)
	if err := r.AddInput(a.field, ochazuke.Kind("field")); err != nil {
		panic(err)
	}
	a.m.ModalViews[promptFrame] = ochazuke.ModalView{Title: "Prompt", Render: func(r *ochazuke.Runtime, f *ochazuke.Frame, w, h int) ochazuke.Block {
		label := ""
		if a.ask != nil {
			label = a.ask.label
		}
		body := ochazuke.InputBlock(a.field, max(1, w), 1)
		return body.Offset(0, 2).WithText(label + "\n\n" + body.Text)
	}}
}

// prompt opens the modal with a label, initial text and kind ("path" lets
// ctrl+o browse); commit runs with the text when enter is pressed.
func (a *App) prompt(label, text, kind string, commit func(string) error) error {
	if a.ask != nil {
		return errors.New("a prompt is already open")
	}
	a.field.Kind = kind
	if err := a.field.Replace(0, len(a.field.Text), text); err != nil {
		return err
	}
	a.field.SetMode("insert")
	a.field.Cursor = len(a.field.Text)
	view := a.m.ModalViews[promptFrame]
	view.Title = label
	a.m.ModalViews[promptFrame] = view
	a.ask = &question{label: label, commit: commit}
	if err := a.r.Push(&ochazuke.Frame{ID: promptFrame, Focus: fieldID}); err != nil {
		a.ask = nil
		return err
	}
	return nil
}
