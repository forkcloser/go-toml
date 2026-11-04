// Package order rearranges the keys of a document's tables in the order a caller
// decides, moving each key-value with its comments: those on the lines directly above
// it and the one trailing it. Keys move within a run of lines between blank lines,
// never across one, never past a table header; dotted keys sharing their first part
// are gathered, at the place of the first, and ordered within it in turn. The
// elements of arrays keep their order, as do table headers and a "#:schema"
// directive on the first line.
package order

import (
	"slices"
	"strconv"

	"github.com/forkcloser/go-toml/internal/syntax"
)

// Sorter returns keys, the distinct first parts of keys written together in the table
// at path, in the order they are to be written. A key it leaves out keeps its place
// after those it returns, in the order written.
type Sorter func(path, keys []string) []string

// Keys returns the tree of the document root with its keys in sort's order. root is
// left as it is: the result shares the nodes that do not change.
func Keys(root *syntax.Node, sort Sorter) *syntax.Node {
	lines := split(root.Children)
	out := make([][]*syntax.Node, 0, len(lines))

	var (
		tables  syntax.Tables
		section []string
		run     []line
	)

	flush := func() {
		out = append(out, arrange(run, section, sort)...)
		run = nil
	}

	_, directive := syntax.Directive(root)

	for i, current := range lines {
		switch {
		case i == 0 && directive:
			out = append(out, current.nodes)
		case current.header != nil:
			flush()

			section = tables.Path(current.header)
			out = append(out, current.nodes)
		case current.blank():
			flush()

			out = append(out, current.nodes)
		default:
			run = append(run, current)
		}
	}

	flush()

	children := make([]*syntax.Node, 0, len(root.Children))

	for i, nodes := range out {
		children = append(children, nodes...)

		if i < len(out)-1 && nodes[len(nodes)-1].Kind != syntax.KindNewline {
			children = append(children, &syntax.Node{Kind: syntax.KindNewline, Text: []byte("\n")})
		}
	}

	return &syntax.Node{Kind: root.Kind, Children: children}
}

// line is one line of a document: its nodes, the line ending included, and the
// expression it holds, if any.
type line struct {
	nodes    []*syntax.Node
	keyValue *syntax.Node
	header   *syntax.Node
	comment  bool
}

func (l line) blank() bool {
	return l.keyValue == nil && l.header == nil && !l.comment
}

func split(children []*syntax.Node) []line {
	var (
		lines   []line
		current line
	)

	for _, child := range children {
		current.nodes = append(current.nodes, child)

		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindNewline:
			lines = append(lines, current)
			current = line{}
		case syntax.KindKeyValue:
			current.keyValue = child
		case syntax.KindTable, syntax.KindArrayTable:
			current.header = child
		case syntax.KindComment:
			current.comment = true
		default:
		}
	}

	if len(current.nodes) > 0 {
		lines = append(lines, current)
	}

	return lines
}

// item is a key-value and the comments that move with it, as the nodes to write, and
// its key's parts not yet ordered.
type item struct {
	nodes []*syntax.Node
	names []string
}

// arrange orders the key-values of a run of lines of the table at path; comments
// below the last key-value stay below it.
func arrange(run []line, path []string, sort Sorter) [][]*syntax.Node {
	var (
		items   []item
		pending []*syntax.Node
	)

	for _, current := range run {
		if current.keyValue == nil {
			pending = append(pending, current.nodes...)

			continue
		}

		names := current.keyValue.Key().Names()
		nodes := replace(
			current.nodes,
			current.keyValue,
			keyValue(current.keyValue, append(slices.Clip(path), names...), sort),
		)
		items = append(items, item{nodes: append(pending, nodes...), names: names})
		pending = nil
	}

	var out [][]*syntax.Node

	for _, entry := range ordered(items, path, sort) {
		out = append(out, entry.nodes)
	}

	if len(pending) > 0 {
		out = append(out, pending)
	}

	return out
}

// ordered returns items in sort's order of their first key parts, those sharing one
// together and ordered by their next parts in turn.
func ordered(items []item, path []string, sort Sorter) []item {
	if len(items) < 2 || slices.ContainsFunc(items, func(entry item) bool { return len(entry.names) == 0 }) {
		return items
	}

	var (
		firsts []string
		byName = map[string][]item{}
	)

	for _, entry := range items {
		first := entry.names[0]
		if _, seen := byName[first]; !seen {
			firsts = append(firsts, first)
		}

		byName[first] = append(byName[first], item{nodes: entry.nodes, names: entry.names[1:]})
	}

	result := make([]item, 0, len(items))

	for _, first := range permutation(firsts, sort(path, slices.Clone(firsts))) {
		group := byName[first]
		if len(group) > 1 {
			group = ordered(group, append(slices.Clip(path), first), sort)
		}

		for _, entry := range group {
			result = append(result, item{nodes: entry.nodes, names: append([]string{first}, entry.names...)})
		}
	}

	return result
}

