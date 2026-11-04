package schema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/forkcloser/go-toml/internal/order"
	"github.com/forkcloser/go-toml/internal/versionsort"
)

var (
	errAnnotation = errors.New(
		`want a strategy, or an object of "properties", "patternProperties" and "additionalProperties" to strategies`,
	)
	errStrategy = errors.New("unknown strategy")
	errGroup    = errors.New("unknown group")
	errDeclared = errors.New(`the "schema" strategy orders only "properties"`)
)

type strategy uint8

const (
	ascending strategy = iota
	descending
	versionSort
	declared
)

// keyKind is which of a table's keys a group holds.
type keyKind uint8

const (
	everyKey keyKind = iota
	propertyKeys
	patternKeys
	additionalKeys
)

// group is a strategy for some of a table's keys.
type group struct {
	keys     keyKind
	strategy strategy
}

func parseStrategy(name string) (strategy, bool) {
	switch name {
	case "ascending":
		return ascending, true
	case "descending":
		return descending, true
	case "version-sort":
		return versionSort, true
	case "schema":
		return declared, true
	default:
		return 0, false
	}
}

func parseKind(name string) (keyKind, bool) {
	switch name {
	case "properties":
		return propertyKeys, true
	case "patternProperties":
		return patternKeys, true
	case "additionalProperties":
		return additionalKeys, true
	default:
		return 0, false
	}
}

// parseOrder reads an annotation: one strategy for every key, or strategies by kind
// of key, applied in the order the object lists them.
func parseOrder(annotation *node) ([]group, error) {
	if name, ok := annotation.scalar.(string); ok {
		chosen, known := parseStrategy(name)
		if !known {
			return nil, fmt.Errorf("%w %q", errStrategy, name)
		}

		return []group{{keys: everyKey, strategy: chosen}}, nil
	}

	if annotation.members == nil {
		return nil, errAnnotation
	}

	groups := make([]group, 0, len(annotation.keys))

	for _, key := range annotation.keys {
		parsed, err := parseGroup(key, annotation.members[key])
		if err != nil {
			return nil, err
		}

		groups = append(groups, parsed)
	}

	return groups, nil
}

// parseGroup reads one member of an annotation's object: a kind of key, and its
// strategy.
func parseGroup(key string, member *node) (group, error) {
	keys, known := parseKind(key)
	if !known {
		return group{}, fmt.Errorf("%w %q: %w", errGroup, key, errAnnotation)
	}

	if member == nil {
		return group{}, errAnnotation
	}

	name, ok := member.scalar.(string)
	if !ok {
		return group{}, fmt.Errorf("group %q: %w", key, errAnnotation)
	}

	chosen, known := parseStrategy(name)
	if !known {
		return group{}, fmt.Errorf("group %q: %w %q", key, errStrategy, name)
	}

	if chosen == declared && keys != propertyKeys {
		return group{}, fmt.Errorf("group %q: %w", key, errDeclared)
	}

	return group{keys: keys, strategy: chosen}, nil
}

// Sorter returns the order of the keys of instance's tables, instance being a
// decoded document as [Instance] makes it.
func (s *Schema) Sorter(instance any) order.Sorter {
	return func(path, keys []string) []string {
		schemas, ok := s.at(path, instance)
		if !ok {
			return keys
		}

		return s.sort(schemas, keys)
	}
}

// at returns the subschemas that apply to the value at path.
func (s *Schema) at(path []string, instance any) ([]*jsonschema.Schema, bool) {
	value := instance
	schemas := closure([]*jsonschema.Schema{s.root}, value)

	for _, step := range path {
		var next []*jsonschema.Schema

		switch container := value.(type) {
		case map[string]any:
			child, ok := container[step]
			if !ok {
				return nil, false
			}

			next, value = propertySchemas(schemas, step), child
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(container) {
				return nil, false
			}

			next, value = itemSchemas(schemas, index), container[index]
		default:
			return nil, false
		}

		schemas = closure(next, value)
	}

	return schemas, true
}

// closure returns the subschemas that apply to value, starting from roots: those
// they refer to and combine, the branches of anyOf and oneOf value matches, and the
// then or else that if chooses.
func closure(roots []*jsonschema.Schema, value any) []*jsonschema.Schema {
	gather := &gathering{value: value, seen: map[*jsonschema.Schema]bool{}}

	for _, root := range roots {
		gather.visit(root)
	}

	return gather.result
}

type gathering struct {
	value  any
	seen   map[*jsonschema.Schema]bool
	result []*jsonschema.Schema
}

