package toml

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml/internal/edit"
	"github.com/forkcloser/go-toml/internal/format"
	"github.com/forkcloser/go-toml/internal/order"
	"github.com/forkcloser/go-toml/internal/render"
	"github.com/forkcloser/go-toml/internal/syntax"
)

var (
	// ErrNotFound is wrapped by the error about a path that names nothing the edit
	// needs: a value to delete, an element past an array's end.
	ErrNotFound = errors.New("nothing at the path")
	// ErrConflict is wrapped by the error about an edit the document's shape does not
	// allow: a key under a value that is not a table, a section under an inline
	// table, a value replacing a table written as sections.
	ErrConflict = errors.New("the path does not fit the document")
	// ErrValue is wrapped by the error about a Go value TOML cannot hold: nil, an
	// unsigned integer past int64, a channel.
	ErrValue = render.ErrValue
)

// Path names a value of a document: keys, and the elements of arrays, arrays of
// tables included, by index.
type Path struct {
	steps []step
}

type step struct {
	key   string
	index int
	isKey bool
}

// Key returns the path of the keys, from the root.
func Key(names ...string) Path {
	return Path{}.Key(names...)
}

// Key returns the path extended by keys.
func (p Path) Key(names ...string) Path {
	steps := slices.Clip(p.steps)

	for _, name := range names {
		steps = append(steps, step{key: name, isKey: true})
	}

	return Path{steps: steps}
}

// Index returns the path extended by an array's element.
func (p Path) Index(index int) Path {
	return Path{steps: append(slices.Clip(p.steps), step{index: index})}
}

// String returns the path as TOML spells keys, an index in brackets.
func (p Path) String() string {
	var out strings.Builder

	for i, each := range p.steps {
		switch {
		case !each.isKey:
			fmt.Fprintf(&out, "[%d]", each.index)
		case i > 0:
			out.WriteString("." + format.Name(each.key))
		default:
			out.WriteString(format.Name(each.key))
		}
	}

	return out.String()
}

// names returns the path's steps as the tree names them: an index in decimal.
func (p Path) names() []string {
	result := make([]string, 0, len(p.steps))

	for _, each := range p.steps {
		if each.isKey {
			result = append(result, each.key)
		} else {
			result = append(result, strconv.Itoa(each.index))
		}
	}

	return result
}

// keys returns the path's keys, its indices left out: a header's key.
func (p Path) keys() []string {
	result := make([]string, 0, len(p.steps))

	for _, each := range p.steps {
		if each.isKey {
			result = append(result, each.key)
		}
	}

	return result
}

func (p Path) last() step {
	return p.steps[len(p.steps)-1]
}

func (p Path) parent() Path {
	return Path{steps: p.steps[:len(p.steps)-1]}
}

// Set writes value at path as a key-value. The key-value written at path, if any,
// keeps its key, its comments and its place: only its value's bytes change. A new
// key-value goes in its table: the table's section, the run of dotted keys that
// defines it, or the inline table that holds it, after the last of its key-values,
// or where the schema [WithSchema] gives orders it; a table known only through the
// headers of tables under it gets a header of its own, before them. A table value
// is written as an inline table. A table written as sections is not a value to set:
// [Document.SetTable] replaces it.
func (d *Document) Set(path Path, value any, opts ...Option) error {
	return d.edit(path, opts, func(change *editing) error {
		node, read, err := render.Value(value)
		if err != nil {
			return change.fail(err, "")
		}

		change.expected, err = setValue(change.before, path, read)
		if err != nil {
			return change.fail(err, "")
		}

		return change.set(path, node)
	})
}

// SetTable writes value, a table, at path as a section: a header of its own and
// its members as key-values, tables among them inline. A table at path is replaced:
// its own section in its place, everything else defining it removed. A new section
// goes after the sections of its parent table, or where the schema orders it among
// its siblings; at the end of the document when its parent has none. path may end
// with an index, an array of tables' element, to replace that element.
func (d *Document) SetTable(path Path, value any, opts ...Option) error {
	return d.edit(path, opts, func(change *editing) error {
		node, read, err := render.Value(value)
		if err != nil {
			return change.fail(err, "")
		}

		if _, ok := read.(map[string]any); !ok {
			return change.fail(ErrConflict, "SetTable writes a table, not a "+kindOf(read))
		}

		if change.expected, err = setValue(change.before, path, read); err != nil {
			return change.fail(err, "")
		}

		return change.setTable(path, node)
	})
}

