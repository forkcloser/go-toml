// Package format renders a syntax tree in the canonical layout: one space around
// "=", no indentation outside collections, one blank line before each table, blank
// lines collapsed, collections on one line when they fit, comments kept where they
// were. Values are written exactly as they were spelled; only what lies between
// tokens changes, and keys lose quotes they never needed.
package format

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/forkcloser/go-toml/internal/syntax"
)

// width is the column a collection on one line may reach.
const width = 100

// line is one line of a document: an expression, a comment, both, or neither.
type line struct {
	expression *syntax.Node
	comment    *syntax.Node
}

func (l line) blank() bool {
	return l.expression == nil && l.comment == nil
}

func (l line) commentOnly() bool {
	return l.expression == nil && l.comment != nil
}

func (l line) header() bool {
	return l.expression != nil &&
		(l.expression.Kind == syntax.KindTable || l.expression.Kind == syntax.KindArrayTable)
}

// Document renders the tree of a document in the canonical layout.
func Document(root *syntax.Node) []byte {
	lines := split(root)

	var out bytes.Buffer

	started, blank := false, false

	for i, current := range lines {
		if current.blank() {
			blank = started

			continue
		}

		if started && blankBefore(lines, i) {
			blank = true
		}

		if blank {
			out.WriteByte('\n')

			blank = false
		}

		if current.expression != nil {
			out.WriteString(expression(current.expression))

			if current.comment != nil {
				out.WriteByte(' ')
			}
		}

		if current.comment != nil {
			out.Write(current.comment.Text)
		}

		out.WriteByte('\n')

		started = true
	}

	return out.Bytes()
}

// split groups a document's top-level tokens and expressions into lines.
func split(root *syntax.Node) []line {
	var (
		lines   []line
		current line
		pending bool
	)

	for _, child := range root.Children {
		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindNewline:
			lines = append(lines, current)
			current, pending = line{}, false
		case syntax.KindWhitespace:
		case syntax.KindComment:
			current.comment, pending = child, true
		default:
			current.expression, pending = child, true
		}
	}

	if pending {
		lines = append(lines, current)
	}

	return lines
}

// blankBefore reports whether line i opens a table: a table header, or the first of
// the comments directly above one. A blank line goes before what opens a table.
func blankBefore(lines []line, i int) bool {
	if lines[i].header() {
		return i == 0 || !lines[i-1].commentOnly()
	}

	if !lines[i].commentOnly() || (i > 0 && lines[i-1].commentOnly()) {
		return false
	}

	end := i
	for end < len(lines) && lines[end].commentOnly() {
		end++
	}

	return end < len(lines) && lines[end].header()
}

func expression(node *syntax.Node) string {
	switch node.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
	case syntax.KindTable:
		return "[" + key(node.Key()) + "]"
	case syntax.KindArrayTable:
		return "[[" + key(node.Key()) + "]]"
	default:
		return keyValue(node, 0)
	}
}

// key renders a dotted key without the whitespace around its dots, each part bare
// when TOML allows it.
func key(n *syntax.Node) string {
	var parts []string

	for _, part := range n.Children {
		switch part.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindBareKey, syntax.KindBasicString, syntax.KindLiteralString:
			parts = append(parts, keyPart(part))
		default:
		}
	}

	return strings.Join(parts, ".")
}

// keyPart unquotes a quoted key whose content is a valid bare key, with no escape:
// "name" and 'name' are name; "a b" and "a\u0062" stay as written.
func keyPart(part *syntax.Node) string {
	text := string(part.Text)
	if part.Kind == syntax.KindBareKey {
		return text
	}

	content := text[1 : len(text)-1]
	if content == "" {
		return text
	}

	for i := range len(content) {
		if !syntax.IsBareKeyByte(content[i]) {
			return text
		}
	}

	return content
}

func keyValue(n *syntax.Node, indent int) string {
	prefix := key(n.Key()) + " = "

	return prefix + value(n.Value(), indent, indent*2+utf8.RuneCountInString(prefix))
}

