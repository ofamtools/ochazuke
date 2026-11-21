package expr

// Expr is a compiled, immutable boolean predicate expression.
// The zero value Expr{} evaluates to true and has Source() == "".
type Expr struct {
	src  string
	root node
}

// Source returns the original expression string passed to Parse.
func (e Expr) Source() string {
	return e.src
}

type node interface {
	eval(env map[string]any) any
}

type literalNode struct {
	val any
}

type listNode struct {
	elems []node
}

type identNode struct {
	path []string
}

type unaryNode struct {
	op      tokKind
	operand node
}

type binaryNode struct {
	op    tokKind
	left  node
	right node
}
