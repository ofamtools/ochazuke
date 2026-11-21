package expr

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type tokKind uint8

const (
	tokEOF tokKind = iota
	tokIdent
	tokString
	tokInt
	tokFloat
	tokTrue
	tokFalse
	tokNull
	tokIn
	tokNot
	tokEq
	tokNeq
	tokLt
	tokLte
	tokGt
	tokGte
	tokAnd
	tokOr
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokComma
	tokDot
)

func (k tokKind) String() string {
	switch k {
	case tokEOF:
		return "EOF"
	case tokIdent:
		return "identifier"
	case tokString:
		return "string"
	case tokInt:
		return "int"
	case tokFloat:
		return "float"
	case tokTrue:
		return "true"
	case tokFalse:
		return "false"
	case tokNull:
		return "null"
	case tokIn:
		return "in"
	case tokNot:
		return "!"
	case tokEq:
		return "=="
	case tokNeq:
		return "!="
	case tokLt:
		return "<"
	case tokLte:
		return "<="
	case tokGt:
		return ">"
	case tokGte:
		return ">="
	case tokAnd:
		return "&&"
	case tokOr:
		return "||"
	case tokLParen:
		return "("
	case tokRParen:
		return ")"
	case tokLBracket:
		return "["
	case tokRBracket:
		return "]"
	case tokComma:
		return ","
	case tokDot:
		return "."
	default:
		return fmt.Sprintf("token(%d)", k)
	}
}

type token struct {
	kind     tokKind
	pos      int
	lit      string
	strVal   string
	intVal   int64
	floatVal float64
}

func tokenize(src string) ([]token, error) {
	var tokens []token
	pos := 0
	for pos < len(src) {
		r, size := utf8.DecodeRuneInString(src[pos:])
		if r == utf8.RuneError && size == 1 {
			return nil, fmt.Errorf("expr: at offset %d: invalid UTF-8 sequence", pos)
		}
		if unicode.IsSpace(r) {
			pos += size
			continue
		}

		b := src[pos]
		switch b {
		case '(':
			tokens = append(tokens, token{kind: tokLParen, pos: pos, lit: "("})
			pos++
		case ')':
			tokens = append(tokens, token{kind: tokRParen, pos: pos, lit: ")"})
			pos++
		case '[':
			tokens = append(tokens, token{kind: tokLBracket, pos: pos, lit: "["})
			pos++
		case ']':
			tokens = append(tokens, token{kind: tokRBracket, pos: pos, lit: "]"})
			pos++
		case ',':
			tokens = append(tokens, token{kind: tokComma, pos: pos, lit: ","})
			pos++
		case '.':
			tokens = append(tokens, token{kind: tokDot, pos: pos, lit: "."})
			pos++
		case '!':
			if pos+1 < len(src) && src[pos+1] == '=' {
				tokens = append(tokens, token{kind: tokNeq, pos: pos, lit: "!="})
				pos += 2
			} else {
				tokens = append(tokens, token{kind: tokNot, pos: pos, lit: "!"})
				pos++
			}
		case '=':
			if pos+1 < len(src) && src[pos+1] == '=' {
				tokens = append(tokens, token{kind: tokEq, pos: pos, lit: "=="})
				pos += 2
			} else {
				return nil, fmt.Errorf("expr: at offset %d: unexpected '=' (did you mean '=='?)", pos)
			}
		case '<':
			if pos+1 < len(src) && src[pos+1] == '=' {
				tokens = append(tokens, token{kind: tokLte, pos: pos, lit: "<="})
				pos += 2
			} else {
				tokens = append(tokens, token{kind: tokLt, pos: pos, lit: "<"})
				pos++
			}
		case '>':
			if pos+1 < len(src) && src[pos+1] == '=' {
				tokens = append(tokens, token{kind: tokGte, pos: pos, lit: ">="})
				pos += 2
			} else {
				tokens = append(tokens, token{kind: tokGt, pos: pos, lit: ">"})
				pos++
			}
		case '&':
			if pos+1 < len(src) && src[pos+1] == '&' {
				tokens = append(tokens, token{kind: tokAnd, pos: pos, lit: "&&"})
				pos += 2
			} else {
				return nil, fmt.Errorf("expr: at offset %d: unexpected '&' (did you mean '&&'?)", pos)
			}
		case '|':
			if pos+1 < len(src) && src[pos+1] == '|' {
				tokens = append(tokens, token{kind: tokOr, pos: pos, lit: "||"})
				pos += 2
			} else {
				return nil, fmt.Errorf("expr: at offset %d: unexpected '|' (did you mean '||'?)", pos)
			}
		case '"':
			tok, nextPos, err := scanString(src, pos)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, tok)
			pos = nextPos
		case '\'':
			return nil, fmt.Errorf("expr: at offset %d: unexpected '\\'' (strings must use double quotes)", pos)
		default:
			if isDigit(b) || (b == '-' && pos+1 < len(src) && isDigit(src[pos+1])) {
				tok, nextPos, err := scanNumber(src, pos)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, tok)
				pos = nextPos
			} else if isIdentStart(b) {
				tok, nextPos := scanIdent(src, pos)
				tokens = append(tokens, tok)
				pos = nextPos
			} else {
				return nil, fmt.Errorf("expr: at offset %d: unexpected character %q", pos, r)
			}
		}
	}

	tokens = append(tokens, token{kind: tokEOF, pos: pos})
	return tokens, nil
}

