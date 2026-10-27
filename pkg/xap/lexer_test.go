package xap

import (
	"reflect"
	"testing"
)

func TestLexTokens(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []token
	}{
		{name: "empty", input: "", want: []token{{typ: tokEOF}}},
		{name: "brackets", input: "{}[]()", want: []token{
			{typ: tokLBrace, val: "{", pos: 0},
			{typ: tokRBrace, val: "}", pos: 1},
			{typ: tokLBracket, val: "[", pos: 2},
			{typ: tokRBracket, val: "]", pos: 3},
			{typ: tokLParen, val: "(", pos: 4},
			{typ: tokRParen, val: ")", pos: 5},
			{typ: tokEOF, pos: 6},
		}},
		{name: "string", input: `x "a b"`, want: []token{
			{typ: tokIdent, val: "x", pos: 0},
			{typ: tokString, val: "a b", pos: 2},
			{typ: tokEOF, pos: 7},
		}},
		{name: "empty string", input: `""`, want: []token{
			{typ: tokString, val: "", pos: 0},
			{typ: tokEOF, pos: 2},
		}},
		{name: "unterminated string", input: `"abc`, want: []token{
			{typ: tokString, val: "abc", pos: 0},
			{typ: tokEOF, pos: 4},
		}},
		{name: "hash inside string", input: `"a#b" c`, want: []token{
			{typ: tokString, val: "a#b", pos: 0},
			{typ: tokIdent, val: "c", pos: 6},
			{typ: tokEOF, pos: 7},
		}},
		{name: "numbers", input: "-1.5e+3 +2 7. 1e5 3E-2", want: []token{
			{typ: tokNumber, val: "-1.5e+3", pos: 0},
			{typ: tokNumber, val: "+2", pos: 8},
			{typ: tokNumber, val: "7.", pos: 11},
			{typ: tokNumber, val: "1e5", pos: 14},
			{typ: tokNumber, val: "3E-2", pos: 18},
			{typ: tokEOF, pos: 22},
		}},
		{name: "identifier characters", input: "foo_1-a.b/c~d", want: []token{
			{typ: tokIdent, val: "foo_1-a.b/c~d", pos: 0},
			{typ: tokEOF, pos: 13},
		}},
		{name: "digit then letter", input: "3D", want: []token{
			{typ: tokNumber, val: "3", pos: 0},
			{typ: tokIdent, val: "D", pos: 1},
			{typ: tokEOF, pos: 2},
		}},
		{name: "lone minus", input: "- x", want: []token{
			{typ: tokNumber, val: "-", pos: 0},
			{typ: tokIdent, val: "x", pos: 2},
			{typ: tokEOF, pos: 3},
		}},
		{name: "vrml comment", input: "a # c {\nb", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 8},
			{typ: tokEOF, pos: 9},
		}},
		{name: "line comment", input: "a // c }\nb", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 9},
			{typ: tokEOF, pos: 10},
		}},
		{name: "block comment", input: "a /* x\ny */ b", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 12},
			{typ: tokEOF, pos: 13},
		}},
		{name: "lone slash", input: "a / b", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 4},
			{typ: tokEOF, pos: 5},
		}},
		{name: "punctuation", input: "a , = ; b", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 8},
			{typ: tokEOF, pos: 9},
		}},
		{name: "whitespace", input: "\t a\n\r b ", want: []token{
			{typ: tokIdent, val: "a", pos: 2},
			{typ: tokIdent, val: "b", pos: 6},
			{typ: tokEOF, pos: 8},
		}},
		{name: "multibyte skipped", input: "a \xc3\xa9 b", want: []token{
			{typ: tokIdent, val: "a", pos: 0},
			{typ: tokIdent, val: "b", pos: 5},
			{typ: tokEOF, pos: 6},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lex(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("lex(%q)\n got %+v\nwant %+v", c.input, got, c.want)
			}
		})
	}
}

func TestLexCharClasses(t *testing.T) {
	cases := []struct {
		ch                                     byte
		digit, numStart, identStart, identChar bool
	}{
		{'0', true, true, false, true},
		{'9', true, true, false, true},
		{'-', false, true, false, true},
		{'+', false, true, false, false},
		{'a', false, false, true, true},
		{'Z', false, false, true, true},
		{'_', false, false, true, true},
		{'.', false, false, false, true},
		{'/', false, false, false, true},
		{'~', false, false, false, true},
		{' ', false, false, false, false},
		{'{', false, false, false, false},
		{'"', false, false, false, false},
		{'#', false, false, false, false},
		{0xe9, false, false, false, false},
	}
	for _, c := range cases {
		if got := isDigit(c.ch); got != c.digit {
			t.Errorf("isDigit(%q) = %v want %v", c.ch, got, c.digit)
		}
		if got := isNumberStart(c.ch); got != c.numStart {
			t.Errorf("isNumberStart(%q) = %v want %v", c.ch, got, c.numStart)
		}
		if got := isIdentStart(c.ch); got != c.identStart {
			t.Errorf("isIdentStart(%q) = %v want %v", c.ch, got, c.identStart)
		}
		if got := isIdentChar(c.ch); got != c.identChar {
			t.Errorf("isIdentChar(%q) = %v want %v", c.ch, got, c.identChar)
		}
	}
}