// permutation returns keys in the order of sorted, a sorter's answer: the keys it
// names, once each, then those it left out, as written.
func permutation(keys, sorted []string) []string {
	result := make([]string, 0, len(keys))
	placed := map[string]bool{}

	for _, key := range sorted {
		if slices.Contains(keys, key) && !placed[key] {
			result = append(result, key)
			placed[key] = true
		}
	}

	for _, key := range keys {
		if !placed[key] {
			result = append(result, key)
		}
	}

	return result
}

func replace(nodes []*syntax.Node, old, replacement *syntax.Node) []*syntax.Node {
	if old == replacement {
		return nodes
	}

	result := slices.Clone(nodes)
	result[slices.Index(result, old)] = replacement

	return result
}

// keyValue returns the key-value n, of the value at path, with the keys of the
// tables its value holds ordered.
func keyValue(pair *syntax.Node, path []string, sort Sorter) *syntax.Node {
	value := pair.Value()

	ordered := collection(value, path, sort)
	if ordered == value {
		return pair
	}

	return &syntax.Node{Kind: pair.Kind, Children: replace(pair.Children, value, ordered)}
}

// collection returns the value, at path, with the keys of the inline tables it is
// or holds ordered.
func collection(value *syntax.Node, path []string, sort Sorter) *syntax.Node {
	switch value.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
	case syntax.KindArray:
		children := make([]*syntax.Node, 0, len(value.Children))
		index := 0

		for _, child := range value.Children {
			if syntax.IsElement(child) {
				child = collection(child, append(slices.Clip(path), strconv.Itoa(index)), sort)
				index++
			}

			children = append(children, child)
		}

		return &syntax.Node{Kind: value.Kind, Children: children}
	case syntax.KindInlineTable:
		return inlineTable(value, path, sort)
	default:
		return value
	}
}

// inlineTable returns the inline table, at path, with its keys ordered. It is
// rebuilt as the canonical layout reads it: the comment after its opening brace,
// then each key-value with the comment lines above it and the comment trailing it,
// a line ending after each, then the comment lines after the last key-value; a
// comma after each key-value but the last, and after the last if it had one.
func inlineTable(table *syntax.Node, path []string, sort Sorter) *syntax.Node {
	inner := table.Children[1 : len(table.Children)-1]

	var (
		opening       *syntax.Node
		items         []item
		pending       []*syntax.Node
		lineEnded     bool
		trailingComma bool
	)

	newline := &syntax.Node{Kind: syntax.KindNewline, Text: []byte("\n")}

	for _, child := range inner {
		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindNewline:
			lineEnded = true
		case syntax.KindComma:
			trailingComma = true
		case syntax.KindComment:
			switch {
			case !lineEnded && len(items) == 0 && len(pending) == 0:
				opening = child
			case !lineEnded && len(items) > 0 && len(pending) == 0:
				last := &items[len(items)-1]
				last.nodes = append(last.nodes, child)
			default:
				pending = append(pending, child, newline)
			}
		case syntax.KindKeyValue:
			names := child.Key().Names()
			ordered := keyValue(child, append(slices.Clip(path), names...), sort)
			items = append(items, item{nodes: append(pending, ordered), names: names})
			pending, lineEnded, trailingComma = nil, false, false
		default:
		}
	}

	children := []*syntax.Node{table.Children[0]}
	if opening != nil {
		children = append(children, opening, newline)
	}

	comma := &syntax.Node{Kind: syntax.KindComma, Text: []byte(",")}
	sorted := ordered(items, path, sort)

	for i, entry := range sorted {
		element := slices.IndexFunc(
			entry.nodes,
			func(child *syntax.Node) bool { return child.Kind == syntax.KindKeyValue },
		)
		children = append(children, entry.nodes[:element+1]...)

		if i < len(sorted)-1 || trailingComma {
			children = append(children, comma)
		}

		children = append(children, entry.nodes[element+1:]...)
		children = append(children, newline)
	}

	children = append(children, pending...)
	children = append(children, table.Children[len(table.Children)-1])

	return &syntax.Node{Kind: table.Kind, Children: children}
}
