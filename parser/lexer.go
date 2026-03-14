// parser/lexer.go
package parser

import (
	"bufio"
	"io"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenError TokenType = iota
	TokenEOF
	TokenText
	TokenOpenTag      // <
	TokenCloseTag     // >
	TokenSlashClose   // />
	TokenOpenBracket  // <
	TokenCloseBracket // >
	TokenIdentifier
	TokenString
	TokenEquals
)

type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

type Lexer struct {
	reader *bufio.Reader
	line   int
	col    int
	inTag  bool
}

func NewLexer(r io.Reader) *Lexer {
	return &Lexer{
		reader: bufio.NewReader(r),
		line:   1,
		col:    1,
	}
}

func (l *Lexer) skipComment() {
	var state int // 0: initial, 1: -, 2: --
	for {
		r, _, err := l.reader.ReadRune()
		if err != nil {
			return
		}

		if r == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}

		switch state {
		case 0:
			if r == '-' {
				state = 1
			}
		case 1:
			if r == '-' {
				state = 2
			} else {
				state = 0
			}
		case 2:
			if r == '>' {
				return
			} else if r != '-' {
				state = 0
			}
		}
	}
}

func (l *Lexer) NextToken() Token {
	if !l.inTag {
		return l.readText()
	}

	for {
		r, _, err := l.reader.ReadRune()
		if err != nil {
			if err == io.EOF {
				return Token{Type: TokenEOF, Line: l.line, Col: l.col}
			}
			return Token{Type: TokenError, Value: err.Error(), Line: l.line, Col: l.col}
		}

		switch r {
		case '>':
			l.col++
			l.inTag = false
			return Token{Type: TokenCloseTag, Value: ">", Line: l.line, Col: l.col - 1}

		case '/':
			next, err := l.reader.Peek(1)
			if err == nil && len(next) > 0 && next[0] == '>' {
				l.reader.ReadRune() // '>'
				l.col += 2
				l.inTag = false
				return Token{Type: TokenSlashClose, Value: "/>", Line: l.line, Col: l.col - 2}
			}
			return l.readIdentifier(r)

		case '=':
			l.col++
			return Token{Type: TokenEquals, Value: "=", Line: l.line, Col: l.col - 1}

		case '"', '\'':
			return l.readString(r)

		case '\n':
			l.line++
			l.col = 1
			continue

		default:
			if unicode.IsSpace(r) {
				l.col++
				continue
			}
			return l.readIdentifier(r)
		}
	}
}

func (l *Lexer) readText() Token {
	startLine, startCol := l.line, l.col
	var value strings.Builder

	for {
		next, err := l.reader.Peek(1)
		if err != nil {
			if value.Len() > 0 {
				return Token{Type: TokenText, Value: value.String(), Line: startLine, Col: startCol}
			}
			return Token{Type: TokenEOF, Line: l.line, Col: l.col}
		}

		if next[0] == '<' {
			// Check for comment <!--
			peeked, _ := l.reader.Peek(4)
			if len(peeked) == 4 && string(peeked) == "<!--" {
				l.reader.Discard(4)
				l.skipComment()
				continue
			}

			// If we have text, return it first
			if value.Len() > 0 {
				return Token{Type: TokenText, Value: value.String(), Line: startLine, Col: startCol}
			}

			// Otherwise, it's a tag start
			l.reader.ReadRune() // consume '<'
			l.inTag = true
			l.col++
			return Token{Type: TokenOpenTag, Value: "<", Line: l.line, Col: l.col - 1}
		}

		r, _, _ := l.reader.ReadRune()
		if r == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		value.WriteRune(r)
	}
}

func (l *Lexer) readString(delim rune) Token {
	startLine, startCol := l.line, l.col
	var value strings.Builder
	l.col++ // skip opening delimiter

	for {
		r, _, err := l.reader.ReadRune() // 3 değer döndürüyor
		if err != nil {
			return Token{Type: TokenError, Value: "unclosed string", Line: startLine, Col: startCol}
		}

		if r == delim {
			l.col++
			return Token{Type: TokenString, Value: value.String(), Line: startLine, Col: startCol}
		}

		if r == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}

		value.WriteRune(r)
	}
}

func (l *Lexer) readIdentifier(first rune) Token {
	startLine, startCol := l.line, l.col
	var value strings.Builder
	value.WriteRune(first)
	l.col++

	for {
		r, _, err := l.reader.ReadRune() // 3 değer döndürüyor
		if err != nil {
			break
		}

		if unicode.IsSpace(r) || r == '<' || r == '>' || r == '=' {
			l.reader.UnreadRune()
			break
		}

		if r == '/' && value.Len() > 0 {
			l.reader.UnreadRune()
			break
		}

		if r == '\n' {
			l.line++
			l.col = 1
			break
		}

		value.WriteRune(r)
		l.col++
	}

	valStr := value.String()
	return Token{Type: TokenIdentifier, Value: valStr, Line: startLine, Col: startCol}
}