func (g *gathering) visit(subschema *jsonschema.Schema) {
	if subschema == nil || g.seen[subschema] {
		return
	}

	g.seen[subschema] = true
	g.result = append(g.result, subschema)

	g.visit(subschema.Ref)
	g.visit(subschema.RecursiveRef)

	if subschema.DynamicRef != nil {
		g.visit(subschema.DynamicRef.Ref)
	}

	for _, branch := range subschema.AllOf {
		g.visit(branch)
	}

	for _, branch := range slices.Concat(subschema.AnyOf, subschema.OneOf) {
		if branch.Validate(g.value) == nil {
			g.visit(branch)
		}
	}

	switch {
	case subschema.If == nil:
	case subschema.If.Validate(g.value) == nil:
		g.visit(subschema.Then)
	default:
		g.visit(subschema.Else)
	}
}

// propertySchemas returns the subschemas of schemas that apply to the member key.
func propertySchemas(schemas []*jsonschema.Schema, key string) []*jsonschema.Schema {
	var result []*jsonschema.Schema

	for _, subschema := range schemas {
		matched := false

		if property, ok := subschema.Properties[key]; ok {
			result = append(result, property)
			matched = true
		}

		for _, pattern := range sortedPatterns(subschema) {
			if pattern.MatchString(key) {
				result = append(result, subschema.PatternProperties[pattern])
				matched = true
			}
		}

		if additional, ok := subschema.AdditionalProperties.(*jsonschema.Schema); ok && !matched {
			result = append(result, additional)
		}
	}

	return result
}

// itemSchemas returns the subschemas of schemas that apply to the element at index.
func itemSchemas(schemas []*jsonschema.Schema, index int) []*jsonschema.Schema {
	var result []*jsonschema.Schema

	for _, subschema := range schemas {
		switch {
		case index < len(subschema.PrefixItems):
			result = append(result, subschema.PrefixItems[index])
		case subschema.Items2020 != nil:
			result = append(result, subschema.Items2020)
		}

		switch items := subschema.Items.(type) {
		case *jsonschema.Schema:
			result = append(result, items)
		case []*jsonschema.Schema:
			if index < len(items) {
				result = append(result, items[index])
			} else if additional, ok := subschema.AdditionalItems.(*jsonschema.Schema); ok {
				result = append(result, additional)
			}
		}
	}

	return result
}

// sortedPatterns returns the patterns of s's patternProperties in a stable order.
func sortedPatterns(subschema *jsonschema.Schema) []jsonschema.Regexp {
	patterns := make([]jsonschema.Regexp, 0, len(subschema.PatternProperties))

	for pattern := range subschema.PatternProperties {
		patterns = append(patterns, pattern)
	}

	slices.SortFunc(patterns, func(a, b jsonschema.Regexp) int { return strings.Compare(a.String(), b.String()) })

	return patterns
}

// sort orders keys of a table schemas apply to by the first annotation among them.
func (s *Schema) sort(schemas []*jsonschema.Schema, keys []string) []string {
	groups := s.annotation(schemas)
	if groups == nil {
		return keys
	}

	declaration := s.declaration(schemas)
	placed := map[string]bool{}
	result := make([]string, 0, len(keys))

	for _, chosen := range groups {
		var members []string

		for _, key := range keys {
			if !placed[key] && chosen.holds(key, declaration, schemas) {
				members = append(members, key)
			}
		}

		slices.SortStableFunc(members, compare(chosen.strategy, declaration))

		for _, key := range members {
			placed[key] = true
		}

		result = append(result, members...)
	}

	return result
}

// annotation returns the first annotation among schemas, or none.
func (s *Schema) annotation(schemas []*jsonschema.Schema) []group {
	for _, subschema := range schemas {
		if found := s.infos[subschema].order; found != nil {
			return found
		}
	}

	return nil
}

// declaration returns the properties schemas declare, in the order declared.
func (s *Schema) declaration(schemas []*jsonschema.Schema) []string {
	var result []string

	for _, subschema := range schemas {
		for _, property := range s.infos[subschema].properties {
			if !slices.Contains(result, property) {
				result = append(result, property)
			}
		}
	}

	return result
}

// holds reports whether the group orders key; the "schema" strategy orders only
// declared keys.
func (g group) holds(key string, declaration []string, schemas []*jsonschema.Schema) bool {
	if g.strategy == declared && !slices.Contains(declaration, key) {
		return false
	}

	return g.keys == everyKey || g.keys == kindOf(key, declaration, schemas)
}

func kindOf(key string, declaration []string, schemas []*jsonschema.Schema) keyKind {
	if slices.Contains(declaration, key) {
		return propertyKeys
	}

	for _, subschema := range schemas {
		for pattern := range subschema.PatternProperties {
			if pattern.MatchString(key) {
				return patternKeys
			}
		}
	}

	return additionalKeys
}

func compare(chosen strategy, declaration []string) func(a, b string) int {
	switch chosen {
	case ascending:
		return strings.Compare
	case descending:
		return func(a, b string) int { return strings.Compare(b, a) }
	case versionSort:
		return versionsort.Compare
	case declared:
		return func(a, b string) int { return slices.Index(declaration, a) - slices.Index(declaration, b) }
	default:
		panic("schema: an unknown strategy")
	}
}