// value renders a value starting at column, at the indent level of its line.
func value(n *syntax.Node, indent, column int) string {
	if n.IsToken() {
		return string(n.Text)
	}

	return collection(n, indent, column)
}

// entry is one element of a collection, with the comment trailing it on its line, or
// a comment standing on a line of its own.
type entry struct {
	element *syntax.Node
	comment *syntax.Node
}

// members is a collection taken apart: its brackets, the comment trailing the opening
// one, and its entries.
type members struct {
	open, closing string
	comment       *syntax.Node
	entries       []entry
	// trailingComma is a comma after the last element: the author's word that the
	// collection is one element per line, which the layout keeps.
	trailingComma bool
}

func membersOf(collection *syntax.Node) members {
	result := members{open: "[", closing: "]"}
	if collection.Kind == syntax.KindInlineTable {
		result.open, result.closing = "{", "}"
	}

	lineEnded := false

	for _, member := range collection.Children[1 : len(collection.Children)-1] {
		switch member.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindWhitespace:
		case syntax.KindComma:
			result.trailingComma = true
		case syntax.KindNewline:
			lineEnded = true
		case syntax.KindComment:
			last := len(result.entries) - 1

			switch {
			case !lineEnded && last < 0:
				result.comment = member
			case !lineEnded && result.entries[last].element != nil:
				result.entries[last].comment = member
			default:
				result.entries = append(result.entries, entry{comment: member})
			}
		default:
			result.entries = append(result.entries, entry{element: member})
			lineEnded, result.trailingComma = false, false
		}
	}

	return result
}

// collection renders an array or an inline table on one line when it holds no comment,
// has no comma after its last element, and fits in width from column; otherwise one
// entry per line, indented one level deeper than its line, each element followed by
// a comma. That comma is what keeps it one per line from then on, even once it would
// fit.
func collection(n *syntax.Node, indent, column int) string {
	parts := membersOf(n)

	if oneLine, ok := flat(n); ok && column+utf8.RuneCountInString(oneLine) <= width {
		return oneLine
	}

	var out strings.Builder

	out.WriteString(parts.open)

	if parts.comment != nil {
		out.WriteByte(' ')
		out.Write(parts.comment.Text)
	}

	out.WriteByte('\n')

	pad := strings.Repeat("  ", indent+1)

	for _, item := range parts.entries {
		out.WriteString(pad)

		if item.element != nil {
			out.WriteString(element(item.element, indent+1, len(pad)))
			out.WriteByte(',')

			if item.comment != nil {
				out.WriteByte(' ')
			}
		}

		if item.comment != nil {
			out.Write(item.comment.Text)
		}

		out.WriteByte('\n')
	}

	out.WriteString(strings.Repeat("  ", indent))
	out.WriteString(parts.closing)

	return out.String()
}

// element renders an element of a collection: a value, or an inline table's
// key-value.
func element(n *syntax.Node, indent, column int) string {
	if n.Kind == syntax.KindKeyValue {
		return keyValue(n, indent)
	}

	return value(n, indent, column)
}

// flat renders n on one line, if it can be: no comment inside it and no value
// spanning lines.
func flat(node *syntax.Node) (string, bool) {
	switch {
	case node.Kind == syntax.KindKeyValue:
		inner, ok := flat(node.Value())

		return key(node.Key()) + " = " + inner, ok
	case node.IsToken():
		return string(node.Text), !bytes.ContainsAny(node.Text, "\r\n")
	}

	parts := membersOf(node)
	if parts.comment != nil || parts.trailingComma {
		return "", false
	}

	rendered := make([]string, 0, len(parts.entries))

	for _, item := range parts.entries {
		if item.element == nil || item.comment != nil {
			return "", false
		}

		one, ok := flat(item.element)
		if !ok {
			return "", false
		}

		rendered = append(rendered, one)
	}

	joined := strings.Join(rendered, ", ")

	switch {
	case parts.open == "[":
		return "[" + joined + "]", true
	case joined == "":
		return "{}", true
	default:
		return "{ " + joined + " }", true
	}
}
