// Copyright (c) 2025 Gaurav Gosain. MIT License (see LICENSE).
// Adapted from tuios/internal/diffview. No Crush code is copied.

package diffview

// Class is what a run of code is, as far as its colour goes. It is a small
// set on purpose: a theme has sixteen colours to say it with, and a class
// that has no colour of its own is drawn as Plain.
type Class uint8

const (
	// Plain is text with no class: names, and anything the lexer does not
	// know.
	Plain Class = iota
	// Keyword is a keyword of the language.
	Keyword
	// Type is a type name or a keyword that names one.
	Type
	// Func is the name of a function being declared or called.
	Func
	// Builtin is a name the language provides: len, print, self.
	Builtin
	// String is a string or character literal.
	String
	// Number is a number, and a constant such as true or nil.
	Number
	// Comment is a comment.
	Comment
	// Operator is an operator.
	Operator
	// Punct is punctuation: brackets, commas, semicolons.
	Punct
	// Preproc is a preprocessor line, a decorator or an annotation.
	Preproc
	// Tag is a markup tag name.
	Tag
	// Attr is a markup attribute name, or a key in a data file.
	Attr
	// Heading is a heading in prose markup.
	Heading
	// Meta is text tuios adds to a line, such as the mark for a missing
	// newline at the end of a file.
	Meta
	// NumClasses is how many classes there are.
	NumClasses
)

// ClassNames names each Class, in the order of their numbers, for a client
// that is sent the numbers.
var ClassNames = [NumClasses]string{
	"plain", "keyword", "type", "func", "builtin", "string", "number",
	"comment", "operator", "punct", "preproc", "tag", "attr", "heading", "meta",
}

// Span is a run of one class in a line, in bytes of the line's text.
type Span struct {
	Start, End int
	Class      Class
}

// Kind is what a line of a diff is, which picks its ground.
type Kind uint8

const (
	// Context is a line both sides have.
	Context Kind = iota
	// Add is a line only the new side has.
	Add
	// Delete is a line only the old side has.
	Delete
	// Missing is the empty half of a side by side row whose line is on the
	// other side only.
	Missing
	numKinds
)
