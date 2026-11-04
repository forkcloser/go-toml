// Package edit plans the byte ranges an edit of a document replaces: what a path
// names, where a new key-value or table goes, what removing one takes with it. Every
// edit is a set of splices of the document's bytes; nothing outside them changes.
package edit

import (
	"bytes"
	"slices"
	"strconv"

	"github.com/forkcloser/go-toml/internal/syntax"
)

// Splice replaces the bytes from Start to End with Text.
type Splice struct {
	Start, End int
	Text       string
}

// Apply returns src with the splices made; they must not overlap.
func Apply(src []byte, splices []Splice) []byte {
	sorted := slices.Clone(splices)
	slices.SortFunc(sorted, func(a, b Splice) int { return a.Start - b.Start })

	var out bytes.Buffer

	last := 0

	for _, splice := range sorted {
		out.Write(src[last:splice.Start])
		out.WriteString(splice.Text)
		last = splice.End
	}

	out.Write(src[last:])

	return out.Bytes()
}

// line is one line of the document: where it starts and ends, its line ending
// included, and what it holds.
type line struct {
	start, end int
	keyValue   *syntax.Node
	header     *syntax.Node
	comment    bool
}

func (l line) blank() bool {
	return l.keyValue == nil && l.header == nil && !l.comment
}

func (l line) commentOnly() bool {
	return l.keyValue == nil && l.header == nil && l.comment
}

// section is the root's lines before the first header, or a header and the lines
// after it up to the next: lines from first, the comments directly above a header
// included, to end, excluded.
type section struct {
	path   []string
	header *syntax.Node
	// first is the first line of the section; head the header's line, or first for
	// the root; end the line after the last.
	first, head, end int
	pairs            []pair
}

// bodyStart returns the first line after the header.
func (s section) bodyStart() int {
	if s.header == nil {
		return s.head
	}

	return s.head + 1
}

// pair is a key-value of a section, with the comment lines directly above it.
type pair struct {
	path    []string
	node    *syntax.Node
	line    int
	comment int
}

// Document is a parsed document indexed for editing.
type Document struct {
	src      []byte
	root     *syntax.Node
	offsets  map[*syntax.Node]int
	lines    []line
	sections []section
}

// New indexes the document src, whose tree is root.
func New(src []byte, root *syntax.Node) *Document {
	doc := &Document{src: src, root: root, offsets: map[*syntax.Node]int{}}

	offset := 0
	doc.measure(root, &offset)
	doc.split()
	doc.section()

	return doc
}

func (d *Document) measure(node *syntax.Node, offset *int) {
	d.offsets[node] = *offset

	if node.IsToken() {
		*offset += len(node.Text)

		return
	}

	for _, child := range node.Children {
		d.measure(child, offset)
	}
}

// span returns where a node starts and ends.
func (d *Document) span(node *syntax.Node) (start, end int) {
	var buf bytes.Buffer

	node.Write(&buf)

	return d.offsets[node], d.offsets[node] + buf.Len()
}

func (d *Document) split() {
	current := line{start: 0}

	for _, child := range d.root.Children {
		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindNewline:
			_, current.end = d.span(child)
			d.lines = append(d.lines, current)
			current = line{start: current.end}
		case syntax.KindKeyValue:
			current.keyValue = child
		case syntax.KindTable, syntax.KindArrayTable:
			current.header = child
		case syntax.KindComment:
			current.comment = true
		default:
		}
	}

	if current.start < len(d.src) {
		current.end = len(d.src)
		d.lines = append(d.lines, current)
	}
}

func (d *Document) section() {
	var tables syntax.Tables

	open := section{path: []string{}}

	for i, current := range d.lines {
		if current.header == nil {
			continue
		}

		first := i
		for first > open.bodyStart() && d.lines[first-1].commentOnly() {
			first--
		}

		d.close(&open, first)
		d.sections = append(d.sections, open)
		open = section{path: tables.Path(current.header), header: current.header, first: first, head: i}
	}

	d.close(&open, len(d.lines))
	d.sections = append(d.sections, open)
}

// close ends target at line end and indexes its key-values.
func (d *Document) close(target *section, end int) {
	target.end = end

	for i := target.bodyStart(); i < end; i++ {
		if d.lines[i].keyValue == nil {
			continue
		}

		comment := i
		for comment > target.bodyStart() && d.lines[comment-1].commentOnly() {
			comment--
		}

		target.pairs = append(target.pairs, pair{
			path:    append(slices.Clone(target.path), d.lines[i].keyValue.Key().Names()...),
			node:    d.lines[i].keyValue,
			line:    i,
			comment: comment,
		})
	}
}

// lastContent returns the line after the last line of s that is not blank, header
// included.
func (d *Document) lastContent(s section) int {
	floor := s.bodyStart()

	last := s.end
	for last > floor && d.lines[last-1].blank() {
		last--
	}

	return last
}

// lineStart returns the offset where line i starts, or the end of the document.
func (d *Document) lineStart(i int) int {
	if i < len(d.lines) {
		return d.lines[i].start
	}

	return len(d.src)
}

// endsLine reports whether the document ends with a line ending, or is empty.
func (d *Document) endsLine() bool {
	return len(d.src) == 0 || d.src[len(d.src)-1] == '\n'
}

// hasPrefix reports whether path starts with prefix.
func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// elements returns the elements of an array or an inline table, in order.
func elements(collection *syntax.Node) []*syntax.Node {
	result := make([]*syntax.Node, 0, len(collection.Children))

	for _, child := range collection.Children {
		if syntax.IsElement(child) {
			result = append(result, child)
		}
	}

	return result
}

// elementPath returns the path, relative to its collection, of an element.
func elementPath(collection *syntax.Node, index int, element *syntax.Node) []string {
	if collection.Kind == syntax.KindInlineTable {
		return element.Key().Names()
	}

	return []string{strconv.Itoa(index)}
}
