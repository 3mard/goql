// Package parse is the lexer/parser based on net/template parser
package parse

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type stateFn func(*lexer) stateFn

type itemType int

const (
	eof = -1
)

const (
	itemError itemType = iota
	itemEOF

	itemSelect
	itemFrom
	itemWhere
	itemOrder
	itemBy
	itemOr
	itemAnd
	itemLimit
	itemIn
	itemAsc
	itemDesc
	itemLike
	itemAlpha
	itemNumber

	itemEqual
	itemGreater
	itemLesser
	itemGreaterEqual
	itemLesserEqual
	itemNotEqual

	itemParenOpen
	itemParenClose

	itemComma
	itemWildCard
	itemLiteral1
	itemLiteral2
	itemSemicolon
)

var (
	keywords = map[string]itemType{
		"select": itemSelect,
		"from":   itemFrom,
		"where":  itemWhere,
		"order":  itemOrder,
		"by":     itemBy,
		"or":     itemOr,
		"and":    itemAnd,
		"limit":  itemLimit,
		"in":     itemIn,
		"asc":    itemAsc,
		"desc":   itemDesc,
		"like":   itemLike,
	}
)

type item struct {
	Typ   itemType
	pos   int
	value string
}

type lexer struct {
	input string // input string
	start int    // start position for the current lexeme
	pos   int    // current position
	width int    // last rune width
	state stateFn
	items chan item

	parenDepth int
}

func (l *lexer) next() rune {
	if l.pos >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = w
	l.pos += l.width
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// backup steps back one rune. Can only be called once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.items <- item{t, l.start, l.input[l.start:l.pos]}
	// Some items contain text internally. If so, count their newlines.
	l.start = l.pos
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid string) bool {
	if strings.ContainsRune(valid, l.next()) {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes a run of runes from the valid set.
func (l *lexer) acceptRun(valid string) {
	for strings.ContainsRune(valid, l.next()) {
	}
	l.backup()
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating l.nextItem.
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.items <- item{itemError, l.start, fmt.Sprintf(format, args...)}
	return nil
}

// nextItem returns the next item from the input.
// Called by the parser, not in the lexing goroutine.
func (l *lexer) nextItem() item {
	return <-l.items
}

// drain drains the output so the lexing goroutine will exit.
// Called by the parser, not in the lexing goroutine.
func (l *lexer) drain() {
	for range l.items {
	}
}

// lex creates a new scanner for the input string.
func lex(input string) *lexer {
	l := &lexer{
		input: input,
		items: make(chan item, 2),
	}
	go l.run()
	return l
}

// run runs the state machine for the lexer.
func (l *lexer) run() {
	for state := lexStart; state != nil; {
		state = state(l)
	}
	close(l.items)
}

func lexStart(l *lexer) stateFn {
	l.acceptRun(" \t\n")
	l.ignore()
	switch r := l.peek(); {
	case r == eof:
		if l.parenDepth > 0 {
			l.errorf("paren not closed")
		}
		return nil
	case isSQLOperator(r):
		return lexOp
	case isAlpha(r):
		return lexAlpha
	case isNumeric(r):
		return lexNumber
	case r == '(':
		return lexParenOpen
	case r == ')':
		return lexParenClose
	case r == '"':
		return lexLiteral2
	case r == '\'':
		return lexLiteral1
	case r == ';':
		return lexSemicolon
	case r == ',':
		return lexComma
	case r == '*':
		return lexSemicolon
	default:
		return l.errorf("invalid character %c", r)
	}
}

func lexOp(l *lexer) stateFn {
	r := l.next()
	rn := l.peek()
	var t itemType
	switch r {
	case '>':
		t = itemGreater
		if rn == '=' {
			l.next()
			t = itemGreaterEqual
		} else if rn == '>' {
			l.next()
			t = itemNotEqual
		}
	case '<':
		t = itemLesser
		if rn == '=' {
			l.next()
			t = itemLesserEqual
		}
	case '=':
		t = itemEqual
	}
	l.emit(t)
	return lexStart
}

func lexAlpha(l *lexer) stateFn {
	for isAlphaNumeric(l.next()) {
	}
	l.backup()
	t := strings.ToLower(l.input[l.start:l.pos])
	item := itemAlpha
	if n, ok := keywords[t]; ok {
		item = n
	}
	l.emit(item)
	return lexStart
}

func lexParenOpen(l *lexer) stateFn {
	l.next()
	l.parenDepth++
	l.emit(itemParenOpen)
	return lexStart
}

func lexParenClose(l *lexer) stateFn {
	l.next()
	l.parenDepth--
	if l.parenDepth < 0 {
		l.errorf("invalid ) ")
		return nil
	}
	l.emit(itemParenOpen)
	return lexStart
}

func lexSemicolon(l *lexer) stateFn {
	l.next()
	l.emit(itemSemicolon)
	return lexStart
}

func lexComma(l *lexer) stateFn {
	l.next()
	l.emit(itemComma)
	return lexStart
}

func lexLiteral1(l *lexer) stateFn {
	l.next()
	for r := l.next(); r != '\''; r = l.next() {
		if r == eof {
			return l.errorf("string is not terminated")
		}
	}
	l.emit(itemLiteral1)
	return lexStart
}

func lexLiteral2(l *lexer) stateFn {
	l.next()
	for r := l.next(); r != '"'; r = l.next() {
		if r == eof {
			return l.errorf("string is not terminated")
		}
	}
	l.emit(itemLiteral2)
	return lexStart
}

func lexWildCard(l *lexer) stateFn {
	l.next()
	l.emit(itemWildCard)
	return lexStart
}

func lexNumber(l *lexer) stateFn {
	for isNumeric(l.next()) {
	}
	l.backup()
	l.emit(itemNumber)
	return lexStart
}

// isSpace reports whether r is a space character.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

// isEndOfLine reports whether r is an end-of-line character.
func isEndOfLine(r rune) bool {
	return r == '\r' || r == '\n'
}

func isAlpha(r rune) bool {
	return unicode.IsLetter(r)
}

// isAlphaNumeric reports whether r is an alphabetic, digit, or underscore.
func isAlphaNumeric(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isNumeric(r rune) bool {
	return unicode.IsDigit(r)
}

func isSQLOperator(r rune) bool {
	return r == '>' || r == '<' || r == '='
}

func Test(t string) {
	l := lex(t)
	for i := range l.items {
		fmt.Printf("\n%d (%d) => %s", i.Typ, i.pos, i.value)
	}
}
