package toml

import (
	"bytes"
	"fmt"
	"time"
	"unicode/utf8"
)

// parser builds the syntax tree of a document the decoder has already accepted, so
// it may be lenient where the decoder is strict: what it must get exactly right is
// where each token starts and ends.
type parser struct {
	src []byte
	pos int
}

func parseTree(src []byte) (*node, error) {
	state := &parser{src: src}
	doc := &node{kind: kindDocument, children: []*node{}}

	for !state.eof() {
		if err := state.line(doc); err != nil {
			return nil, err
		}
	}

	return doc, nil
}

// line takes what one line holds: nothing, a comment, a table header or a key and its
// value, then whatever ends the line.
func (p *parser) line(doc *node) error {
	p.whitespace(doc)

	if p.eof() {
		return nil
	}

	var (
		expression *node
		err        error
	)

	switch p.peek() {
	case '\n', '\r':
		return p.newline(doc)
	case '#':
		doc.add(p.comment())

		return nil
	case '[':
		expression, err = p.header()
	default:
		expression, err = p.keyValue()
	}

	if err != nil {
		return err
	}

	doc.add(expression)

	return p.endOfLine(doc)
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *parser) peek() byte {
	return p.src[p.pos]
}

func (p *parser) at(s string) bool {
	return bytes.HasPrefix(p.src[p.pos:], []byte(s))
}

func (p *parser) token(tokenKind kind, length int) *node {
	t := &node{kind: tokenKind, text: p.src[p.pos : p.pos+length]}
	p.pos += length

	return t
}

func (p *parser) errorf(format string, args ...any) error {
	line, column := 1, 1

	for _, r := range string(p.src[:p.pos]) {
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return &Error{
		Err:    ErrInternal,
		Line:   line,
		Column: column,
		Msg:    "the syntax tree failed on a document the decoder accepted: " + fmt.Sprintf(format, args...),
	}
}

// whitespace adds the run of spaces and tabs at the current position, if any.
func (p *parser) whitespace(parent *node) {
	end := p.pos
	for end < len(p.src) && (p.src[end] == ' ' || p.src[end] == '\t') {
		end++
	}

	if end > p.pos {
		parent.add(p.token(kindWhitespace, end-p.pos))
	}
}

func (p *parser) newline(parent *node) error {
	switch {
	case p.at("\n"):
		parent.add(p.token(kindNewline, len("\n")))
	case p.at("\r\n"):
		parent.add(p.token(kindNewline, len("\r\n")))
	default:
		return p.errorf("carriage return without a line feed")
	}

	return nil
}

// comment takes everything up to the end of the line, the line ending excluded.
func (p *parser) comment() *node {
	end := p.pos
	for end < len(p.src) && p.src[end] != '\n' && !bytes.HasPrefix(p.src[end:], []byte("\r\n")) {
		end++
	}

	return p.token(kindComment, end-p.pos)
}

// endOfLine takes what may follow an expression on its line: whitespace, a comment,
// and the line ending, or the end of the document.
func (p *parser) endOfLine(parent *node) error {
	p.whitespace(parent)

	if !p.eof() && p.peek() == '#' {
		parent.add(p.comment())
	}

	if p.eof() {
		return nil
	}

	return p.newline(parent)
}

// gap takes whitespace, comments and line endings, as arrays and inline tables allow
// between their elements.
func (p *parser) gap(parent *node) error {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t':
			p.whitespace(parent)
		case '#':
			parent.add(p.comment())
		case '\n', '\r':
			if err := p.newline(parent); err != nil {
				return err
			}
		default:
			return nil
		}
	}

	return nil
}

func (p *parser) header() (*node, error) {
	headerKind, openKind, closeKind, closing := kindTable, kindOpenBracket, kindCloseBracket, "]"
	if p.at("[[") {
		headerKind, openKind, closeKind, closing = kindArrayTable, kindOpenDoubleBracket, kindCloseDoubleBracket, "]]"
	}

	header := &node{kind: headerKind, children: []*node{p.token(openKind, len(closing))}}
	p.whitespace(header)

	key, err := p.key()
	if err != nil {
		return nil, err
	}

	header.add(key)
	p.whitespace(header)

	if !p.at(closing) {
		return nil, p.errorf("expected %q", closing)
	}

	header.add(p.token(closeKind, len(closing)))

	return header, nil
}

// key takes a simple or dotted key. Whitespace after its last part is left for the
// caller, which owns what separates the key from what follows.
func (p *parser) key() (*node, error) {
	key := &node{kind: kindKey, children: []*node{}}

	for {
		part, err := p.simpleKey()
		if err != nil {
			return nil, err
		}

		key.add(part)

		mark := p.pos
		gap := &node{children: []*node{}}
		p.whitespace(gap)

		if p.eof() || p.peek() != '.' {
			p.pos = mark

			return key, nil
		}

		key.children = append(key.children, gap.children...)
		key.add(p.token(kindDot, 1))
		p.whitespace(key)
	}
}

func (p *parser) simpleKey() (*node, error) {
	if p.eof() {
		return nil, p.errorf("expected a key")
	}

	switch p.peek() {
	case '"':
		return p.basicString()
	case '\'':
		return p.literalString()
	}

	end := p.pos
	for end < len(p.src) && isBareKeyByte(p.src[end]) {
		end++
	}

	if end == p.pos {
		return nil, p.errorf("expected a key")
	}

	return p.token(kindBareKey, end-p.pos), nil
}