func scanString(src string, start int) (token, int, error) {
	pos := start + 1
	var buf strings.Builder

	for pos < len(src) {
		b := src[pos]
		if b == '"' {
			return token{
				kind:   tokString,
				pos:    start,
				lit:    src[start : pos+1],
				strVal: buf.String(),
			}, pos + 1, nil
		}
		if b == '\\' {
			escPos := pos
			pos++
			if pos >= len(src) {
				return token{}, 0, fmt.Errorf("expr: at offset %d: unterminated escape sequence in string literal", escPos)
			}
			switch src[pos] {
			case '"':
				buf.WriteByte('"')
				pos++
			case '\\':
				buf.WriteByte('\\')
				pos++
			case '/':
				buf.WriteByte('/')
				pos++
			case 'n':
				buf.WriteByte('\n')
				pos++
			case 't':
				buf.WriteByte('\t')
				pos++
			case 'r':
				buf.WriteByte('\r')
				pos++
			case 'b':
				buf.WriteByte('\b')
				pos++
			case 'f':
				buf.WriteByte('\f')
				pos++
			case 'u':
				pos++
				u1, nextPos, err := parseHex4(src, pos, escPos)
				if err != nil {
					return token{}, 0, err
				}
				pos = nextPos
				if 0xD800 <= u1 && u1 <= 0xDBFF {
					if pos+1 >= len(src) || src[pos] != '\\' || src[pos+1] != 'u' {
						return token{}, 0, fmt.Errorf("expr: at offset %d: invalid UTF-16 surrogate pair in string literal", escPos)
					}
					pairPos := pos
					u2, nextPos2, err := parseHex4(src, pos+2, pairPos)
					if err != nil {
						return token{}, 0, err
					}
					if u2 < 0xDC00 || u2 > 0xDFFF {
						return token{}, 0, fmt.Errorf("expr: at offset %d: invalid UTF-16 surrogate pair in string literal", escPos)
					}
					pos = nextPos2
					buf.WriteRune(utf16.DecodeRune(rune(u1), rune(u2)))
				} else if 0xDC00 <= u1 && u1 <= 0xDFFF {
					return token{}, 0, fmt.Errorf("expr: at offset %d: unexpected low UTF-16 surrogate in string literal", escPos)
				} else {
					buf.WriteRune(rune(u1))
				}
			default:
				r, _ := utf8.DecodeRuneInString(src[pos:])
				return token{}, 0, fmt.Errorf("expr: at offset %d: invalid escape sequence '\\%c'", escPos, r)
			}
			continue
		}
		if b < 0x20 {
			if b == '\n' || b == '\r' {
				return token{}, 0, fmt.Errorf("expr: at offset %d: unterminated string literal", start)
			}
			return token{}, 0, fmt.Errorf("expr: at offset %d: unescaped control character in string literal", pos)
		}
		if b < utf8.RuneSelf {
			buf.WriteByte(b)
			pos++
			continue
		}
		r, size := utf8.DecodeRuneInString(src[pos:])
		if r == utf8.RuneError && size == 1 {
			return token{}, 0, fmt.Errorf("expr: at offset %d: invalid UTF-8 sequence in string literal", pos)
		}
		buf.WriteRune(r)
		pos += size
	}

	return token{}, 0, fmt.Errorf("expr: at offset %d: unterminated string literal", start)
}

