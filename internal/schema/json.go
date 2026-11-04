package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

var (
	errTrailing = errors.New("data after the JSON value")
	errKey      = errors.New("an object's key that is not a string")
)

// node is a JSON value with its objects' members in the order written, which the
// compiled schema does not keep and the "schema" key order is.
type node struct {
	keys    []string
	members map[string]*node
	items   []*node
	scalar  any
}

func parseJSON(data []byte) (*node, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	root, err := parseValue(decoder)
	if err != nil {
		return nil, err
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errTrailing
	}

	return root, nil
}

func parseValue(decoder *json.Decoder) (*node, error) {
	token, err := next(decoder)
	if err != nil {
		return nil, err
	}

	switch token {
	case json.Delim('{'):
		return parseObject(decoder)
	case json.Delim('['):
		return parseArray(decoder)
	default:
		return &node{scalar: token}, nil
	}
}

// parseObject reads an object's members, its opening brace read.
func parseObject(decoder *json.Decoder) (*node, error) {
	object := &node{members: map[string]*node{}}

	for decoder.More() {
		key, err := next(decoder)
		if err != nil {
			return nil, err
		}

		member, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}

		name, ok := key.(string)
		if !ok {
			return nil, errKey
		}

		if _, seen := object.members[name]; !seen {
			object.keys = append(object.keys, name)
		}

		object.members[name] = member
	}

	_, err := next(decoder)

	return object, err
}

// parseArray reads an array's elements, its opening bracket read.
func parseArray(decoder *json.Decoder) (*node, error) {
	array := &node{items: []*node{}}

	for decoder.More() {
		item, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}

		array.items = append(array.items, item)
	}

	_, err := next(decoder)

	return array, err
}

func next(decoder *json.Decoder) (json.Token, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read JSON: %w", err)
	}

	return token, nil
}

// plain returns the value as encoding/json decodes into any, numbers as json.Number:
// what the compiler takes.
func (n *node) plain() any {
	switch {
	case n.members != nil:
		object := make(map[string]any, len(n.members))

		for name, member := range n.members {
			object[name] = member.plain()
		}

		return object
	case n.items != nil:
		array := make([]any, len(n.items))

		for i, item := range n.items {
			array[i] = item.plain()
		}

		return array
	default:
		return n.scalar
	}
}

// at returns the value at a JSON pointer's tokens, already unescaped.
func (n *node) at(pointer []string) *node {
	current := n

	for _, token := range pointer {
		switch {
		case current == nil:
			return nil
		case current.members != nil:
			current = current.members[token]
		case current.items != nil:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(current.items) {
				return nil
			}

			current = current.items[index]
		default:
			return nil
		}
	}

	return current
}
