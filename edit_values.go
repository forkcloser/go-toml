package toml

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/forkcloser/go-toml/internal/schema"
)

// The meaning of an edit: what the document, decoded, holds after it. Each edit is
// held to it.

// lookup returns the value at path in a decoded document, and whether it is there.
// A path through a value that is neither a table nor an array is an error wrapping
// ErrConflict.
func lookup(doc map[string]any, path Path) (any, bool, error) {
	var current any = doc

	for i, each := range path.steps {
		next, found, err := child(current, each, Path{steps: path.steps[:i]})
		if err != nil || !found {
			return nil, false, err
		}

		current = next
	}

	return current, true, nil
}

// child returns what a step reaches from current, at here, and whether it is there.
func child(current any, each step, here Path) (any, bool, error) {
	switch container := current.(type) {
	case map[string]any:
		if !each.isKey {
			return nil, false, fmt.Errorf("%w: %s is a table, not an array", ErrConflict, here)
		}

		value, ok := container[each.key]

		return value, ok, nil
	case []any:
		if each.isKey {
			return nil, false, fmt.Errorf("%w: %s is an array, not a table", ErrConflict, here)
		}

		if each.index < 0 || each.index >= len(container) {
			return nil, false, nil
		}

		return container[each.index], true, nil
	default:
		return nil, false, fmt.Errorf("%w: %s is a %s", ErrConflict, here, kindOf(current))
	}
}

// setValue returns a copy of doc with value at path: tables made for missing keys,
// an element added past an array's last.
func setValue(doc map[string]any, path Path, value any) (map[string]any, error) {
	result := cloneTable(doc)

	parent, err := makeTables(result, path)
	if err != nil {
		return nil, err
	}

	last := path.last()

	switch container := parent.(type) {
	case map[string]any:
		if !last.isKey || container == nil {
			return nil, fmt.Errorf("%w: %s is a table, not an array", ErrConflict, path.parent())
		}

		container[last.key] = value
	case []any:
		switch {
		case last.isKey:
			return nil, fmt.Errorf("%w: %s is an array, not a table", ErrConflict, path.parent())
		case last.index == len(container):
			replaceIn(result, path.parent(), append(container, value))
		case last.index < 0 || last.index > len(container):
			return nil, fmt.Errorf("%w: %s has %d elements", ErrNotFound, path.parent(), len(container))
		default:
			container[last.index] = value
		}
	default:
		return nil, fmt.Errorf("%w: %s is a %s", ErrConflict, path.parent(), kindOf(parent))
	}

	return result, nil
}

// makeTables returns the parent of path in doc, making a table for each missing key
// on the way; a missing key that an index follows is no array, and an error.
func makeTables(doc map[string]any, path Path) (any, error) {
	var current any = doc

	steps := path.parent().steps

	for i, each := range steps {
		next, found, err := child(current, each, Path{steps: steps[:i]})
		if err != nil {
			return nil, err
		}

		if !found {
			table, isTable := current.(map[string]any)
			if !isTable || !each.isKey || !path.steps[i+1].isKey {
				return nil, fmt.Errorf("%w: no array at %s", ErrNotFound, Path{steps: steps[:i+1]})
			}

			next = map[string]any{}
			table[each.key] = next
		}

		current = next
	}

	return current, nil
}

// deleteValue returns a copy of doc without the value at path.
func deleteValue(doc map[string]any, path Path) (map[string]any, error) {
	if _, found, err := lookup(doc, path); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("%w: %s", ErrNotFound, path)
		}

		return nil, err
	}

	result := cloneTable(doc)
	parent, _, _ := lookup(result, path.parent())

	switch container := parent.(type) {
	case map[string]any:
		delete(container, path.last().key)
	case []any:
		replaceIn(result, path.parent(), slices.Delete(container, path.last().index, path.last().index+1))
	}

	return result, nil
}

// replaceIn sets the value at path, whose parent is there, to value.
func replaceIn(doc map[string]any, path Path, value any) {
	parent, _, _ := lookup(doc, path.parent())

	switch container := parent.(type) {
	case map[string]any:
		if container != nil {
			container[path.last().key] = value
		}
	case []any:
		if path.last().index < len(container) {
			container[path.last().index] = value
		}
	}
}

// prune drops from expected the tables and arrays along path that an edit left
// empty and the document no longer writes: a table that only dotted keys or the
// headers under it defined, an array of tables that lost its last element.
func prune(expected, after map[string]any, path []string) {
	for depth := len(path) - 1; depth >= 1; depth-- {
		value, ok := at(expected, path[:depth])
		if !ok || !empty(value) {
			return
		}

		if _, there := at(after, path[:depth]); there {
			return
		}

		parent, _ := at(expected, path[:depth-1])
		if table, isTable := parent.(map[string]any); isTable {
			delete(table, path[depth-1])
		}
	}
}

// at returns the value at path, an index in decimal, in a decoded document.
func at(doc map[string]any, path []string) (any, bool) {
	var current any = doc

	for _, name := range path {
		switch container := current.(type) {
		case map[string]any:
			value, ok := container[name]
			if !ok {
				return nil, false
			}

			current = value
		case []any:
			index, err := strconv.Atoi(name)
			if err != nil || index < 0 || index >= len(container) {
				return nil, false
			}

			current = container[index]
		default:
			return nil, false
		}
	}

	return current, true
}

func empty(value any) bool {
	switch container := value.(type) {
	case map[string]any:
		return len(container) == 0
	case []any:
		return len(container) == 0
	default:
		return false
	}
}

func cloneTable(table map[string]any) map[string]any {
	result := make(map[string]any, len(table))
	for key, member := range table {
		result[key] = clone(member)
	}

	return result
}

func clone(value any) any {
	switch container := value.(type) {
	case map[string]any:
		return cloneTable(container)
	case []any:
		result := make([]any, len(container))
		for i, element := range container {
			result[i] = clone(element)
		}

		return result
	default:
		return value
	}
}

func schemaInstance(doc map[string]any) any {
	return schema.Instance(doc)
}
