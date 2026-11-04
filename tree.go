package toml

import "bytes"

type kind uint8

const (
	// Nodes, which hold other nodes and tokens.
	kindDocument kind = iota
	kindKeyValue
	kindKey
	kindTable
	kindArrayTable
	kindArray
	kindInlineTable

	// Tokens, which hold bytes. Every kind from here on is a token.
	kindWhitespace
	kindNewline
	kindComment
	kindBareKey
	kindBasicString
	kindLiteralString
	kindMultilineBasicString
	kindMultilineLiteralString
	// kindScalar is an integer, a float, a boolean or a date-time, as written: its
	// spelling is checked by the decoder, not here.
	kindScalar
	kindEquals
	kindDot
	kindComma
	kindOpenBracket
	kindCloseBracket
	kindOpenDoubleBracket
	kindCloseDoubleBracket
	kindOpenBrace
	kindCloseBrace
)

// node is a node of the syntax tree, holding children, or a token, holding text; its
// kind says which. A document's bytes are its tokens' text, in order.
type node struct {
	kind     kind
	text     []byte
	children []*node
}

func (n *node) add(child *node) {
	n.children = append(n.children, child)
}

func (n *node) write(buf *bytes.Buffer) {
	if n.kind >= kindWhitespace {
		buf.Write(n.text)

		return
	}

	for _, child := range n.children {
		child.write(buf)
	}
}