func isBareKeyByte(char byte) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || isDigit(char) ||
		char == '_' || char == '-'
}

func (p *parser) keyValue() (*node, error) {
	keyValue := &node{kind: kindKeyValue, children: []*node{}}

	key, err := p.key()
	if err != nil {
		return nil, err
	}

	keyValue.add(key)
	p.whitespace(keyValue)

	if p.eof() || p.peek() != '=' {
		return nil, p.errorf("expected \"=\"")
	}

	keyValue.add(p.token(kindEquals, 1))
	p.whitespace(keyValue)

	value, err := p.value()
	if err != nil {
		return nil, err
	}

	keyValue.add(value)

	return keyValue, nil
}

func (p *parser) value() (*node, error) {
	if p.eof() {
		return nil, p.errorf("expected a value")
	}

	switch {
	case p.at(`"""`):
		return p.multilineString(kindMultilineBasicString, `"""`, true)
	case p.at(`'''`):
		return p.multilineString(kindMultilineLiteralString, `'''`, false)
	case p.peek() == '"':
		return p.basicString()
	case p.peek() == '\'':
		return p.literalString()
	case p.peek() == '[':
		return p.collection(kindArray, kindOpenBracket, kindCloseBracket, ']', "array", p.value)
	case p.peek() == '{':
		return p.collection(kindInlineTable, kindOpenBrace, kindCloseBrace, '}', "inline table", p.keyValue)
	}

	return p.scalar()
}

func (p *parser) basicString() (*node, error) {
	for end := p.pos + 1; end < len(p.src); end++ {
		switch p.src[end] {
		case '\\':
			end++
		case '"':
			return p.token(kindBasicString, end+1-p.pos), nil
		case '\n', '\r':
			return nil, p.errorf("line ending in a string")
		}
	}

	return nil, p.errorf("unterminated string")
}

func (p *parser) literalString() (*node, error) {
	for end := p.pos + 1; end < len(p.src); end++ {
		switch p.src[end] {
		case '\'':
			return p.token(kindLiteralString, end+1-p.pos), nil
		case '\n', '\r':
			return nil, p.errorf("line ending in a string")
		}
	}

	return nil, p.errorf("unterminated string")
}

// multilineString ends at the first run of three or more delimiters: up to two
// delimiters right before the closing three belong to the content.
func (p *parser) multilineString(stringKind kind, delimiter string, escapes bool) (*node, error) {
	quote := delimiter[0]

	for end := p.pos + len(delimiter); end < len(p.src); end++ {
		switch char := p.src[end]; {
		case escapes && char == '\\':
			end++
		case char == quote:
			run := end
			for run < len(p.src) && p.src[run] == quote {
				run++
			}

			if run-end >= len(delimiter) {
				return p.token(stringKind, run-p.pos), nil
			}

			end = run - 1
		}
	}

	return nil, p.errorf("unterminated multi-line string")
}

// scalar takes an integer, a float, a boolean or a date-time. A date followed by a
// space and a time is one value.
func (p *parser) scalar() (*node, error) {
	end := p.pos
	for end < len(p.src) {
		switch char := p.src[end]; {
		case isScalarByte(char):
			end++
		case char == ' ' && isDate(p.src[p.pos:end]) && isTimeStart(p.src[end+1:]):
			end++
		default:
			return p.scalarToken(end)
		}
	}

	return p.scalarToken(end)
}

func (p *parser) scalarToken(end int) (*node, error) {
	if end == p.pos {
		r, _ := utf8.DecodeRune(p.src[p.pos:])

		return nil, p.errorf("expected a value, found %q", r)
	}

	return p.token(kindScalar, end-p.pos), nil
}

func isScalarByte(char byte) bool {
	return isBareKeyByte(char) || char == '+' || char == '.' || char == ':'
}

func isDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

// isDate reports whether b is YYYY-MM-DD.
func isDate(b []byte) bool {
	if len(b) != len(time.DateOnly) || b[4] != '-' || b[7] != '-' {
		return false
	}

	for i, char := range b {
		if i != 4 && i != 7 && !isDigit(char) {
			return false
		}
	}

	return true
}

// isTimeStart reports whether b starts with HH:.
func isTimeStart(b []byte) bool {
	return len(b) >= len("00:") && isDigit(b[0]) && isDigit(b[1]) && b[2] == ':'
}

// collection takes an array or an inline table: elements separated by commas, a
// trailing comma allowed, whitespace, comments and line endings between them.
func (p *parser) collection(
	collectionKind, openKind, closeKind kind,
	closing byte,
	name string,
	element func() (*node, error),
) (*node, error) {
	collection := &node{kind: collectionKind, children: []*node{p.token(openKind, 1)}}

	for {
		if err := p.gap(collection); err != nil {
			return nil, err
		}

		if p.eof() {
			return nil, p.errorf("unterminated %s", name)
		}

		if p.peek() == closing {
			collection.add(p.token(closeKind, 1))

			return collection, nil
		}

		item, err := element()
		if err != nil {
			return nil, err
		}

		collection.add(item)

		if err := p.gap(collection); err != nil {
			return nil, err
		}

		if p.eof() {
			return nil, p.errorf("unterminated %s", name)
		}

		switch p.peek() {
		case ',':
			collection.add(p.token(kindComma, 1))
		case closing:
			collection.add(p.token(closeKind, 1))

			return collection, nil
		default:
			return nil, p.errorf("expected \",\" or %q", closing)
		}
	}
}
