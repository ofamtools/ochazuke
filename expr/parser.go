package expr

import "fmt"

const maxDepth = 256

// Parse compiles a predicate expression string into an immutable Expr.
// An empty or whitespace-only string compiles to a constant true expression.
func Parse(src string) (Expr, error) {
	tokens, err := tokenize(src)
	if err != nil {
		return Expr{}, err
	}
	if tokens[0].kind == tokEOF {
		return Expr{src: src, root: literalNode{val: true}}, nil
	}

	p := parser{tokens: tokens}
	root, err := p.parseOr()
	if err != nil {
		return Expr{}, err
	}
	if tok := p.peek(); tok.kind != tokEOF {
		return Expr{}, fmt.Errorf("expr: at offset %d: unexpected token %q", tok.pos, tok.lit)
	}
	return Expr{src: src, root: root}, nil
}

type parser struct {
	tokens []token
	pos    int
	depth  int
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

func (p *parser) next() token {
	tok := p.peek()
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return tok
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		op := p.next().kind
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		op := p.next().kind
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseEquality() (node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for {
		kind := p.peek().kind
		if kind != tokEq && kind != tokNeq {
			break
		}
		p.next()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: kind, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseComparison() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		kind := p.peek().kind
		switch kind {
		case tokLt, tokLte, tokGt, tokGte, tokIn:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = binaryNode{op: kind, left: left, right: right}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().kind == tokNot {
		tok := p.next()
		p.depth++
		if p.depth > maxDepth {
			return nil, fmt.Errorf("expr: at offset %d: expression nesting exceeds maximum depth (%d)", tok.pos, maxDepth)
		}
		operand, err := p.parseUnary()
		p.depth--
		if err != nil {
			return nil, err
		}
		return unaryNode{op: tokNot, operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	tok := p.peek()
	switch tok.kind {
	case tokString:
		p.next()
		return literalNode{val: tok.strVal}, nil
	case tokInt:
		p.next()
		return literalNode{val: tok.intVal}, nil
	case tokFloat:
		p.next()
		return literalNode{val: tok.floatVal}, nil
	case tokTrue:
		p.next()
		return literalNode{val: true}, nil
	case tokFalse:
		p.next()
		return literalNode{val: false}, nil
	case tokNull:
		p.next()
		return literalNode{val: nil}, nil
	case tokIdent:
		p.next()
		path := []string{tok.lit}
		for p.peek().kind == tokDot {
			dotTok := p.next()
			nextTok := p.peek()
			if nextTok.kind != tokIdent {
				return nil, fmt.Errorf("expr: at offset %d: expected identifier after '.'", dotTok.pos)
			}
			p.next()
			path = append(path, nextTok.lit)
		}
		return identNode{path: path}, nil
	case tokLParen:
		p.next()
		if p.peek().kind == tokRParen {
			return nil, fmt.Errorf("expr: at offset %d: empty parentheses", tok.pos)
		}
		p.depth++
		if p.depth > maxDepth {
			return nil, fmt.Errorf("expr: at offset %d: expression nesting exceeds maximum depth (%d)", tok.pos, maxDepth)
		}
		inner, err := p.parseOr()
		p.depth--
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			got := p.peek()
			return nil, fmt.Errorf("expr: at offset %d: expected ')' (got %s)", got.pos, got.kind)
		}
		p.next()
		return inner, nil
	case tokLBracket:
		p.next()
		if p.peek().kind == tokRBracket {
			p.next()
			return listNode{elems: nil}, nil
		}
		p.depth++
		if p.depth > maxDepth {
			return nil, fmt.Errorf("expr: at offset %d: expression nesting exceeds maximum depth (%d)", tok.pos, maxDepth)
		}
		var elems []node
		for {
			elem, err := p.parseOr()
			if err != nil {
				p.depth--
				return nil, err
			}
			elems = append(elems, elem)
			if p.peek().kind == tokComma {
				commaTok := p.next()
				if p.peek().kind == tokRBracket {
					p.depth--
					return nil, fmt.Errorf("expr: at offset %d: trailing comma in list literal", commaTok.pos)
				}
				continue
			}
			if p.peek().kind == tokRBracket {
				p.next()
				break
			}
			got := p.peek()
			p.depth--
			return nil, fmt.Errorf("expr: at offset %d: expected ',' or ']' in list literal (got %s)", got.pos, got.kind)
		}
		p.depth--
		return listNode{elems: elems}, nil
	case tokEOF:
		return nil, fmt.Errorf("expr: at offset %d: unexpected end of expression", tok.pos)
	default:
		return nil, fmt.Errorf("expr: at offset %d: unexpected token %q", tok.pos, tok.lit)
	}
}
