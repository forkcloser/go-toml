// Package render writes a Go value as a TOML value in the canonical spelling: what
// go-toml's encoder makes of it (a struct's fields in their order, a map's keys
// sorted, tables inline), each string a basic string, each float in its shortest
// form, each key bare where TOML allows it.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml/internal/format"
	"github.com/forkcloser/go-toml/internal/syntax"
)

// ErrValue is wrapped by the error about a Go value TOML cannot hold.
var ErrValue = errors.New("no TOML value for it")

var errNil = errors.New("nil")

// Value returns the value of v as a syntax tree, and as the decoder reads it back.
func Value(value any) (*syntax.Node, any, error) {
	var text bytes.Buffer

	encoder := gotoml.NewEncoder(&text)
	encoder.SetTablesInline(true)

	if err := encoder.Encode(map[string]any{"v": value}); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrValue, err)
	}

	var decoded map[string]any
	if err := gotoml.Unmarshal(text.Bytes(), &decoded); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrValue, err)
	}

	read, ok := decoded["v"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %w", ErrValue, errNil)
	}

	root, err := syntax.Parse(text.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrValue, err)
	}

	for _, child := range root.Children {
		if child.Kind == syntax.KindKeyValue {
			return canonical(child.Value(), read), read, nil
		}
	}

	return nil, nil, fmt.Errorf("%w: %w", ErrValue, errNil)
}

// canonical respells the value node, which decodes as read: its scalars and keys.
func canonical(node *syntax.Node, read any) *syntax.Node {
	switch node.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
	case syntax.KindArray:
		return canonicalArray(node, read)
	case syntax.KindInlineTable:
		return canonicalTable(node, read)
	default:
		return &syntax.Node{Kind: node.Kind, Text: []byte(Scalar(read))}
	}
}

func canonicalArray(node *syntax.Node, read any) *syntax.Node {
	elements, ok := read.([]any)
	if !ok {
		return node
	}

	children := make([]*syntax.Node, 0, len(node.Children))
	index := 0

	for _, child := range node.Children {
		if syntax.IsElement(child) && index < len(elements) {
			child = canonical(child, elements[index])
			index++
		}

		children = append(children, child)
	}

	return &syntax.Node{Kind: node.Kind, Children: children}
}

func canonicalTable(node *syntax.Node, read any) *syntax.Node {
	members, ok := read.(map[string]any)
	if !ok {
		return node
	}

	children := make([]*syntax.Node, 0, len(node.Children))

	for _, child := range node.Children {
		if child.Kind == syntax.KindKeyValue {
			child = member(child, members)
		}

		children = append(children, child)
	}

	return &syntax.Node{Kind: node.Kind, Children: children}
}

// member respells a key-value of an inline table holding members.
func member(pair *syntax.Node, members map[string]any) *syntax.Node {
	names := pair.Key().Names()

	var read any = members

	for _, name := range names {
		if table, ok := read.(map[string]any); ok {
			read = table[name]
		}
	}

	key := &syntax.Node{Kind: syntax.KindKey, Children: []*syntax.Node{
		{Kind: syntax.KindBareKey, Text: []byte(format.Names(names))},
	}}

	children := make([]*syntax.Node, 0, len(pair.Children))

	for _, child := range pair.Children {
		switch child {
		case pair.Key():
			child = key
		case pair.Value():
			child = canonical(child, read)
		}

		children = append(children, child)
	}

	return &syntax.Node{Kind: pair.Kind, Children: children}
}

// Scalar spells a decoded scalar: a string, an integer, a float, a boolean, a date
// or a time.
func Scalar(read any) string {
	switch typed := read.(type) {
	case string:
		return format.Quote(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return float(typed)
	case bool:
		return strconv.FormatBool(typed)
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func float(value float64) string {
	switch {
	case math.IsNaN(value):
		return "nan"
	case math.IsInf(value, 1):
		return "inf"
	case math.IsInf(value, -1):
		return "-inf"
	}

	text := strconv.FormatFloat(value, 'g', -1, 64)
	if !strings.ContainsAny(text, ".e") {
		text += ".0"
	}

	return text
}