// Append adds value at the end of the array at path: an array of tables gets a
// section for it, an array written inline an element. An array that does not exist
// yet is written as an array of tables when value is a table, as an array holding
// value otherwise.
func (d *Document) Append(path Path, value any, opts ...Option) error {
	return d.edit(path, opts, func(change *editing) error {
		node, read, err := render.Value(value)
		if err != nil {
			return change.fail(err, "")
		}

		existing, found, err := lookup(change.before, path)
		if err != nil {
			return change.fail(err, "")
		}

		array, isArray := existing.([]any)
		if found && !isArray {
			return change.fail(ErrConflict, "Append adds to an array, not a "+kindOf(existing))
		}

		if found {
			change.expected, err = setValue(change.before, path.Index(len(array)), read)
		} else {
			change.expected, err = setValue(change.before, path, []any{read})
		}

		if err != nil {
			return change.fail(err, "")
		}

		return change.append(path, len(array), node, read, found)
	})
}

// Delete removes what path names, and everything written under it: its sections,
// with the comments above their headers; its key-values, with the comments above
// them; its elements of inline tables and arrays.
func (d *Document) Delete(path Path) error {
	return d.edit(path, nil, func(change *editing) error {
		var err error

		if change.expected, err = deleteValue(change.before, path); err != nil {
			return change.fail(err, "")
		}

		change.splice(change.index.Delete(path.names(), -1))

		return nil
	})
}

// editing is one edit under way: the document as it was, decoded, what it must
// decode to after, and the bytes as edited so far.
type editing struct {
	path     Path
	src      []byte
	root     *syntax.Node
	index    *edit.Document
	before   map[string]any
	expected map[string]any
	schema   *Schema
}

// edit runs an edit and holds it to its meaning: the document after must parse and
// decode to expected. The document changes only when it does.
func (d *Document) edit(path Path, opts []Option, run func(*editing) error) error {
	if len(path.steps) == 0 {
		return &Error{Err: ErrConflict, Msg: "the root is not a value to edit"}
	}

	var chosen options

	for _, opt := range opts {
		opt(&chosen)
	}

	src := d.Bytes()

	var before map[string]any
	if err := gotoml.Unmarshal(src, &before); err != nil {
		return wrap(src, err, ErrInvalid)
	}

	change := &editing{
		path:   path,
		src:    src,
		root:   d.root,
		index:  edit.New(src, d.root),
		before: before,
		schema: chosen.schema,
	}

	if err := run(change); err != nil {
		return err
	}

	after, err := Parse(change.src)
	if err != nil {
		return &Error{
			Err: ErrInternal,
			Key: path.names(),
			Msg: "the edit made a document that is not TOML: " + err.Error(),
		}
	}

	var decoded map[string]any
	if err := gotoml.Unmarshal(change.src, &decoded); err != nil {
		return &Error{Err: ErrInternal, Key: path.names(), Msg: err.Error()}
	}

	prune(change.expected, decoded, path.names())

	if !equal(change.expected, decoded) {
		return &Error{Err: ErrInternal, Key: path.names(), Msg: "the edit changed the document beyond the edit"}
	}

	d.root = after.root

	return nil
}

func (e *editing) fail(err error, msg string) error {
	sentinel := err
	for _, known := range []error{ErrNotFound, ErrConflict, ErrValue} {
		if errors.Is(err, known) {
			sentinel = known
		}
	}

	if msg == "" {
		msg = strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	}

	return &Error{Err: sentinel, Key: e.path.names(), Msg: msg}
}

// splice applies splices to the bytes and indexes the result.
func (e *editing) splice(splices []edit.Splice) {
	e.src = edit.Apply(e.src, splices)

	root, err := syntax.Parse(e.src)
	if err != nil {
		// The meaning check reports it: the bytes are what they are.
		return
	}

	e.root = root
	e.index = edit.New(e.src, root)
}

