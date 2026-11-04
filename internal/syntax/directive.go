package syntax

import (
	"bytes"
	"strings"
)

// Directive returns what the "#:schema" directive names, if the document's first line
// is one: a comment that is "#:schema", whitespace, and the schema's location.
func Directive(root *Node) (string, bool) {
	if len(root.Children) == 0 || root.Children[0].Kind != KindComment {
		return "", false
	}

	rest, ok := bytes.CutPrefix(root.Children[0].Text, []byte("#:schema"))
	if !ok || len(rest) == 0 || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}

	location := strings.TrimSpace(string(rest))

	return location, location != ""
}
