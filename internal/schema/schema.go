// Package schema validates decoded TOML against a JSON Schema and orders a table's
// keys by the schema's x-tombi-table-keys-order annotations.
package schema

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/forkcloser/xos"
)

// ErrInvalid is wrapped by the error about a file that is not a JSON Schema, or
// whose annotations this package cannot apply.
var ErrInvalid = errors.New("invalid JSON Schema")

var errRemote = errors.New("a schema is a local file, never fetched")

// annotation is the keyword naming a table's key order.
const annotation = "x-tombi-table-keys-order"

// Schema is a compiled JSON Schema, with what ordering keys needs of each of its
// subschemas.
type Schema struct {
	root  *jsonschema.Schema
	infos map[*jsonschema.Schema]info
}

// info is what a subschema says of key order: its annotation, if any, and its
// properties in the order declared.
type info struct {
	order      []group
	properties []string
}

// Load reads and compiles the schema at path, and the local files it refers to.
func Load(path string) (*Schema, error) {
	files := &loader{documents: map[string]*node{}}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(files)

	root, err := compiler.Compile(path)

	switch {
	case files.err != nil:
		return nil, files.err
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	result := &Schema{root: root, infos: map[*jsonschema.Schema]info{}}

	for _, subschema := range reachable(root) {
		found, err := files.info(subschema.Location)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, subschema.Location, err)
		}

		result.infos[subschema] = found
	}

	return result, nil
}

// loader reads schema files, keeping each one's members in order. Its first I/O
// error is kept apart: the compiler reports it as a schema fault.
type loader struct {
	documents map[string]*node
	err       error
}

func (l *loader) Load(location string) (any, error) {
	path, err := jsonschema.FileLoader{}.ToFile(location)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errRemote, location)
	}

	data, err := xos.ReadFile(path)
	if err != nil {
		err = fmt.Errorf("read the schema: %w", err)
		if l.err == nil {
			l.err = err
		}

		return nil, err
	}

	ordered, err := parseJSON(data)
	if err != nil {
		return nil, err
	}

	l.documents[path] = ordered

	return ordered.plain(), nil
}

// info reads the annotation and the properties' order of the subschema at location,
// a file URL and a JSON pointer.
func (l *loader) info(location string) (info, error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return info{}, fmt.Errorf("the location of a subschema: %w", err)
	}

	fragment := parsed.Fragment
	parsed.Fragment = ""

	path, err := jsonschema.FileLoader{}.ToFile(parsed.String())
	if err != nil {
		return info{}, nil //nolint:nilerr // a subschema of a built-in metaschema: it orders nothing
	}

	document := l.documents[path]
	if document == nil || (fragment != "" && !strings.HasPrefix(fragment, "/")) {
		return info{}, nil
	}

	var pointer []string
	if fragment != "" {
		pointer = strings.Split(fragment[1:], "/")
	}

	for i, token := range pointer {
		pointer[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}

	object := document.at(pointer)
	if object == nil || object.members == nil {
		return info{}, nil
	}

	result := info{}

	if properties := object.members["properties"]; properties != nil {
		result.properties = properties.keys
	}

	if keysOrder := object.members[annotation]; keysOrder != nil {
		result.order, err = parseOrder(keysOrder)
		if err != nil {
			return info{}, fmt.Errorf("%s: %w", annotation, err)
		}
	}

	return result, nil
}

// reachable returns every subschema the root reaches through the keywords that
// decide what applies to a table or an array element.
func reachable(root *jsonschema.Schema) []*jsonschema.Schema {
	seen := map[*jsonschema.Schema]bool{}

	var result []*jsonschema.Schema

	queue := []*jsonschema.Schema{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == nil || seen[current] {
			continue
		}

		seen[current] = true
		result = append(result, current)
		queue = append(queue, children(current)...)
	}

	return result
}

func children(subschema *jsonschema.Schema) []*jsonschema.Schema {
	result := []*jsonschema.Schema{
		subschema.Ref,
		subschema.RecursiveRef,
		subschema.If,
		subschema.Then,
		subschema.Else,
		subschema.Items2020,
	}

	if subschema.DynamicRef != nil {
		result = append(result, subschema.DynamicRef.Ref)
	}

	result = append(result, subschema.AllOf...)
	result = append(result, subschema.AnyOf...)
	result = append(result, subschema.OneOf...)
	result = append(result, subschema.PrefixItems...)

	for _, property := range subschema.Properties {
		result = append(result, property)
	}

	for _, pattern := range subschema.PatternProperties {
		result = append(result, pattern)
	}

	if additional, ok := subschema.AdditionalProperties.(*jsonschema.Schema); ok {
		result = append(result, additional)
	}

	switch items := subschema.Items.(type) {
	case *jsonschema.Schema:
		result = append(result, items)
	case []*jsonschema.Schema:
		result = append(result, items...)
	}

	if additional, ok := subschema.AdditionalItems.(*jsonschema.Schema); ok {
		result = append(result, additional)
	}

	return result
}