// sorter returns the order of keys the schema gives the document as it will be, or
// nil.
func (e *editing) sorter() order.Sorter {
	if e.schema == nil {
		return nil
	}

	return e.schema.compiled.Sorter(schemaInstance(e.expected))
}

// set writes node, a rendered value, at path as a key-value.
func (e *editing) set(path Path, node *syntax.Node) error {
	names := path.names()

	if exact, ok := e.index.Exact(names); ok {
		indent, column := e.index.Place(exact)
		e.splice([]edit.Splice{e.index.Replace(exact, format.ValueAt(e.ordered(names, node), indent, column))})

		return nil
	}

	if e.index.Sections(names) {
		return e.fail(ErrConflict, path.String()+" is a table written as sections: SetTable replaces it")
	}

	if _, found, _ := lookup(e.before, path); found {
		e.splice(e.index.Delete(names, -1))
	}

	return e.insert(path, node)
}

// insert writes node at path, which the document does not hold, as a key-value of
// the nearest table it holds.
func (e *editing) insert(path Path, node *syntax.Node) error {
	var current map[string]any
	if err := gotoml.Unmarshal(e.src, &current); err != nil {
		return e.fail(err, "")
	}

	table := path.parent()
	for len(table.steps) > 0 {
		if _, found, _ := lookup(current, table); found {
			break
		}

		table = table.parent()
	}

	rest := Path{steps: path.steps[len(table.steps):]}
	tableNames := table.names()
	container := e.index.ContainerOf(tableNames)

	if !rest.steps[0].isKey {
		return e.addElement(container, path, node)
	}

	for _, each := range rest.steps {
		if !each.isKey {
			return e.fail(ErrNotFound, "no array at "+Path{steps: path.steps[:len(path.steps)-1]}.String())
		}
	}

	if container.Collection != nil {
		if container.Collection.Kind != syntax.KindInlineTable {
			return e.fail(ErrConflict, table.String()+" is an array")
		}

		key := append(slices.Clone(container.Prefix), rest.keys()...)
		member := format.Names(key) + " = " + format.ValueAt(e.ordered(path.names(), node), 0, 0)
		before := edit.InlineBefore(container.Collection, container.Prefix, key[len(container.Prefix)], tableNames,
			e.sorter())
		e.splice(e.index.AddElement(container.Collection, member, before))

		return nil
	}

	if container.Implied {
		header := "[" + format.Names(table.keys()) + "]\n"
		e.splice(
			[]edit.Splice{e.index.SectionBefore(container.Section, header+e.pairText(rest.keys(), path.names(), node))},
		)

		return nil
	}

	key := append(slices.Clone(container.Prefix), rest.keys()...)
	e.splice([]edit.Splice{e.index.AddPair(container.Section, container.Prefix, key[len(container.Prefix)],
		e.pairText(key, path.names(), node), e.sorter())})

	return nil
}

// addElement writes node as the element at path, past the end of an inline array.
func (e *editing) addElement(container edit.Container, path Path, node *syntax.Node) error {
	if container.Collection == nil || container.Collection.Kind != syntax.KindArray {
		return e.fail(ErrConflict, path.parent().String()+" is an array of tables: Append adds to it")
	}

	if path.last().index != edit.Count(container.Collection) {
		return e.fail(
			ErrNotFound,
			fmt.Sprintf("%s has %d elements", path.parent(), edit.Count(container.Collection)),
		)
	}

	e.splice(e.index.AddElement(container.Collection, format.ValueAt(e.ordered(path.names(), node), 0, 0),
		edit.Count(container.Collection)))

	return nil
}

// setTable writes node, a rendered table, at path as a section.
func (e *editing) setTable(path Path, node *syntax.Node) error {
	names := path.names()
	text := e.sectionText(path, node, false)

	if keep, splice, ok := e.index.Own(names, text); ok {
		e.splice(append(e.index.Delete(names, keep), splice))

		return nil
	}

	if !path.last().isKey {
		return e.fail(ErrNotFound, "no element "+path.String())
	}

	if _, found, _ := lookup(e.before, path); found {
		e.splice(e.index.Delete(names, -1))
	}

	return e.addSection(path, text)
}

