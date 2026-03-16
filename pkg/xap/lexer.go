package xap

import (
	"unicode"
)

type tokenType int

const (
	tokEOF     tokenType = iota
	tokLBrace            // {
	tokRBrace            // }
	tokLBracket          // [
	tokRBracket          // ]
	tokLParen            // (
	tokRParen            // )
	tokIdent             // identifier/keyword
	tokString            // "quoted string"
	tokNumber            // number literal
)

type token struct {
	typ tokenType
	val string
	pos int // byte offset in input
}

type lexer struct {
	input  string
	pos    int
	tokens []token
}

func lex(input string) []token {
	l := &lexer{input: input}
	l.run()
	return l.tokens
}

func (l *lexer) run() {
	for l.pos < len(l.input) {
		l.skipWhitespace()
		if l.pos >= len(l.input) {
			break
		}

		ch := l.input[l.pos]

		switch {
		case ch == '{':
			l.emit(tokLBrace, "{")
		case ch == '}':
			l.emit(tokRBrace, "}")
		case ch == '[':
			l.emit(tokLBracket, "[")
		case ch == ']':
			l.emit(tokRBracket, "]")
		case ch == '(':
			l.emit(tokLParen, "(")
		case ch == ')':
			l.emit(tokRParen, ")")
		case ch == '"':
			l.lexString()
			continue
		case ch == '#':
			// VRML-style line comment
			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.pos++
			}
			continue
		case ch == '/' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '/':
			// C-style line comment
			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.pos++
			}
			continue
		case ch == '/' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '*':
			// Block comment /* ... */
			l.pos += 2
			for l.pos+1 < len(l.input) {
				if l.input[l.pos] == '*' && l.input[l.pos+1] == '/' {
					l.pos += 2
					break
				}
				l.pos++
			}
			continue
		case isNumberStart(ch):
			l.lexNumber()
			continue
		case isIdentStart(ch):
			l.lexIdent()
			continue
		default:
			l.pos++
			continue
		}

		l.pos++
	}

	l.tokens = append(l.tokens, token{typ: tokEOF, pos: l.pos})
}

func (l *lexer) emit(typ tokenType, val string) {
	l.tokens = append(l.tokens, token{typ: typ, val: val, pos: l.pos})
}

func (l *lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		ch := rune(l.input[l.pos])
		if unicode.IsSpace(ch) {
			l.pos++
		} else {
			break
		}
	}
}

func (l *lexer) lexString() {
	startPos := l.pos // position of opening "
	l.pos++           // skip opening "
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '"' {
		l.pos++
	}
	l.tokens = append(l.tokens, token{typ: tokString, val: l.input[start:l.pos], pos: startPos})
	if l.pos < len(l.input) {
		l.pos++ // skip closing "
	}
}

func (l *lexer) lexNumber() {
	start := l.pos
	if l.input[l.pos] == '-' || l.input[l.pos] == '+' {
		l.pos++
	}
	for l.pos < len(l.input) && (isDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		l.pos++
	}
	// Handle scientific notation
	if l.pos < len(l.input) && (l.input[l.pos] == 'e' || l.input[l.pos] == 'E') {
		l.pos++
		if l.pos < len(l.input) && (l.input[l.pos] == '+' || l.input[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
			l.pos++
		}
	}
	l.tokens = append(l.tokens, token{typ: tokNumber, val: l.input[start:l.pos], pos: start})
}

func (l *lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		l.pos++
	}
	l.tokens = append(l.tokens, token{typ: tokIdent, val: l.input[start:l.pos], pos: start})
}

func isNumberStart(ch byte) bool {
	return isDigit(ch) || ch == '-' || ch == '+'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch) || ch == '-' || ch == '.' || ch == '/' || ch == '~'
}