func parseHex4(src string, pos, escPos int) (uint16, int, error) {
	if pos+4 > len(src) {
		return 0, 0, fmt.Errorf("expr: at offset %d: invalid \\u escape sequence", escPos)
	}
	hexPart := src[pos : pos+4]
	for i := 0; i < 4; i++ {
		c := hexPart[i]
		isHex := ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
		if !isHex {
			return 0, 0, fmt.Errorf("expr: at offset %d: invalid \\u escape sequence", escPos)
		}
	}
	v, err := strconv.ParseUint(hexPart, 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("expr: at offset %d: invalid \\u escape sequence", escPos)
	}
	return uint16(v), pos + 4, nil
}

func scanNumber(src string, start int) (token, int, error) {
	pos := start
	if src[pos] == '-' {
		pos++
	}
	for pos < len(src) && isDigit(src[pos]) {
		pos++
	}

	isFloat := false
	if pos < len(src) && src[pos] == '.' {
		isFloat = true
		pos++
		if pos >= len(src) || !isDigit(src[pos]) {
			return token{}, 0, fmt.Errorf("expr: at offset %d: invalid number literal: expected digit after '.'", pos)
		}
		for pos < len(src) && isDigit(src[pos]) {
			pos++
		}
	}

	if pos < len(src) && (src[pos] == 'e' || src[pos] == 'E') {
		isFloat = true
		pos++
		if pos < len(src) && (src[pos] == '+' || src[pos] == '-') {
			pos++
		}
		if pos >= len(src) || !isDigit(src[pos]) {
			return token{}, 0, fmt.Errorf("expr: at offset %d: invalid number literal: malformed exponent", pos)
		}
		for pos < len(src) && isDigit(src[pos]) {
			pos++
		}
	}

	if pos < len(src) && (src[pos] == '.' || isIdentContinue(src[pos])) {
		return token{}, 0, fmt.Errorf("expr: at offset %d: invalid number literal near %q", start, src[start:pos+1])
	}

	raw := src[start:pos]
	if isFloat {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsInf(f, 0) {
			return token{}, 0, fmt.Errorf("expr: at offset %d: float literal %q out of range", start, raw)
		}
		return token{kind: tokFloat, pos: start, lit: raw, floatVal: f}, pos, nil
	}

	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return token{}, 0, fmt.Errorf("expr: at offset %d: integer literal %q out of range", start, raw)
	}
	return token{kind: tokInt, pos: start, lit: raw, intVal: v}, pos, nil
}

func scanIdent(src string, start int) (token, int) {
	pos := start + 1
	for pos < len(src) && isIdentContinue(src[pos]) {
		pos++
	}
	word := src[start:pos]
	var kind tokKind
	switch word {
	case "true":
		kind = tokTrue
	case "false":
		kind = tokFalse
	case "null":
		kind = tokNull
	case "in":
		kind = tokIn
	default:
		kind = tokIdent
	}
	return token{kind: kind, pos: start, lit: word}, pos
}

func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

func isIdentStart(b byte) bool {
	return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || b == '_'
}

func isIdentContinue(b byte) bool {
	return isIdentStart(b) || isDigit(b)
}