// addSection writes a new section for the table at path.
func (e *editing) addSection(path Path, text string) error {
	parent := path.parent().names()

	var current map[string]any
	if err := gotoml.Unmarshal(e.src, &current); err != nil {
		return e.fail(err, "")
	}

	for ancestor := path.parent(); len(ancestor.steps) > 0; ancestor = ancestor.parent() {
		if _, found, _ := lookup(current, ancestor); !found {
			continue
		}

		if container := e.index.ContainerOf(ancestor.names()); container.Collection != nil {
			return e.fail(ErrConflict, ancestor.String()+" is written inline: a section cannot extend it")
		}

		break
	}

	e.splice(
		[]edit.Splice{e.index.AddSection(append(slices.Clone(parent), path.names()[len(parent)]), text, e.sorter())},
	)

	return nil
}

// append writes node as the element at index of the array at path.
func (e *editing) append(path Path, index int, node *syntax.Node, read any, found bool) error {
	element := path.Index(index)
	_, table := read.(map[string]any)

	if found {
		if exact, ok := e.index.Exact(path.names()); ok {
			e.splice(e.index.AddElement(exact, format.ValueAt(e.ordered(element.names(), node), 0, 0), index))

			return nil
		}

		if !table {
			return e.fail(ErrConflict, path.String()+" is an array of tables: what it holds is tables")
		}

		splice, ok := e.index.ArrayEnd(path.names(), e.sectionText(element, node, true))
		if !ok {
			return e.fail(ErrConflict, path.String()+" is not written as an array")
		}

		e.splice([]edit.Splice{splice})

		return nil
	}

	if !table {
		return e.insert(path, &syntax.Node{Kind: syntax.KindArray, Children: []*syntax.Node{
			{Kind: syntax.KindOpenBracket, Text: []byte("[")}, node, {Kind: syntax.KindCloseBracket, Text: []byte("]")},
		}})
	}

	return e.addSection(path, e.sectionText(element, node, true))
}

// ordered returns node, the value at path, with the keys of its tables in the
// schema's order, if there is a schema.
func (e *editing) ordered(path []string, node *syntax.Node) *syntax.Node {
	sort := e.sorter()
	if sort == nil {
		return node
	}

	fragment, err := syntax.Parse([]byte("v = " + write(node) + "\n"))
	if err != nil {
		return node
	}

	fragment = order.Keys(fragment, func(at, keys []string) []string {
		if len(at) == 0 {
			return keys
		}

		return sort(append(slices.Clone(path), at[1:]...), keys)
	})

	for _, child := range fragment.Children {
		if child.Kind == syntax.KindKeyValue {
			return child.Value()
		}
	}

	return node
}

// pairText returns the canonical lines of a key-value of key, relative to its
// section, and the value node at path.
func (e *editing) pairText(key, path []string, node *syntax.Node) string {
	return canonicalLines(format.Names(key) + " = " + write(e.ordered(path, node)) + "\n")
}

// sectionText returns the canonical lines of a section for the table node at path:
// its header, a table's or an array of tables' element, and its members.
func (e *editing) sectionText(path Path, node *syntax.Node, element bool) string {
	header := "[" + format.Names(path.keys()) + "]\n"
	if element || !path.last().isKey {
		header = "[[" + format.Names(path.keys()) + "]]\n"
	}

	ordered := e.ordered(path.names(), node)

	var body strings.Builder

	for _, child := range ordered.Children {
		if child.Kind == syntax.KindKeyValue {
			body.WriteString(write(child) + "\n")
		}
	}

	return header + canonicalLines(body.String())
}

// canonicalLines returns lines of key-values in the canonical layout.
func canonicalLines(text string) string {
	root, err := syntax.Parse([]byte(text))
	if err != nil {
		return text
	}

	return string(format.Document(root))
}

func write(node *syntax.Node) string {
	var buf bytes.Buffer

	node.Write(&buf)

	return buf.String()
}

func kindOf(value any) string {
	switch value.(type) {
	case map[string]any:
		return "table"
	case []any:
		return "array"
	case string:
		return "string"
	default:
		return fmt.Sprintf("%T", value)
	}
}
