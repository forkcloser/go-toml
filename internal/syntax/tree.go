// Package syntax is the lossless syntax tree of a TOML document: every byte of the
// document is the text of one of its tokens, in order.
package syntax

import "bytes"

// Kind is what a node of the tree is.
type Kind uint8

// The kinds of nodes, which hold other nodes and tokens, then of tokens, which hold
// bytes.
const (
	KindDocument Kind = iota
	KindKeyValue
	KindKey
	KindTable
	KindArrayTable
	KindArray
	KindInlineTable

	// Every kind from here on is a token.
	KindWhitespace
	KindNewline
	KindComment
	KindBareKey
	KindBasicString
	KindLiteralString
	KindMultilineBasicString
	KindMultilineLiteralString
	// KindScalar is an integer, a float, a boolean or a date-time, as written: its
	// spelling is checked by the decoder, not here.
	KindScalar
	KindEquals
	KindDot
	KindComma
	KindOpenBracket
	KindCloseBracket
	KindOpenDoubleBracket
	KindCloseDoubleBracket
	KindOpenBrace
	KindCloseBrace
)

// Node is a node of the tree, holding children, or a token, holding text; its kind
// says which.
type Node struct {
	Kind     Kind
	Text     []byte
	Children []*Node
}

// IsToken reports whether the node is a token.
func (n *Node) IsToken() bool {
	return n.Kind >= KindWhitespace
}

// Write appends the node's text, its tokens' text in order, to buf.
func (n *Node) Write(buf *bytes.Buffer) {
	if n.IsToken() {
		buf.Write(n.Text)

		return
	}

	for _, child := range n.Children {
		child.Write(buf)
	}
}

func (n *Node) add(child *Node) {
	n.Children = append(n.Children, child)
}
