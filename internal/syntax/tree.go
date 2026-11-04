// Package syntax is the lossless syntax tree of a TOML document: every byte of the
// document is the text of one of its tokens, in order.
package syntax

import (
	"bytes"
	"strconv"
)

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

// Key returns the key of a key-value or a table header. Parse builds every one with
// a key, so a node without one is a bug in this package, and panics.
func (n *Node) Key() *Node {
	for _, child := range n.Children {
		if child.Kind == KindKey {
			return child
		}
	}

	panic("syntax: a node without a key: " + strconv.Itoa(int(n.Kind)))
}

// Value returns the value of a key-value, the node after its "=". Parse builds every
// key-value with a value, so one without is a bug in this package, and panics.
func (n *Node) Value() *Node {
	for i, child := range n.Children {
		if child.Kind != KindEquals {
			continue
		}

		for _, after := range n.Children[i+1:] {
			if after.Kind != KindWhitespace {
				return after
			}
		}
	}

	panic("syntax: a key-value without a value")
}

func (n *Node) add(child *Node) {
	n.Children = append(n.Children, child)
}
