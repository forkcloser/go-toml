// Package locate finds where a document defines a value: the line and column of the
// key, header or element that holds it, for a path of keys and array indices.
package locate

import (
	"slices"
	"strconv"

	"github.com/forkcloser/go-toml/internal/syntax"
)

// definition is a place that defines the value at path.
type definition struct {
	path         []string
	line, column int
}

// Index is every definition in a document, in document order.
type Index struct {
	definitions []definition
}

// New indexes the definitions of the document whose tree is root.
func New(root *syntax.Node) *Index {
	walk := &walker{line: 1, column: 1}
	walk.define(nil)

	var (
		tables  syntax.Tables
		section []string
	)

	for _, child := range root.Children {
		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindTable, syntax.KindArrayTable:
			section = tables.Path(child)
			walk.define(section)
			walk.skip(child)
		case syntax.KindKeyValue:
			walk.keyValue(section, child)
		default:
			walk.skip(child)
		}
	}

	return &Index{definitions: walk.definitions}
}

// Find returns the line and column, 1-based and in characters, of the definition of
// path: where the value at path is written; else, for a table defined only through
// what it holds, the first definition inside it; else the definition of its nearest
// enclosing value.
func (x *Index) Find(path []string) (line, column int) {
	for _, d := range x.definitions {
		if slices.Equal(d.path, path) {
			return d.line, d.column
		}
	}

	for _, d := range x.definitions {
		if len(d.path) > len(path) && slices.Equal(d.path[:len(path)], path) {
			return d.line, d.column
		}
	}

	best := x.definitions[0]

	for _, d := range x.definitions {
		if len(d.path) > len(best.path) && len(d.path) < len(path) && slices.Equal(path[:len(d.path)], d.path) {
			best = d
		}
	}

	return best.line, best.column
}

// walker goes through the tree's tokens in order, keeping the line and column of the
// next one.
type walker struct {
	line, column int
	definitions  []definition
}

func (w *walker) define(path []string) {
	w.definitions = append(w.definitions, definition{path: path, line: w.line, column: w.column})
}

// skip moves past every token of node.
func (w *walker) skip(node *syntax.Node) {
	if !node.IsToken() {
		for _, child := range node.Children {
			w.skip(child)
		}

		return
	}

	for _, r := range string(node.Text) {
		if r == '\n' {
			w.line++
			w.column = 1
		} else {
			w.column++
		}
	}
}

// keyValue defines each prefix of the key, at its part, and what the value holds.
func (w *walker) keyValue(table []string, pair *syntax.Node) {
	key, value := pair.Key(), pair.Value()
	path := slices.Clone(table)

	for _, child := range pair.Children {
		if child != key {
			if child == value {
				w.value(path, child)
			} else {
				w.skip(child)
			}

			continue
		}

		for _, part := range key.Children {
			if name, ok := syntax.Name(part); ok {
				path = append(slices.Clip(path), name)
				w.define(path)
			}

			w.skip(part)
		}
	}
}

// value defines what an array or an inline table holds, by index or key.
func (w *walker) value(path []string, value *syntax.Node) {
	switch value.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
	case syntax.KindArray:
		index := 0

		for _, child := range value.Children {
			if !syntax.IsElement(child) {
				w.skip(child)

				continue
			}

			element := append(slices.Clip(path), strconv.Itoa(index))
			w.define(element)
			w.value(element, child)

			index++
		}
	case syntax.KindInlineTable:
		for _, child := range value.Children {
			if child.Kind == syntax.KindKeyValue {
				w.keyValue(path, child)
			} else {
				w.skip(child)
			}
		}
	default:
		w.skip(value)
	}
}
