package syntax

import (
	"strconv"
	"strings"
)

// Tables resolves table headers, in document order, to the path of the table each
// opens: its keys, with an array of tables' element as its index, in decimal.
// [[a]] then [a.b] opens a/0/b; a second [[a]] opens a/1.
type Tables struct {
	// arrays is how many elements each array of tables has so far, by path.
	arrays map[string]int
}

// Path returns the path of the table header, KindTable or KindArrayTable, opens.
// Headers must be given in document order, each once.
func (t *Tables) Path(header *Node) []string {
	if t.arrays == nil {
		t.arrays = map[string]int{}
	}

	names := header.Key().Names()
	path := make([]string, 0, 2*len(names))

	for i, name := range names {
		path = append(path, name)
		known := identity(path)

		if i == len(names)-1 && header.Kind == KindArrayTable {
			t.arrays[known]++
		}

		if count, ok := t.arrays[known]; ok {
			path = append(path, strconv.Itoa(count-1))
		}
	}

	return path
}

// identity is a path as a map key, unambiguous whatever its keys hold.
func identity(path []string) string {
	var quoted strings.Builder

	for _, part := range path {
		quoted.WriteString(strconv.Quote(part))
	}

	return quoted.String()
}
